package scheduler

import (
	"context"
	"testing"
	"time"

	taskpb "github.com/joelcrouch/pipeline-orchestrator/control-plane/internal/gen/task"
	"github.com/joelcrouch/pipeline-orchestrator/control-plane/internal/raft"
	"github.com/hashicorp/go-hclog"
	hashiraft "github.com/hashicorp/raft"
)

// ── Helpers ──────────────────────────────────────────────────────────────────

func setupTestCluster(t *testing.T) (*raft.RaftNode, *raft.PipelineFSM) {
	t.Helper()
	fsm := raft.NewPipelineFSM()
	cfg := raft.Config{
		NodeID:    "test-node",
		DataDir:   t.TempDir(),
		Bootstrap: true,
	}
	_, transport := hashiraft.NewInmemTransport(hashiraft.ServerAddress("test-node"))
	node, err := raft.NewRaftNodeWithTransport(cfg, fsm, transport, hclog.NewNullLogger())
	if err != nil {
		t.Fatalf("failed to create raft node: %v", err)
	}
	
	// Wait for leader
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if node.State() == hashiraft.Leader {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if node.State() != hashiraft.Leader {
		t.Fatal("node failed to become leader")
	}

	return node, fsm
}

// ── Tests ────────────────────────────────────────────────────────────────────

func TestSubmitJob(t *testing.T) {
	node, fsm := setupTestCluster(t)
	s := NewJobScheduler(node, fsm, "50051")

	req := &taskpb.JobRequest{
		JobId:     "job-1",
		M:         256,
		KDim:      256,
		N:         256,
		BlockSize: 128,
	}

	resp, err := s.SubmitJob(context.Background(), req)
	if err != nil {
		t.Fatalf("SubmitJob failed: %v", err)
	}
	if !resp.Ok {
		t.Fatalf("SubmitJob response not OK: %s", resp.Error)
	}

	// Verify FSM state
	job := fsm.GetJob("job-1")
	if job == nil {
		t.Fatal("job not found in FSM")
	}
	// 2*2*2 = 8 map tasks
	if len(job.MapTasks) != 8 {
		t.Errorf("expected 8 map tasks, got %d", len(job.MapTasks))
	}

	// Verify scheduler queues
	s.mu.Lock()
	qLen := len(s.mapQueue)
	s.mu.Unlock()
	if qLen != 8 {
		t.Errorf("expected 8 map tasks in queue, got %d", qLen)
	}
}

func TestPollAndReportTask(t *testing.T) {
	node, fsm := setupTestCluster(t)
	s := NewJobScheduler(node, fsm, "50051")

	s.SubmitJob(context.Background(), &taskpb.JobRequest{
		JobId: "job-1", M: 128, KDim: 128, N: 128, BlockSize: 128,
	})

	// Poll task
	pollResp, err := s.PollTask(context.Background(), &taskpb.PollTaskRequest{
		WorkerId: "worker-1", TaskType: "map",
	})
	if err != nil || !pollResp.HasTask {
		t.Fatalf("PollTask failed: %v, has_task=%v", err, pollResp.HasTask)
	}
	
	taskID := pollResp.MapTask.TaskId

	// Report result
	reportResp, err := s.ReportTaskResult(context.Background(), &taskpb.ReportResultRequest{
		JobId: "job-1", TaskId: taskID, Success: true, DurationMs: 100,
	})
	if err != nil || !reportResp.Ok {
		t.Fatalf("ReportTaskResult failed: %v", err)
	}

	// Verify FSM update
	job := fsm.GetJob("job-1")
	if job.MapTasks[taskID].Status != "done" {
		t.Errorf("expected task status done, got %s", job.MapTasks[taskID].Status)
	}
}

func TestShuffleTrigger(t *testing.T) {
	node, fsm := setupTestCluster(t)
	s := NewJobScheduler(node, fsm, "50051")

	s.SubmitJob(context.Background(), &taskpb.JobRequest{
		JobId: "job-1", M: 128, KDim: 256, N: 128, BlockSize: 128,
	})

	// Complete map tasks map_0_0_0 and map_0_1_0
	for _, tid := range []string{"map_0_0_0", "map_0_1_0"} {
		s.ReportTaskResult(context.Background(), &taskpb.ReportResultRequest{
			JobId: "job-1", TaskId: tid, Success: true,
		})
	}

	// Should have a reduce task now
	s.mu.Lock()
	if len(s.reduceQueue) != 1 {
		t.Errorf("expected 1 reduce task, got %d", len(s.reduceQueue))
	}
	s.mu.Unlock()
}

func TestFollowerRedirects(t *testing.T) {
	fsm := raft.NewPipelineFSM()
	cfg := raft.Config{NodeID: "follower", DataDir: t.TempDir(), Bootstrap: false}
	_, trans := hashiraft.NewInmemTransport(hashiraft.ServerAddress("follower"))
	node, _ := raft.NewRaftNodeWithTransport(cfg, fsm, trans, hclog.NewNullLogger())
	s := NewJobScheduler(node, fsm, "50051")

	ctx := context.Background()
	
	resp, _ := s.SubmitJob(ctx, &taskpb.JobRequest{JobId: "j1"})
	if resp.Ok { t.Error("SubmitJob on follower should not be OK") }

	poll, _ := s.PollTask(ctx, &taskpb.PollTaskRequest{WorkerId: "w1", TaskType: "map"})
	if poll.HasTask { t.Error("PollTask on follower should not return task") }

	report, _ := s.ReportTaskResult(ctx, &taskpb.ReportResultRequest{JobId: "j1", TaskId: "t1"})
	if report.Ok { t.Error("ReportResult on follower should not be OK") }
}

func TestTaskFailure(t *testing.T) {
	node, fsm := setupTestCluster(t)
	s := NewJobScheduler(node, fsm, "50051")

	s.SubmitJob(context.Background(), &taskpb.JobRequest{
		JobId: "fail-job", M: 128, KDim: 128, N: 128, BlockSize: 128,
	})

	_, _ = s.ReportTaskResult(context.Background(), &taskpb.ReportResultRequest{
		JobId: "fail-job", TaskId: "map_0_0_0", Success: false, Error: "oom",
	})

	job := fsm.GetJob("fail-job")
	if job.MapTasks["map_0_0_0"].Status != "failed" {
		t.Errorf("expected status failed, got %s", job.MapTasks["map_0_0_0"].Status)
	}
}

func TestGetJobStatus(t *testing.T) {
	node, fsm := setupTestCluster(t)
	s := NewJobScheduler(node, fsm, "50051")

	// 1. Non-existent job
	_, err := s.GetJobStatus(context.Background(), &taskpb.GetJobStatusRequest{JobId: "ghost"})
	if err == nil { t.Error("expected error for non-existent job") }

	// 2. Real job
	s.SubmitJob(context.Background(), &taskpb.JobRequest{
		JobId: "j1", M: 128, KDim: 128, N: 128, BlockSize: 128,
	})
	status, _ := s.GetJobStatus(context.Background(), &taskpb.GetJobStatusRequest{JobId: "j1"})
	if status.JobId != "j1" || status.MapTotal != 1 {
		t.Errorf("unexpected status: %+v", status)
	}
}

func TestMapRequeueTimeout(t *testing.T) {
	node, fsm := setupTestCluster(t)
	s := NewJobScheduler(node, fsm, "50051")
	s.SubmitJob(context.Background(), &taskpb.JobRequest{
		JobId: "j1", M: 128, KDim: 128, N: 128, BlockSize: 128,
	})

	s.PollTask(context.Background(), &taskpb.PollTaskRequest{WorkerId: "w1", TaskType: "map"})
	
	s.mu.Lock()
	at := s.assignedMap["map_0_0_0"]
	at.assignedAt = time.Now().Add(-1 * time.Hour)
	s.assignedMap["map_0_0_0"] = at
	s.mu.Unlock()

	s.requeueTimedOutTasks()

	s.mu.Lock()
	if len(s.mapQueue) != 1 { t.Error("map task not re-queued") }
	if len(s.assignedMap) != 0 { t.Error("task not removed from assigned map") }
	s.mu.Unlock()
}

func TestReduceRequeueTimeout(t *testing.T) {
	node, fsm := setupTestCluster(t)
	s := NewJobScheduler(node, fsm, "50051")
	s.SubmitJob(context.Background(), &taskpb.JobRequest{
		JobId: "j1", M: 128, KDim: 128, N: 128, BlockSize: 128,
	})

	// Setup: Map done -> Reduce pending -> Poll it
	s.ReportTaskResult(context.Background(), &taskpb.ReportResultRequest{JobId: "j1", TaskId: "map_0_0_0", Success: true})
	s.PollTask(context.Background(), &taskpb.PollTaskRequest{WorkerId: "w1", TaskType: "reduce"})

	s.mu.Lock()
	rat := s.assignedReduce["reduce_0_0"]
	rat.assignedAt = time.Now().Add(-1 * time.Hour)
	s.assignedReduce["reduce_0_0"] = rat
	s.mu.Unlock()

	s.requeueTimedOutTasks()

	s.mu.Lock()
	if len(s.reduceQueue) != 1 { t.Error("reduce task not re-queued") }
	if len(s.assignedReduce) != 0 { t.Error("task not removed from assigned map") }
	s.mu.Unlock()
}


func TestEndToEndJobProgress(t *testing.T) {
	node, fsm := setupTestCluster(t)
	s := NewJobScheduler(node, fsm, "50051")

	// 1x1x1 grid = 1 map, 1 reduce
	s.SubmitJob(context.Background(), &taskpb.JobRequest{
		JobId: "e2e", M: 128, KDim: 128, N: 128, BlockSize: 128,
	})

	// Complete Map
	s.ReportTaskResult(context.Background(), &taskpb.ReportResultRequest{
		JobId: "e2e", TaskId: "map_0_0_0", Success: true,
	})

	// Complete Reduce (this triggers checkJobProgress)
	s.ReportTaskResult(context.Background(), &taskpb.ReportResultRequest{
		JobId: "e2e", TaskId: "reduce_0_0", Success: true,
	})

	// Wait for FSM to reflect 'done' (async Apply)
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if fsm.GetJob("e2e").Status == "done" {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}

	job := fsm.GetJob("e2e")
	if job.Status != "done" {
		t.Errorf("expected job status done, got %s", job.Status)
	}
}

func TestRaftAddrToGRPC(t *testing.T) {
	s := &JobScheduler{grpcPort: "50051"}
	
	// 1. Valid host:port
	if got := s.raftAddrToGRPC("cp-aws-1:7000"); got != "cp-aws-1:50051" {
		t.Errorf("expected cp-aws-1:50051, got %s", got)
	}

	// 2. Malformed
	if got := s.raftAddrToGRPC("bad-addr"); got != "" {
		t.Errorf("expected empty string for bad addr, got %q", got)
	}

	// 3. Empty
	if got := s.raftAddrToGRPC(""); got != "" {
		t.Error("expected empty string for empty input")
	}
}

