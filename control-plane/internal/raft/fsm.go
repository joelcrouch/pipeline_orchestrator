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
)

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

// WorkerInfo holds runtime state for a registered worker.
type WorkerInfo struct {
	ID       string    `json:"id"`
	Address  string    `json:"address"`
	CloudTag string    `json:"cloud_tag"`
	Status   string    `json:"status"`
	LastSeen time.Time `json:"last_seen"`
}

// PipelineFSM is the Raft finite state machine for the control plane.
// Raft calls Apply() serially, so map mutations are safe without a lock.
// External readers (HTTP handlers) hold mu.RLock to avoid data races.
type PipelineFSM struct {
	mu      sync.RWMutex
	workers map[string]*WorkerInfo
}

// NewPipelineFSM constructs a ready-to-use PipelineFSM.
func NewPipelineFSM() *PipelineFSM {
	return &PipelineFSM{workers: make(map[string]*WorkerInfo)}
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
	default:
		slog.Warn("FSM Apply: unknown command type", "type", cmd.Type, "index", log.Index)
		return fmt.Errorf("unknown command type: %s", cmd.Type)
	}
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

// Snapshot captures a point-in-time copy of FSM state for Raft snapshotting.
func (f *PipelineFSM) Snapshot() (hashiraft.FSMSnapshot, error) {
	f.mu.RLock()
	workersCopy := make(map[string]*WorkerInfo, len(f.workers))
	for k, v := range f.workers {
		cp := *v
		workersCopy[k] = &cp
	}
	f.mu.RUnlock()

	data, err := json.Marshal(workersCopy)
	if err != nil {
		return nil, fmt.Errorf("snapshot marshal: %w", err)
	}
	slog.Info("FSM Snapshot", "workers", len(workersCopy))
	return &pipelineFSMSnapshot{data: data}, nil
}

// Restore replaces FSM state from a snapshot reader.
func (f *PipelineFSM) Restore(rc io.ReadCloser) error {
	defer rc.Close()
	var workers map[string]*WorkerInfo
	if err := json.NewDecoder(rc).Decode(&workers); err != nil {
		return fmt.Errorf("restore decode: %w", err)
	}
	f.mu.Lock()
	f.workers = workers
	f.mu.Unlock()
	slog.Info("FSM Restore", "workers", len(workers))
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
