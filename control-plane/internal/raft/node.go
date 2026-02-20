// Package raft implements a Raft consensus algorithm for the
// distributed control plane. Implementation begins in Sprint 1.
package raft

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"time"

	"github.com/hashicorp/go-hclog"
	hashiraft "github.com/hashicorp/raft"
	raftboltdb "github.com/hashicorp/raft-boltdb"
)

type Config struct {
	Bootstrap bool
	DataDir   string
	NodeID    string
	Peers     []string //host:port entries fro all cluster members, including self
	RaftAddr  string
}

// raftNode wraps hcorp/raft with botldb persistnce
type RaftNode struct {
	raft *hashiraft.Raft
	cfg  Config
}

// newRaftNode makes/starts a raft node with tcp transport
// boltdb (bdb) files stored in cfg.DataDir -mount /data/raft/as a docker volume
func NewRaftNode(cfg Config, fsm hashiraft.FSM) (*RaftNode, error) {
	_, port, err := net.SplitHostPort(cfg.RaftAddr)
	if err != nil {
		return nil, fmt.Errorf("parse raft addr %q: %w", cfg.RaftAddr, err)
	}

	// Advertise the configured hostname:port to peers.
	// Bind to all interfaces so every Docker network is reachable.
	advertise, err := net.ResolveTCPAddr("tcp", cfg.RaftAddr)
	if err != nil {
		return nil, fmt.Errorf("resolve raft addr %q: %w", cfg.RaftAddr, err)
	}

	logger := hclog.New(&hclog.LoggerOptions{Name: "raft", Level: hclog.Info})
	transport, err := hashiraft.NewTCPTransportWithLogger(
		":"+port, advertise, 3, 10*time.Second, logger,
	)
	if err != nil {
		return nil, fmt.Errorf("tcp transport: %w", err)
	}
	return newRaftNodeWithTransport(cfg, fsm, transport, logger)
}

// newRaftNodeWithTransport is the internal constructor — used by NewRaftNode and tests.
func newRaftNodeWithTransport(cfg Config, fsm hashiraft.FSM, transport hashiraft.Transport,
	logger hclog.Logger) (*RaftNode, error) {
	if err := os.MkdirAll(cfg.DataDir, 0o750); err != nil {
		return nil, fmt.Errorf("create data dir: %w", err)
	}

	// One BoltDB file serves as both LogStore and StableStore.
	boltPath := filepath.Join(cfg.DataDir, "raft.db")
	boltStore, err := raftboltdb.NewBoltStore(boltPath)
	if err != nil {
		return nil, fmt.Errorf("bolt store: %w", err)
	}

	snapDir := filepath.Join(cfg.DataDir, "snapshots")
	snapStore, err := hashiraft.NewFileSnapshotStore(snapDir, 3, os.Stderr)
	if err != nil {
		return nil, fmt.Errorf("snapshot store: %w", err)
	}

	raftCfg := hashiraft.DefaultConfig()
	raftCfg.LocalID = hashiraft.ServerID(cfg.NodeID)
	raftCfg.HeartbeatTimeout = 500 * time.Millisecond
	raftCfg.ElectionTimeout = 1000 * time.Millisecond
	raftCfg.CommitTimeout = 50 * time.Millisecond
	raftCfg.Logger = logger

	r, err := hashiraft.NewRaft(raftCfg, fsm, boltStore, boltStore, snapStore, transport)
	if err != nil {
		return nil, fmt.Errorf("new raft: %w", err)
	}

	if cfg.Bootstrap {
		hasState, err := hashiraft.HasExistingState(boltStore, boltStore, snapStore)
		if err != nil {
			return nil, fmt.Errorf("check existing state: %w", err)
		}
		if !hasState {
			bootstrapCfg := hashiraft.Configuration{
				Servers: peersToServers(cfg.Peers, cfg.NodeID, transport.LocalAddr()),
			}
			if f := r.BootstrapCluster(bootstrapCfg); f.Error() != nil {
				return nil, fmt.Errorf("bootstrap cluster: %w", f.Error())
			}
		}
	}

	return &RaftNode{raft: r, cfg: cfg}, nil
}

// peersToServers converts a Peers slice into a raft.Configuration server list.
// If peers is empty it falls back to a single-node entry (used in tests).
func peersToServers(peers []string, nodeID string, localAddr hashiraft.ServerAddress) []hashiraft.Server {
	if len(peers) == 0 {
		return []hashiraft.Server{{
			ID:      hashiraft.ServerID(nodeID),
			Address: localAddr,
		}}
	}
	servers := make([]hashiraft.Server, 0, len(peers))
	for _, peer := range peers {
		id := peer
		if host, _, err := net.SplitHostPort(peer); err == nil {
			id = host // "cp-aws-1:7000" → ServerID "cp-aws-1"
		}
		servers = append(servers, hashiraft.Server{
			ID:      hashiraft.ServerID(id),
			Address: hashiraft.ServerAddress(peer),
		})
	}
	return servers
}

// State returns the current Raft state of this node.
func (n *RaftNode) State() hashiraft.RaftState {
	return n.raft.State()
}

// StateFloat returns state as a float64 for Prometheus: 0=Follower 1=Candidate 2=Leader 3=Shutdown.
func (n *RaftNode) StateFloat() float64 {
	switch n.raft.State() {
	case hashiraft.Follower:
		return 0
	case hashiraft.Candidate:
		return 1
	case hashiraft.Leader:
		return 2
	default:
		return 3
	}
}

// Stats returns the raw hashicorp/raft stats map.
func (n *RaftNode) Stats() map[string]string {
	return n.raft.Stats()
}

// Leader returns the address of the current leader, or empty string if unknown.
func (n *RaftNode) Leader() string {
	return string(n.raft.Leader())
}

// Raft returns the underlying hashicorp/raft instance.
func (n *RaftNode) Raft() *hashiraft.Raft {
	return n.raft
}

// Shutdown cleanly stops the Raft node.
func (n *RaftNode) Shutdown() error {
	return n.raft.Shutdown().Error()
}
