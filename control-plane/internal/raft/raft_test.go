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

	// InmemTransport avoids real TCP ports in unit tests.
	_, transport := hashiraft.NewInmemTransport(hashiraft.ServerAddress("test-node-1"))

	node, err := newRaftNodeWithTransport(cfg, &PipelineFSM{}, transport,
		hclog.NewNullLogger())
	if err != nil {
		t.Fatalf("newRaftNodeWithTransport: %v", err)
	}
	defer func() {
		if err := node.Shutdown(); err != nil {
			t.Logf("shutdown: %v", err)
		}
	}()

	// A single-node cluster should elect itself leader quickly.
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
