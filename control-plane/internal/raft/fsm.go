package raft

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"time"

	hashiraft "github.com/hashicorp/raft"
)

// CommandType identifies the FSM command being applied.
type CommandType string

const (
	CmdRegisterWorker     CommandType = "register_worker"
	CmdUpdateWorkerStatus CommandType = "update_worker_status"
	CmdSubmitJob          CommandType = "submit_job"
	CmdUpdateTaskStatus   CommandType = "update_task_status"
	CmdUpdateJobStatus    CommandType = "update_job_status"
	CmdBatch              CommandType = "batch"
)

// BatchPayload carries multiple commands to be applied in order.
type BatchPayload struct {
	Commands []Command `json:"commands"`
}

// Command is the envelope for all FSM commands. Payload is type-specific JSON.
type Command struct {
	Type    CommandType     `json:"type"`
	Payload json.RawMessage `json:"payload"`
}

// RegisterWorkerPayload carries fields for a register_worker command.
type RegisterWorkerPayload struct {
	ID       string `json:"id"`
	Address  string `json:"address"`
	CloudTag string `json:"cloud_tag"`
}

// UpdateWorkerStatusPayload carries fields for an update_worker_status command.
type UpdateWorkerStatusPayload struct {
	ID     string `json:"id"`
	Status string `json:"status"`
}

// SubmitJobPayload carries fields for a submit_job command.
type SubmitJobPayload struct {
	JobID      string      `json:"job_id"`
	MapTasks   []*TaskInfo `json:"map_tasks"`
	Status     string      `json:"status"`
}

// UpdateTaskStatusPayload carries fields for an update_task_status command.
type UpdateTaskStatusPayload struct {
	JobID      string `json:"job_id"`
	TaskID     string `json:"task_id"`
	Status     string `json:"status"`
	DurationMs int64  `json:"duration_ms"`
	TaskType   string `json:"task_type"` // "map" or "reduce"
	ReduceTask *TaskInfo `json:"reduce_task,omitempty"` // populated if this update triggers a reduce task
}

// UpdateJobStatusPayload carries fields for an update_job_status command.
type UpdateJobStatusPayload struct {
	JobID  string `json:"job_id"`
	Status string `json:"status"`
}

// WorkerInfo holds runtime state for a registered worker.
type WorkerInfo struct {
	ID       string    `json:"id"`
	Address  string    `json:"address"`
	CloudTag string    `json:"cloud_tag"`
	Status   string    `json:"status"`
	LastSeen time.Time `json:"last_seen"`
}

// TaskInfo holds state for a single task (Map or Reduce).
type TaskInfo struct {
	ID         string            `json:"id"`
	Type       string            `json:"type"` // "map" or "reduce"
	Status     string            `json:"status"` // "pending", "assigned", "done", "failed"
	WorkerID   string            `json:"worker_id,omitempty"`
	StartedAt  time.Time         `json:"started_at,omitempty"`
	DurationMs int64             `json:"duration_ms,omitempty"`
	Metadata   map[string]string `json:"metadata"` // URI info, block indices, etc.
}

// JobInfo holds state for a complete MapReduce job.
type JobInfo struct {
	ID          string               `json:"id"`
	Status      string               `json:"status"` // "pending", "mapping", "reducing", "done", "failed"
	MapTasks    map[string]*TaskInfo `json:"map_tasks"`
	ReduceTasks map[string]*TaskInfo `json:"reduce_tasks"`
	CreatedAt   time.Time            `json:"created_at"`
}

// PipelineFSM is the Raft finite state machine for the control plane.
// Raft calls Apply() serially, so map mutations are safe without a lock.
// External readers (HTTP handlers) hold mu.RLock to avoid data races.
type PipelineFSM struct {
	mu      sync.RWMutex
	workers map[string]*WorkerInfo
	jobs    map[string]*JobInfo
}

// NewPipelineFSM constructs a ready-to-use PipelineFSM.
func NewPipelineFSM() *PipelineFSM {
	return &PipelineFSM{
		workers: make(map[string]*WorkerInfo),
		jobs:    make(map[string]*JobInfo),
	}
}

// Apply is called by Raft once a log entry is committed by a quorum.
func (f *PipelineFSM) Apply(log *hashiraft.Log) interface{} {
	if log.Type != hashiraft.LogCommand {
		return nil
	}
	var cmd Command
	if err := json.Unmarshal(log.Data, &cmd); err != nil {
		slog.Error("FSM Apply: unmarshal command", "error", err, "index", log.Index)
		return fmt.Errorf("unmarshal command: %w", err)
	}

	f.mu.Lock()
	defer f.mu.Unlock()

	switch cmd.Type {
	case CmdRegisterWorker:
		return f.applyRegisterWorker(cmd.Payload, log.Index)
	case CmdUpdateWorkerStatus:
		return f.applyUpdateWorkerStatus(cmd.Payload, log.Index)
	case CmdSubmitJob:
		return f.applySubmitJob(cmd.Payload, log.Index)
	case CmdUpdateTaskStatus:
		return f.applyUpdateTaskStatus(cmd.Payload, log.Index)
	case CmdUpdateJobStatus:
		return f.applyUpdateJobStatus(cmd.Payload, log.Index)
	case CmdBatch:
		return f.applyBatch(cmd.Payload, log.Index)
	default:
		slog.Warn("FSM Apply: unknown command type", "type", cmd.Type, "index", log.Index)
		return fmt.Errorf("unknown command type: %s", cmd.Type)
	}
}

