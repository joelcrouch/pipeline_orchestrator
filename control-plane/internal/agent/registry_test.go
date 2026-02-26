package agent

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	hashiraft "github.com/hashicorp/raft"

	workerpb "github.com/joelcrouch/pipeline-orchestrator/control-plane/internal/gen/worker"
	internalraft "github.com/joelcrouch/pipeline-orchestrator/control-plane/internal/raft"
)

// ── mockRaft ────────────────────────────────────────────────────────────────

type mockRaft struct {
	isLeader    bool
	leaderAddr  string
	leaderID    string // server ID (hostname) of the leader
	appliedCmds [][]byte
	applyErr    error
}

func (m *mockRaft) Apply(cmd []byte, _ time.Duration) error {
	if m.applyErr != nil {
		return m.applyErr
	}
	m.appliedCmds = append(m.appliedCmds, cmd)
	return nil
}
func (m *mockRaft) Leader() string   { return m.leaderAddr }
func (m *mockRaft) LeaderID() string { return m.leaderID }
func (m *mockRaft) State() hashiraft.RaftState {
	if m.isLeader {
		return hashiraft.Leader
	}
	return hashiraft.Follower
}

// ── helpers ─────────────────────────────────────────────────────────────────

func newLeaderRegistry() (*AgentRegistry, *mockRaft) {
	mr := &mockRaft{isLeader: true, leaderAddr: "cp-aws-1:7000"}
	return NewAgentRegistry(mr, "50051"), mr
}

func newFollowerRegistry() (*AgentRegistry, *mockRaft) {
	mr := &mockRaft{isLeader: false, leaderAddr: "cp-aws-1:7000"}
	return NewAgentRegistry(mr, "50051"), mr
}

// lastAppliedCommand decodes the most recently applied Raft command.
func lastAppliedCommand(t *testing.T, mr *mockRaft) internalraft.Command {
	t.Helper()
	if len(mr.appliedCmds) == 0 {
		t.Fatal("no commands applied")
	}
	var cmd internalraft.Command
	if err := json.Unmarshal(mr.appliedCmds[len(mr.appliedCmds)-1], &cmd); err != nil {
		t.Fatalf("unmarshal command: %v", err)
	}
	return cmd
}

// ── RegisterWorker tests ─────────────────────────────────────────────────────

