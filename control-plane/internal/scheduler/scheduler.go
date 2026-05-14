package scheduler

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"net"
	"strings"
	"sync"
	"time"

	taskpb "github.com/joelcrouch/pipeline-orchestrator/control-plane/internal/gen/task"
	"github.com/joelcrouch/pipeline-orchestrator/control-plane/internal/raft"
)

type JobScheduler struct {
	taskpb.UnimplementedTaskServiceServer
	raft *raft.RaftNode
	fsm  *raft.PipelineFSM

	mu sync.Mutex
	// in-memory queues for pending tasks
	mapQueue    []*taskpb.MapTask
	reduceQueue []*taskpb.ReduceTask

	// tracks assigned tasks for re-queueing on timeout
	assignedMap    map[string]assignedTask
	assignedReduce map[string]assignedTask

	grpcPort string
}

type assignedTask struct {
	assignedAt time.Time
	workerID   string
	task       interface{}
}

func NewJobScheduler(r *raft.RaftNode, f *raft.PipelineFSM, grpcPort string) *JobScheduler {
	s := &JobScheduler{
		raft:           r,
		fsm:            f,
		assignedMap:    make(map[string]assignedTask),
		assignedReduce: make(map[string]assignedTask),
		grpcPort:       grpcPort,
	}
	go s.requeueLoop()
	return s
}

// raftAddrToGRPC converts a Raft peer address (e.g. "cp-aws-1:7000") into
// the corresponding gRPC address (e.g. "cp-aws-1:50051") by replacing the port.
func (s *JobScheduler) raftAddrToGRPC(raftAddr string) string {
	if raftAddr == "" {
		return ""
	}
	host, _, err := net.SplitHostPort(raftAddr)
	if err != nil {
		slog.Warn("raftAddrToGRPC: cannot parse addr", "addr", raftAddr)
		return ""
	}
	// If the host is an IP address, use the leader's server ID (hostname) instead.
	if net.ParseIP(host) != nil {
		id := s.raft.LeaderID()
		if id == "" {
			return ""
		}
		host = id
	}
	return net.JoinHostPort(host, s.grpcPort)
}

func (s *JobScheduler) SubmitJob(ctx context.Context, req *taskpb.JobRequest) (*taskpb.JobResponse, error) {
	if s.raft.State() != 2 { // Leader
		return &taskpb.JobResponse{
			Ok:         false,
			Error:      "not leader",
			LeaderAddr: s.raftAddrToGRPC(s.raft.Leader()),
		}, nil
	}

	bi := int(math.Ceil(float64(req.M) / float64(req.BlockSize)))
	bj := int(math.Ceil(float64(req.KDim) / float64(req.BlockSize)))
	bk := int(math.Ceil(float64(req.N) / float64(req.BlockSize)))

	var mapTasks []*raft.TaskInfo
	base := fmt.Sprintf("s3://pipeline-data/jobs/%s", req.JobId)

	modelType := req.ModelType
	if modelType == "" {
		modelType = "matmul"
	}

	for i := 0; i < bi; i++ {
		for j := 0; j < bj; j++ {
			for k := 0; k < bk; k++ {
				taskID := fmt.Sprintf("map_%d_%d_%d", i, j, k)
				mapTasks = append(mapTasks, &raft.TaskInfo{
					ID:     taskID,
					Type:   "map",
					Status: "pending",
					Metadata: map[string]string{
						"i":          fmt.Sprintf("%d", i),
						"j":          fmt.Sprintf("%d", j),
						"k":          fmt.Sprintf("%d", k),
						"a_uri":      fmt.Sprintf("%s/blocks/A_%d_%d.npy", base, i, j),
						"b_uri":      fmt.Sprintf("%s/blocks/B_%d_%d.npy", base, j, k),
						"output_uri": fmt.Sprintf("%s/partial/C_%d_%d_%d.npy", base, i, k, j),
						"block_size": fmt.Sprintf("%d", req.BlockSize),
						"model_type": modelType,
						"job_id":     req.JobId,
					},
				})
			}
		}
	}

	payload := raft.SubmitJobPayload{
		JobID:    req.JobId,
		MapTasks: mapTasks,
		Status:   "mapping",
	}

	cmd, err := raft.MarshalCommand(raft.CmdSubmitJob, payload)
	if err != nil {
		return nil, err
	}

	if err := s.raft.Apply(cmd, 20*time.Second); err != nil {
		return nil, err
	}

	s.rebuildQueues()

	return &taskpb.JobResponse{Ok: true}, nil
}