func (f *PipelineFSM) applyBatch(raw json.RawMessage, index uint64) interface{} {
	var p BatchPayload
	if err := json.Unmarshal(raw, &p); err != nil {
		return fmt.Errorf("unmarshal batch: %w", err)
	}

	for _, subCmd := range p.Commands {
		var err interface{}
		switch subCmd.Type {
		case CmdRegisterWorker:
			err = f.applyRegisterWorker(subCmd.Payload, index)
		case CmdUpdateWorkerStatus:
			err = f.applyUpdateWorkerStatus(subCmd.Payload, index)
		case CmdSubmitJob:
			err = f.applySubmitJob(subCmd.Payload, index)
		case CmdUpdateTaskStatus:
			err = f.applyUpdateTaskStatus(subCmd.Payload, index)
		case CmdUpdateJobStatus:
			err = f.applyUpdateJobStatus(subCmd.Payload, index)
		default:
			slog.Warn("FSM Apply Batch: unknown sub-command type", "type", subCmd.Type, "index", index)
			err = fmt.Errorf("unknown sub-command type: %s", subCmd.Type)
		}
		if err != nil {
			return err
		}
	}
	return nil
}

func (f *PipelineFSM) applyRegisterWorker(raw json.RawMessage, index uint64) interface{} {
	var p RegisterWorkerPayload
	if err := json.Unmarshal(raw, &p); err != nil {
		return fmt.Errorf("unmarshal register_worker: %w", err)
	}
	f.workers[p.ID] = &WorkerInfo{
		ID:       p.ID,
		Address:  p.Address,
		CloudTag: p.CloudTag,
		Status:   "online",
		LastSeen: time.Now().UTC(),
	}
	slog.Info("FSM: worker registered", "worker_id", p.ID, "cloud", p.CloudTag,
		"index", index)
	return nil
}

func (f *PipelineFSM) applyUpdateWorkerStatus(raw json.RawMessage, index uint64) interface{} {
	var p UpdateWorkerStatusPayload
	if err := json.Unmarshal(raw, &p); err != nil {
		return fmt.Errorf("unmarshal update_worker_status: %w", err)
	}
	w, ok := f.workers[p.ID]
	if !ok {
		return fmt.Errorf("worker %q not found", p.ID)
	}
	w.Status = p.Status
	w.LastSeen = time.Now().UTC()
	slog.Info("FSM: worker status updated", "worker_id", p.ID, "status", p.Status, "index", index)
	return nil
}

func (f *PipelineFSM) applySubmitJob(raw json.RawMessage, index uint64) interface{} {
	var p SubmitJobPayload
	if err := json.Unmarshal(raw, &p); err != nil {
		return fmt.Errorf("unmarshal submit_job: %w", err)
	}
	
	job := &JobInfo{
		ID:          p.JobID,
		Status:      p.Status,
		MapTasks:    make(map[string]*TaskInfo),
		ReduceTasks: make(map[string]*TaskInfo),
		CreatedAt:   time.Now().UTC(),
	}
	for _, t := range p.MapTasks {
		job.MapTasks[t.ID] = t
	}
	f.jobs[p.JobID] = job
	slog.Info("FSM: job submitted", "job_id", p.JobID, "map_tasks", len(p.MapTasks), "index", index)
	return nil
}

func (f *PipelineFSM) applyUpdateTaskStatus(raw json.RawMessage, index uint64) interface{} {
	var p UpdateTaskStatusPayload
	if err := json.Unmarshal(raw, &p); err != nil {
		return fmt.Errorf("unmarshal update_task_status: %w", err)
	}
	job, ok := f.jobs[p.JobID]
	if !ok {
		return fmt.Errorf("job %q not found", p.JobID)
	}

	var task *TaskInfo
	if p.TaskType == "map" {
		task, ok = job.MapTasks[p.TaskID]
	} else {
		task, ok = job.ReduceTasks[p.TaskID]
	}

	if !ok {
		// If it's a reduce task update but task doesn't exist, it might be the first time we hear about it
		// (though usually we create them when mapping finishes).
		if p.TaskType == "reduce" && p.ReduceTask != nil {
			job.ReduceTasks[p.TaskID] = p.ReduceTask
			task = p.ReduceTask
		} else {
			return fmt.Errorf("task %q not found in job %q", p.TaskID, p.JobID)
		}
	}

	task.Status = p.Status
	task.DurationMs = p.DurationMs
	
	// If this update also carries a new reduce task (triggered by mapping finishing), add it.
	if p.ReduceTask != nil {
		job.ReduceTasks[p.ReduceTask.ID] = p.ReduceTask
		slog.Info("FSM: reduce task added", "job_id", p.JobID, "task_id", p.ReduceTask.ID, "index", index)
	}

	slog.Info("FSM: task status updated", "job_id", p.JobID, "task_id", p.TaskID, "status", p.Status, "index", index)
	return nil
}