func TestRegisterWorker_OnLeader(t *testing.T) {
	reg, mr := newLeaderRegistry()
	resp, err := reg.RegisterWorker(context.Background(), &workerpb.RegisterWorkerRequest{
		WorkerId: "w-1", Address: "worker-aws-1:8081", CloudTag: "aws",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !resp.Ok {
		t.Errorf("expected ok=true, got false (leader_addr=%s)", resp.LeaderAddr)
	}
	if len(mr.appliedCmds) != 1 {
		t.Fatalf("expected 1 applied command, got %d", len(mr.appliedCmds))
	}
	cmd := lastAppliedCommand(t, mr)
	if cmd.Type != internalraft.CmdRegisterWorker {
		t.Errorf("expected CmdRegisterWorker, got %s", cmd.Type)
	}

	reg.mu.Lock()
	_, exists := reg.trackers["w-1"]
	reg.mu.Unlock()
	if !exists {
		t.Error("tracker for w-1 not created after registration")
	}
}

func TestRegisterWorker_OnFollower(t *testing.T) {
	reg, mr := newFollowerRegistry()
	resp, err := reg.RegisterWorker(context.Background(), &workerpb.RegisterWorkerRequest{
		WorkerId: "w-1", Address: "worker-aws-1:8081", CloudTag: "aws",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Ok {
		t.Error("expected ok=false on follower")
	}
	if resp.LeaderAddr != "cp-aws-1:50051" {
		t.Errorf("expected leader_addr=cp-aws-1:50051, got %s", resp.LeaderAddr)
	}
	if len(mr.appliedCmds) != 0 {
		t.Error("follower must not call Apply")
	}
}

func TestRegisterWorker_OnFollower_NoLeader(t *testing.T) {
	mr := &mockRaft{isLeader: false, leaderAddr: ""}
	reg := NewAgentRegistry(mr, "50051")
	resp, err := reg.RegisterWorker(context.Background(), &workerpb.RegisterWorkerRequest{
		WorkerId: "w-1",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.LeaderAddr != "" {
		t.Errorf("expected empty leader_addr when no leader known, got %s", resp.LeaderAddr)
	}
}

// ── Heartbeat tests ──────────────────────────────────────────────────────────

func TestHeartbeat_OnLeader(t *testing.T) {
	reg, _ := newLeaderRegistry()
	before := time.Now()
	resp, err := reg.Heartbeat(context.Background(), &workerpb.HeartbeatRequest{WorkerId: "w-1"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !resp.Ok {
		t.Error("expected ok=true")
	}
	reg.mu.Lock()
	tracker := reg.trackers["w-1"]
	reg.mu.Unlock()
	if tracker == nil {
		t.Fatal("tracker should have been created")
	}
	if tracker.LastSeen.Before(before) {
		t.Error("LastSeen not updated")
	}
}

func TestHeartbeat_OnFollower(t *testing.T) {
	reg, mr := newFollowerRegistry()
	resp, err := reg.Heartbeat(context.Background(), &workerpb.HeartbeatRequest{WorkerId: "w-1"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Ok {
		t.Error("expected ok=false on follower")
	}
	if resp.LeaderAddr != "cp-aws-1:50051" {
		t.Errorf("expected redirect to cp-aws-1:50051, got %s", resp.LeaderAddr)
	}
	if len(mr.appliedCmds) != 0 {
		t.Error("follower must not call Apply")
	}
}

func TestHeartbeat_ResetsMarkedOffline(t *testing.T) {
	reg, _ := newLeaderRegistry()
	// Seed a tracker that was previously marked offline
	reg.mu.Lock()
	reg.trackers["w-1"] = &HeartbeatTracker{
		LastSeen:      time.Now().Add(-20 * time.Second),
		MarkedOffline: true,
	}
	reg.mu.Unlock()

	_, err := reg.Heartbeat(context.Background(), &workerpb.HeartbeatRequest{WorkerId: "w-1"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	reg.mu.Lock()
	marked := reg.trackers["w-1"].MarkedOffline
	reg.mu.Unlock()
	if marked {
		t.Error("MarkedOffline should be reset to false after a heartbeat")
	}
}

// ── checkHeartbeats tests ────────────────────────────────────────────────────

func TestCheckHeartbeats_MarksOffline(t *testing.T) {
	reg, mr := newLeaderRegistry()
	reg.mu.Lock()
	reg.trackers["w-stale"] = &HeartbeatTracker{
		LastSeen:      time.Now().Add(-20 * time.Second), // well past the 15 s timeout
		MarkedOffline: false,
	}
	reg.mu.Unlock()

	reg.checkHeartbeats()

	if len(mr.appliedCmds) != 1 {
		t.Fatalf("expected 1 Apply call, got %d", len(mr.appliedCmds))
	}
	cmd := lastAppliedCommand(t, mr)
	if cmd.Type != internalraft.CmdUpdateWorkerStatus {
		t.Errorf("expected CmdUpdateWorkerStatus, got %s", cmd.Type)
	}
	var p internalraft.UpdateWorkerStatusPayload
	if err := json.Unmarshal(cmd.Payload, &p); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if p.ID != "w-stale" || p.Status != "offline" {
		t.Errorf("unexpected payload: %+v", p)
	}
}

func TestCheckHeartbeats_NoSpam(t *testing.T) {
	reg, mr := newLeaderRegistry()
	reg.mu.Lock()
	reg.trackers["w-stale"] = &HeartbeatTracker{
		LastSeen:      time.Now().Add(-20 * time.Second),
		MarkedOffline: false,
	}
	reg.mu.Unlock()

	reg.checkHeartbeats()
	reg.checkHeartbeats() // second call — must not apply again

	if len(mr.appliedCmds) != 1 {
		t.Errorf("expected exactly 1 Apply (no spam), got %d", len(mr.appliedCmds))
	}
}

func TestCheckHeartbeats_NotLeader(t *testing.T) {
	reg, mr := newFollowerRegistry()
	reg.mu.Lock()
	reg.trackers["w-stale"] = &HeartbeatTracker{
		LastSeen:      time.Now().Add(-20 * time.Second),
		MarkedOffline: false,
	}
	reg.mu.Unlock()

	reg.checkHeartbeats()

	if len(mr.appliedCmds) != 0 {
		t.Error("follower must not call Apply in checkHeartbeats")
	}
}

func TestCheckHeartbeats_FreshWorkerNotMarked(t *testing.T) {
	reg, mr := newLeaderRegistry()
	reg.mu.Lock()
	reg.trackers["w-fresh"] = &HeartbeatTracker{
		LastSeen:      time.Now(), // just heartbeated
		MarkedOffline: false,
	}
	reg.mu.Unlock()

	reg.checkHeartbeats()

	if len(mr.appliedCmds) != 0 {
		t.Error("fresh worker must not be marked offline")
	}
}

// ── raftAddrToGRPC tests ─────────────────────────────────────────────────────

func TestRaftAddrToGRPC(t *testing.T) {
	reg := NewAgentRegistry(&mockRaft{}, "50051")
	cases := []struct {
		in, want string
	}{
		{"cp-aws-1:7000", "cp-aws-1:50051"},
		{"cp-gcp-1:7000", "cp-gcp-1:50051"},
		{"", ""},
		{"bad-addr", ""},
	}
	for _, c := range cases {
		got := reg.raftAddrToGRPC(c.in)
		if got != c.want {
			t.Errorf("raftAddrToGRPC(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestRaftAddrToGRPC_IPFallsBackToLeaderID(t *testing.T) {
	// When the Raft transport resolves hostnames to IPs, Leader() returns an
	// IP-based address. raftAddrToGRPC must use the leader's server ID instead.
	reg := NewAgentRegistry(&mockRaft{leaderID: "cp-gcp-1"}, "50051")
	got := reg.raftAddrToGRPC("10.20.0.11:7000")
	want := "cp-gcp-1:50051"
	if got != want {
		t.Errorf("raftAddrToGRPC(%q) = %q, want %q", "10.20.0.11:7000", got, want)
	}
}

func TestRaftAddrToGRPC_IPWithNoLeaderID(t *testing.T) {
	// If leader ID is unknown, return empty string rather than an IP-based addr.
	reg := NewAgentRegistry(&mockRaft{leaderID: ""}, "50051")
	got := reg.raftAddrToGRPC("10.20.0.11:7000")
	if got != "" {
		t.Errorf("expected empty string when leaderID unknown, got %q", got)
	}
}
