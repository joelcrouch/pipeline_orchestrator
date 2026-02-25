package raft

import (
	"bytes"
	"fmt"
	"io"
	"testing"
	"time"

	"github.com/hashicorp/go-hclog"
	hashiraft "github.com/hashicorp/raft"
)

// ── helpers ────────────────────────────────────────────────────────────────

func mustMarshalCmd(t *testing.T, typ CommandType, payload interface{}) []byte {
	t.Helper()
	b, err := MarshalCommand(typ, payload)
	if err != nil {
		t.Fatalf("MarshalCommand: %v", err)
	}
	return b
}

func waitForLeader(t *testing.T, nodes []*RaftNode, timeout time.Duration) int {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		for i, n := range nodes {
			if n.State() == hashiraft.Leader {
				return i
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("no leader elected within %s", timeout)
	return -1
}

func makeCluster(t *testing.T, n int) ([]*RaftNode, []*PipelineFSM, []*hashiraft.InmemTransport,
	[]hashiraft.ServerAddress) {
	t.Helper()
	addrs := make([]hashiraft.ServerAddress, n)
	peers := make([]string, n)
	for i := 0; i < n; i++ {
		addrs[i] = hashiraft.ServerAddress(fmt.Sprintf("node-%d", i+1))
		peers[i] = fmt.Sprintf("node-%d", i+1)
	}

	trans := make([]*hashiraft.InmemTransport, n)
	for i, addr := range addrs {
		_, trans[i] = hashiraft.NewInmemTransport(addr)
	}
	for i := range trans {
		for j := range trans {
			if i != j {
				trans[i].Connect(addrs[j], trans[j])
			}
		}
	}

	logger := hclog.NewNullLogger()
	nodes := make([]*RaftNode, n)
	fsms := make([]*PipelineFSM, n)
	for i := 0; i < n; i++ {
		fsms[i] = NewPipelineFSM()
		var err error
		nodes[i], err = newRaftNodeWithTransport(Config{
			NodeID:    peers[i],
			DataDir:   t.TempDir(),
			Bootstrap: true,
			Peers:     peers,
		}, fsms[i], trans[i], logger)
		if err != nil {
			t.Fatalf("create node %d: %v", i+1, err)
		}
	}

	t.Cleanup(func() {
		for _, node := range nodes {
			_ = node.Shutdown()
		}
	})

	return nodes, fsms, trans, addrs
}

// ── S1.1 tests (unchanged) ─────────────────────────────────────────────────

func TestNodeInit(t *testing.T) {
	cfg := Config{
		NodeID:    "test-node-1",
		DataDir:   t.TempDir(),
		Bootstrap: true,
	}
	_, transport := hashiraft.NewInmemTransport(hashiraft.ServerAddress("test-node-1"))
	node, err := newRaftNodeWithTransport(cfg, NewPipelineFSM(), transport, hclog.NewNullLogger())
	if err != nil {
		t.Fatalf("newRaftNodeWithTransport: %v", err)
	}
	defer func() {
		if err := node.Shutdown(); err != nil {
			t.Logf("shutdown: %v", err)
		}
	}()

	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if node.State() == hashiraft.Leader {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if got := node.State(); got != hashiraft.Leader {
		t.Fatalf("expected Leader, got %s", got)
	}
	t.Logf("node reached state: %s", node.State())
}

func TestBootstrapMultiNode(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping multi-node bootstrap test in short mode")
	}
	nodes, _, _, _ := makeCluster(t, 3)
	leaderIdx := waitForLeader(t, nodes, 15*time.Second)

	leaders := 0
	for _, n := range nodes {
		if n.State() == hashiraft.Leader {
			leaders++
		}
	}
	if leaders != 1 {
		t.Fatalf("expected exactly 1 leader, got %d", leaders)
	}
	t.Logf("states — node-1:%s node-2:%s node-3:%s (leader=node-%d)",
		nodes[0].State(), nodes[1].State(), nodes[2].State(), leaderIdx+1)
}

// ── S1.3 tests ─────────────────────────────────────────────────────────────

func TestFSMApply(t *testing.T) {
	fsm := NewPipelineFSM()

	cmd := mustMarshalCmd(t, CmdRegisterWorker, RegisterWorkerPayload{
		ID: "w-1", Address: "10.10.0.20:8081", CloudTag: "aws",
	})
	result := fsm.Apply(&hashiraft.Log{
		Index: 1, Term: 1, Type: hashiraft.LogCommand, Data: cmd,
	})
	if result != nil {
		t.Fatalf("unexpected Apply result: %v", result)
	}

	w := fsm.GetWorker("w-1")
	if w == nil {
		t.Fatal("worker w-1 not found after Apply")
	}
	if w.CloudTag != "aws" || w.Status != "online" {
		t.Errorf("unexpected worker state: %+v", w)
	}

	// Update status
	cmd = mustMarshalCmd(t, CmdUpdateWorkerStatus, UpdateWorkerStatusPayload{
		ID: "w-1", Status: "offline",
	})
	fsm.Apply(&hashiraft.Log{Index: 2, Term: 1, Type: hashiraft.LogCommand, Data: cmd})

	if w := fsm.GetWorker("w-1"); w.Status != "offline" {
		t.Errorf("expected status offline, got %s", w.Status)
	}
}

func TestFSMSnapshotRestore(t *testing.T) {
	fsm := NewPipelineFSM()

	for i, tag := range []string{"aws", "gcp", "azure"} {
		cmd := mustMarshalCmd(t, CmdRegisterWorker, RegisterWorkerPayload{
			ID:       fmt.Sprintf("w-%d", i),
			Address:  fmt.Sprintf("10.%d.0.20:8081", (i+1)*10),
			CloudTag: tag,
		})
		fsm.Apply(&hashiraft.Log{Index: uint64(i + 1), Term: 1, Type: hashiraft.LogCommand, Data: cmd})
	}

	if n := len(fsm.Workers()); n != 3 {
		t.Fatalf("expected 3 workers before snapshot, got %d", n)
	}

	// Snapshot
	snap, err := fsm.Snapshot()
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	sink := &testSnapshotSink{buf: &bytes.Buffer{}}
	if err := snap.Persist(sink); err != nil {
		t.Fatalf("Persist: %v", err)
	}
	snap.Release()

	// Restore into a fresh FSM
	fsm2 := NewPipelineFSM()
	if err := fsm2.Restore(io.NopCloser(bytes.NewReader(sink.buf.Bytes()))); err != nil {
		t.Fatalf("Restore: %v", err)
	}

	restored := fsm2.Workers()
	if len(restored) != 3 {
		t.Fatalf("expected 3 workers after restore, got %d", len(restored))
	}
	for i := 0; i < 3; i++ {
		id := fmt.Sprintf("w-%d", i)
		if _, ok := restored[id]; !ok {
			t.Errorf("worker %s missing after restore", id)
		}
	}
	t.Logf("snapshot/restore verified: %d workers consistent", len(restored))
}

func TestReplicationToFollowers(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping replication test in short mode")
	}

	nodes, fsms, _, _ := makeCluster(t, 3)
	leaderIdx := waitForLeader(t, nodes, 15*time.Second)

	cmd := mustMarshalCmd(t, CmdRegisterWorker, RegisterWorkerPayload{
		ID: "repl-worker", Address: "10.10.0.20:8081", CloudTag: "aws",
	})
	if err := nodes[leaderIdx].Apply(cmd, 2*time.Second); err != nil {
		t.Fatalf("Apply on leader: %v", err)
	}

	// All 3 FSMs must reflect the entry within 500ms
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		allHave := true
		for _, fsm := range fsms {
			if fsm.GetWorker("repl-worker") == nil {
				allHave = false
				break
			}
		}
		if allHave {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	for i, fsm := range fsms {
		w := fsm.GetWorker("repl-worker")
		if w == nil {
			t.Errorf("node-%d: worker 'repl-worker' not replicated within 500ms", i+1)
		} else {
			t.Logf("node-%d: ✓ cloud=%s status=%s", i+1, w.CloudTag, w.Status)
		}
	}
}

func TestMajorityQuorum(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping quorum test in short mode")
	}

	nodes, fsms, trans, addrs := makeCluster(t, 3)
	leaderIdx := waitForLeader(t, nodes, 15*time.Second)

	// Isolate one follower
	isolatedIdx := (leaderIdx + 1) % 3
	trans[isolatedIdx].DisconnectAll()
	for i := range trans {
		if i != isolatedIdx {
			trans[i].Disconnect(addrs[isolatedIdx])
		}
	}

	// Apply should commit with 2/3 majority
	cmd := mustMarshalCmd(t, CmdRegisterWorker, RegisterWorkerPayload{
		ID: "quorum-worker", Address: "10.10.0.20:8081", CloudTag: "aws",
	})
	if err := nodes[leaderIdx].Apply(cmd, 3*time.Second); err != nil {
		t.Fatalf("Apply with one node isolated: %v", err)
	}

	// Apply returning means the entry is committed; wait for connected followers
	// to finish their async FSM dispatch before asserting.
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		allHave := true
		for i, fsm := range fsms {
			if i == isolatedIdx {
				continue
			}
			if fsm.GetWorker("quorum-worker") == nil {
				allHave = false
				break
			}
		}
		if allHave {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	// The two connected nodes must have it
	for i, fsm := range fsms {
		if i == isolatedIdx {
			continue
		}
		if fsm.GetWorker("quorum-worker") == nil {
			t.Errorf("node-%d (connected): missing worker after majority commit", i+1)
		}
	}
	t.Logf("node-%d (isolated): has entry=%v", isolatedIdx+1,
		fsms[isolatedIdx].GetWorker("quorum-worker") != nil)

	// Reconnect isolated node
	for i := range trans {
		if i != isolatedIdx {
			trans[isolatedIdx].Connect(addrs[i], trans[i])
			trans[i].Connect(addrs[isolatedIdx], trans[isolatedIdx])
		}
	}

	// Isolated node should catch up
	deadline = time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if fsms[isolatedIdx].GetWorker("quorum-worker") != nil {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if fsms[isolatedIdx].GetWorker("quorum-worker") == nil {
		t.Errorf("node-%d (reconnected): failed to catch up", isolatedIdx+1)
	} else {
		t.Logf("node-%d (reconnected): caught up ✓", isolatedIdx+1)
	}
}