func (s *JobScheduler) PollTask(ctx context.Context, req *taskpb.PollTaskRequest) (*taskpb.PollTaskResponse, error) {
	if s.raft.State() != 2 {
		return &taskpb.PollTaskResponse{
			HasTask:    false,
			LeaderAddr: s.raftAddrToGRPC(s.raft.Leader()),
		}, nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if req.TaskType == "map" {
		if len(s.mapQueue) > 0 {
			t := s.mapQueue[0]
			s.mapQueue = s.mapQueue[1:]
			s.assignedMap[t.TaskId] = assignedTask{
				assignedAt: time.Now(),
				workerID:   req.WorkerId,
				task:       t,
			}
			return &taskpb.PollTaskResponse{HasTask: true, MapTask: t}, nil
		}
	} else if req.TaskType == "reduce" {
		if len(s.reduceQueue) > 0 {
			t := s.reduceQueue[0]
			s.reduceQueue = s.reduceQueue[1:]
			s.assignedReduce[t.TaskId] = assignedTask{
				assignedAt: time.Now(),
				workerID:   req.WorkerId,
				task:       t,
			}
			return &taskpb.PollTaskResponse{HasTask: true, ReduceTask: t}, nil
		}
	}

	return &taskpb.PollTaskResponse{HasTask: false}, nil
}

func (s *JobScheduler) ReportTaskResult(ctx context.Context, req *taskpb.ReportResultRequest) (*taskpb.ReportResultResponse, error) {
	if s.raft.State() != 2 {
		return &taskpb.ReportResultResponse{
			Ok:         false,
			LeaderAddr: s.raftAddrToGRPC(s.raft.Leader()),
		}, nil
	}

	s.mu.Lock()
	delete(s.assignedMap, req.TaskId)
	delete(s.assignedReduce, req.TaskId)
	s.mu.Unlock()

	taskType := "map"
	if strings.HasPrefix(req.TaskId, "reduce_") {
		taskType = "reduce"
	}

	payload := raft.UpdateTaskStatusPayload{
		JobID:      req.JobId,
		TaskID:     req.TaskId,
		Status:     "done",
		DurationMs: req.DurationMs,
		TaskType:   taskType,
	}
	if !req.Success {
		payload.Status = "failed"
	}

	// Logic to trigger Reduce tasks
	if taskType == "map" && req.Success {
		newReduce := s.checkMapProgress(req.JobId, req.TaskId)
		if newReduce != nil {
			payload.ReduceTask = newReduce
		}
	}

	cmd, _ := raft.MarshalCommand(raft.CmdUpdateTaskStatus, payload)
	if err := s.raft.Apply(cmd, 20*time.Second); err != nil {
		return nil, err
	}

	// Update in-memory queues if a new reduce task was added
	if payload.ReduceTask != nil {
		s.mu.Lock()
		s.reduceQueue = append(s.reduceQueue, taskInfoToReduceTask(req.JobId, payload.ReduceTask))
		s.mu.Unlock()
	}

	if taskType == "reduce" && req.Success {
		s.checkJobProgress(req.JobId)
	}

	return &taskpb.ReportResultResponse{Ok: true}, nil
}

func (s *JobScheduler) GetJobStatus(ctx context.Context, req *taskpb.GetJobStatusRequest) (*taskpb.GetJobStatusResponse, error) {
	job := s.fsm.GetJob(req.JobId)
	if job == nil {
		return nil, fmt.Errorf("job not found")
	}

	mDone := 0
	for _, t := range job.MapTasks {
		if t.Status == "done" {
			mDone++
		}
	}
	rDone := 0
	for _, t := range job.ReduceTasks {
		if t.Status == "done" {
			rDone++
		}
	}

	return &taskpb.GetJobStatusResponse{
		JobId:       job.ID,
		Status:      job.Status,
		MapTotal:    int32(len(job.MapTasks)),
		MapDone:     int32(mDone),
		ReduceTotal: int32(len(job.ReduceTasks)),
		ReduceDone:  int32(rDone),
		LeaderAddr:  s.raftAddrToGRPC(s.raft.Leader()),
	}, nil
}

func (s *JobScheduler) rebuildQueues() {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.mapQueue = nil
	s.reduceQueue = nil

	for _, job := range s.fsm.Jobs() {
		if job.Status == "done" || job.Status == "failed" {
			continue
		}
		for _, t := range job.MapTasks {
			if t.Status == "pending" {
				s.mapQueue = append(s.mapQueue, taskInfoToMapTask(job.ID, t))
			}
		}
		for _, t := range job.ReduceTasks {
			if t.Status == "pending" {
				s.reduceQueue = append(s.reduceQueue, taskInfoToReduceTask(job.ID, t))
			}
		}
	}
}

func (s *JobScheduler) checkMapProgress(jobID, mapTaskID string) *raft.TaskInfo {
	job := s.fsm.GetJob(jobID)
	if job == nil {
		return nil
	}

	var i, j, k int
	fmt.Sscanf(mapTaskID, "map_%d_%d_%d", &i, &j, &k)

	maxJ := -1
	for _, t := range job.MapTasks {
		var ti, tj, tk int
		fmt.Sscanf(t.ID, "map_%d_%d_%d", &ti, &tj, &tk)
		if ti == i && tk == k && tj > maxJ {
			maxJ = tj
		}
	}

	for tj := 0; tj <= maxJ; tj++ {
		tid := fmt.Sprintf("map_%d_%d_%d", i, tj, k)
		t, ok := job.MapTasks[tid]
		if !ok || (tid != mapTaskID && t.Status != "done") {
			return nil
		}
	}

	// All maps for this (i,k) done
	reduceID := fmt.Sprintf("reduce_%d_%d", i, k)
	if _, exists := job.ReduceTasks[reduceID]; exists {
		return nil
	}

	var inputURIs []string
	for tj := 0; tj <= maxJ; tj++ {
		tid := fmt.Sprintf("map_%d_%d_%d", i, tj, k)
		inputURIs = append(inputURIs, job.MapTasks[tid].Metadata["output_uri"])
	}

	return &raft.TaskInfo{
		ID:     reduceID,
		Type:   "reduce",
		Status: "pending",
		Metadata: map[string]string{
			"i":          fmt.Sprintf("%d", i),
			"k":          fmt.Sprintf("%d", k),
			"input_uris": strings.Join(inputURIs, ","),
			"output_uri": fmt.Sprintf("s3://pipeline-data/jobs/%s/result/C_%d_%d.npy", jobID, i, k),
		},
	}
}

func (s *JobScheduler) checkJobProgress(jobID string) {
	job := s.fsm.GetJob(jobID)
	if job == nil {
		return
	}

	allDone := true
	for _, t := range job.ReduceTasks {
		if t.Status != "done" {
			allDone = false
			break
		}
	}

	// Check if we even have all reduce tasks yet
	var maxI, maxK int
	for _, t := range job.MapTasks {
		var ti, tj, tk int
		fmt.Sscanf(t.ID, "map_%d_%d_%d", &ti, &tj, &tk)
		if ti > maxI {
			maxI = ti
		}
		if tk > maxK {
			maxK = tk
		}
	}
	if len(job.ReduceTasks) < (maxI+1)*(maxK+1) {
		allDone = false
	}

	if allDone {
		cmd, _ := raft.MarshalCommand(raft.CmdUpdateJobStatus, raft.UpdateJobStatusPayload{JobID: jobID, Status: "done"})
		s.raft.Apply(cmd, 5*time.Second)
	}
}

func (s *JobScheduler) requeueLoop() {
	ticker := time.NewTicker(10 * time.Second)
	for range ticker.C {
		if s.raft.State() == 2 {
			s.requeueTimedOutTasks()
		}
	}
}

func (s *JobScheduler) requeueTimedOutTasks() {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	timeout := 30 * time.Second
	for id, at := range s.assignedMap {
		if now.Sub(at.assignedAt) > timeout {
			slog.Warn("Map task timed out, re-queueing", "task_id", id, "worker_id", at.workerID)
			s.mapQueue = append(s.mapQueue, at.task.(*taskpb.MapTask))
			delete(s.assignedMap, id)
		}
	}
	for id, at := range s.assignedReduce {
		if now.Sub(at.assignedAt) > timeout {
			s.reduceQueue = append(s.reduceQueue, at.task.(*taskpb.ReduceTask))
			delete(s.assignedReduce, id)
		}
	}
}

func taskInfoToMapTask(jobID string, t *raft.TaskInfo) *taskpb.MapTask {
	return &taskpb.MapTask{
		JobId:     jobID,
		TaskId:    t.ID,
		I:         mustParseInt32(t.Metadata["i"]),
		J:         mustParseInt32(t.Metadata["j"]),
		K:         mustParseInt32(t.Metadata["k"]),
		AUri:      t.Metadata["a_uri"],
		BUri:      t.Metadata["b_uri"],
		OutputUri: t.Metadata["output_uri"],
		BlockSize: mustParseInt32(t.Metadata["block_size"]),
		Metadata:  t.Metadata,
	}
}

func taskInfoToReduceTask(jobID string, t *raft.TaskInfo) *taskpb.ReduceTask {
	return &taskpb.ReduceTask{
		JobId:     jobID,
		TaskId:    t.ID,
		I:         mustParseInt32(t.Metadata["i"]),
		K:         mustParseInt32(t.Metadata["k"]),
		InputUris: strings.Split(t.Metadata["input_uris"], ","),
		OutputUri: t.Metadata["output_uri"],
		Metadata:  t.Metadata,
	}
}

func mustParseInt32(s string) int32 {
	var i int32
	fmt.Sscanf(s, "%d", &i)
	return i
}