func (f *PipelineFSM) applyUpdateJobStatus(raw json.RawMessage, index uint64) interface{} {
	var p UpdateJobStatusPayload
	if err := json.Unmarshal(raw, &p); err != nil {
		return fmt.Errorf("unmarshal update_job_status: %w", err)
	}
	job, ok := f.jobs[p.JobID]
	if !ok {
		return fmt.Errorf("job %q not found", p.JobID)
	}
	job.Status = p.Status
	slog.Info("FSM: job status updated", "job_id", p.JobID, "status", p.Status, "index", index)
	return nil
}

// Snapshot captures a point-in-time copy of FSM state for Raft snapshotting.
func (f *PipelineFSM) Snapshot() (hashiraft.FSMSnapshot, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()

	state := struct {
		Workers map[string]*WorkerInfo `json:"workers"`
		Jobs    map[string]*JobInfo    `json:"jobs"`
	}{
		Workers: f.workers,
		Jobs:    f.jobs,
	}

	data, err := json.Marshal(state)
	if err != nil {
		return nil, fmt.Errorf("snapshot marshal: %w", err)
	}
	slog.Info("FSM Snapshot", "workers", len(f.workers), "jobs", len(f.jobs))
	return &pipelineFSMSnapshot{data: data}, nil
}

// Restore replaces FSM state from a snapshot reader.
func (f *PipelineFSM) Restore(rc io.ReadCloser) error {
	defer rc.Close()
	var state struct {
		Workers map[string]*WorkerInfo `json:"workers"`
		Jobs    map[string]*JobInfo    `json:"jobs"`
	}
	if err := json.NewDecoder(rc).Decode(&state); err != nil {
		return fmt.Errorf("restore decode: %w", err)
	}
	f.mu.Lock()
	f.workers = state.Workers
	f.jobs = state.Jobs
	f.mu.Unlock()
	slog.Info("FSM Restore", "workers", len(f.workers), "jobs", len(f.jobs))
	return nil
}

// Workers returns a copy of all workers for external readers.
func (f *PipelineFSM) Workers() map[string]*WorkerInfo {
	f.mu.RLock()
	defer f.mu.RUnlock()
	out := make(map[string]*WorkerInfo, len(f.workers))
	for k, v := range f.workers {
		cp := *v
		out[k] = &cp
	}
	return out
}

// Jobs returns a copy of all jobs for external readers.
func (f *PipelineFSM) Jobs() map[string]*JobInfo {
	f.mu.RLock()
	defer f.mu.RUnlock()
	out := make(map[string]*JobInfo, len(f.jobs))
	for k, v := range f.jobs {
		cp := *v
		out[k] = &cp
	}
	return out
}

// GetJob returns a copy of a specific job, or nil if not found.
func (f *PipelineFSM) GetJob(id string) *JobInfo {
	f.mu.RLock()
	defer f.mu.RUnlock()
	job, ok := f.jobs[id]
	if !ok {
		return nil
	}
	cp := *job
	return &cp
}

// GetWorker returns a copy of a specific worker, or nil if not found.
func (f *PipelineFSM) GetWorker(id string) *WorkerInfo {
	f.mu.RLock()
	defer f.mu.RUnlock()
	w, ok := f.workers[id]
	if !ok {
		return nil
	}
	cp := *w
	return &cp
}

// MarshalCommand is a convenience helper to build a JSON-encoded Command.
func MarshalCommand(t CommandType, payload interface{}) ([]byte, error) {
	p, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	return json.Marshal(Command{Type: t, Payload: p})
}

// pipelineFSMSnapshot implements raft.FSMSnapshot.
type pipelineFSMSnapshot struct {
	data []byte
}

func (s *pipelineFSMSnapshot) Persist(sink hashiraft.SnapshotSink) error {
	if _, err := sink.Write(s.data); err != nil {
		_ = sink.Cancel()
		return fmt.Errorf("snapshot write: %w", err)
	}
	return sink.Close()
}

func (s *pipelineFSMSnapshot) Release() {}

// Ensure PipelineFSM satisfies the interface at compile time.
var _ hashiraft.FSM = (*PipelineFSM)(nil)

// newTestSink is only used in tests — lives here so test file stays clean.
type testSnapshotSink struct{ buf *bytes.Buffer }

func (s *testSnapshotSink) Write(p []byte) (int, error) { return s.buf.Write(p) }
func (s *testSnapshotSink) Close() error                { return nil }
func (s *testSnapshotSink) ID() string                  { return "test-sink" }
func (s *testSnapshotSink) Cancel() error               { return nil }
