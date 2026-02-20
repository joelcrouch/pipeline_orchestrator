package raft

import (
	"testing"
	"time"

	"github.com/hashicorp/go-hclog"
	hashiraft "github.com/hashicorp/raft"
)

func TestNodeInit(t *testing.T) {
	cfg := Config{
		NodeID:    "test-node-1",
		DataDir:   t.TempDir(),
		Bootstrap: true,
	}

	_, transport := hashiraft.NewInmemTransport(hashiraft.ServerAddress("test-node-1"))

	node, err := newRaftNodeWithTransport(cfg, &PipelineFSM{}, transport, hclog.NewNullLogger())
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

	addrs := []hashiraft.ServerAddress{"node-1", "node-2", "node-3"}
	peers := []string{"node-1", "node-2", "node-3"}

	_, trans1 := hashiraft.NewInmemTransport(addrs[0])
	_, trans2 := hashiraft.NewInmemTransport(addrs[1])
	_, trans3 := hashiraft.NewInmemTransport(addrs[2])

	// Wire the transports so they can reach each other.
	trans1.Connect(addrs[1], trans2)
	trans1.Connect(addrs[2], trans3)
	trans2.Connect(addrs[0], trans1)
	trans2.Connect(addrs[2], trans3)
	trans3.Connect(addrs[0], trans1)
	trans3.Connect(addrs[1], trans2)

	logger := hclog.NewNullLogger()
	mkNode := func(id string, trans hashiraft.Transport) *RaftNode {
		t.Helper()
		n, err := newRaftNodeWithTransport(Config{
			NodeID:    id,
			DataDir:   t.TempDir(),
			Bootstrap: true,
			Peers:     peers,
		}, &PipelineFSM{}, trans, logger)
		if err != nil {
			t.Fatalf("create node %s: %v", id, err)
		}
		return n
	}

	nodes := []*RaftNode{
		mkNode("node-1", trans1),
		mkNode("node-2", trans2),
		mkNode("node-3", trans3),
	}
	defer func() {
		for _, n := range nodes {
			if err := n.Shutdown(); err != nil {
				t.Logf("shutdown: %v", err)
			}
		}
	}()

	// Wait for exactly one leader to emerge.
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		leaders := 0
		for _, n := range nodes {
			if n.State() == hashiraft.Leader {
				leaders++
			}
		}
		if leaders == 1 {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}

	leaders := 0
	for _, n := range nodes {
		if n.State() == hashiraft.Leader {
			leaders++
		}
	}
	if leaders != 1 {
		t.Fatalf("expected exactly 1 leader, got %d", leaders)
	}
	t.Logf("states — node-1:%s node-2:%s node-3:%s",
		nodes[0].State(), nodes[1].State(), nodes[2].State())
}
