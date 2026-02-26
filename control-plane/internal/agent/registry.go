// Package agent manages worker node registration and heartbeat tracking.
package agent

import (
	"context"
	"log/slog"
	"net"
	"sync"
	"time"

	hashiraft "github.com/hashicorp/raft"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	workerpb "github.com/joelcrouch/pipeline-orchestrator/control-plane/internal/gen/worker"
	internalraft "github.com/joelcrouch/pipeline-orchestrator/control-plane/internal/raft"
)

const (
	heartbeatTimeout = 15 * time.Second // 3 missed beats at 5 s interval
	monitorInterval  = 5 * time.Second
	raftApplyTimeout = 2 * time.Second
)

// RaftApplier is the subset of RaftNode that AgentRegistry needs.
// The narrow interface keeps the registry testable without a real Raft cluster.
type RaftApplier interface {
	Apply(cmd []byte, timeout time.Duration) error
	Leader() string
	LeaderID() string
	State() hashiraft.RaftState
}

// HeartbeatTracker holds ephemeral (non-Raft) liveness state for one worker.
// It lives only in memory on the current leader — the authoritative worker status
// is always the PipelineFSM replicated via Raft.
type HeartbeatTracker struct {
	LastSeen      time.Time
	MarkedOffline bool // prevents duplicate Raft Apply calls for the same offline event
}

// AgentRegistry implements workerpb.WorkerServiceServer.
// It handles RegisterWorker and Heartbeat RPCs, enforces leader-only writes,
// and runs a background goroutine to mark workers offline after missed heartbeats.
type AgentRegistry struct {
	workerpb.UnimplementedWorkerServiceServer

	mu       sync.Mutex
	trackers map[string]*HeartbeatTracker

	raft     RaftApplier
	grpcPort string // e.g. "50051" — used to build the gRPC redirect addr from a Raft addr
}

// NewAgentRegistry creates an AgentRegistry. Call Start to activate the monitor.
// grpcPort is the port the gRPC server listens on (e.g. "50051").
func NewAgentRegistry(raft RaftApplier, grpcPort string) *AgentRegistry {
	return &AgentRegistry{
		trackers: make(map[string]*HeartbeatTracker),
		raft:     raft,
		grpcPort: grpcPort,
	}
}

// Start launches the background heartbeat-monitor goroutine.
// ctx should be cancelled on graceful shutdown.
func (r *AgentRegistry) Start(ctx context.Context) {
	go r.monitorLoop(ctx)
}

// RegisterWorker handles a worker's initial registration RPC.
// Followers return a redirect; only the leader writes to Raft.
func (r *AgentRegistry) RegisterWorker(
	ctx context.Context,
	req *workerpb.RegisterWorkerRequest,
) (*workerpb.RegisterWorkerResponse, error) {

	if r.raft.State() != hashiraft.Leader {
		leaderGRPC := r.raftAddrToGRPC(r.raft.Leader())
		slog.Info("RegisterWorker: not leader, redirecting",
			"leader_grpc", leaderGRPC, "worker_id", req.WorkerId)
		return &workerpb.RegisterWorkerResponse{
			Ok:         false,
			LeaderAddr: leaderGRPC,
		}, nil
	}

	cmd, err := internalraft.MarshalCommand(internalraft.CmdRegisterWorker,
		internalraft.RegisterWorkerPayload{
			ID:       req.WorkerId,
			Address:  req.Address,
			CloudTag: req.CloudTag,
		})
	if err != nil {
		return nil, status.Errorf(codes.Internal, "marshal command: %v", err)
	}

	if err := r.raft.Apply(cmd, raftApplyTimeout); err != nil {
		return nil, status.Errorf(codes.Internal, "raft apply: %v", err)
	}

	r.mu.Lock()
	r.trackers[req.WorkerId] = &HeartbeatTracker{
		LastSeen:      time.Now().UTC(),
		MarkedOffline: false,
	}
	r.mu.Unlock()

	slog.Info("worker registered",
		"worker_id", req.WorkerId,
		"cloud", req.CloudTag,
		"address", req.Address)
	return &workerpb.RegisterWorkerResponse{Ok: true}, nil
}

// Heartbeat handles a periodic liveness ping from a registered worker.
// Followers return a redirect; only the leader updates the tracker.
func (r *AgentRegistry) Heartbeat(
	ctx context.Context,
	req *workerpb.HeartbeatRequest,
) (*workerpb.HeartbeatResponse, error) {

	if r.raft.State() != hashiraft.Leader {
		leaderGRPC := r.raftAddrToGRPC(r.raft.Leader())
		return &workerpb.HeartbeatResponse{
			Ok:         false,
			LeaderAddr: leaderGRPC,
		}, nil
	}

	r.mu.Lock()
	t, exists := r.trackers[req.WorkerId]
	if !exists {
		// Worker heartbeating without having registered on this leader
		// (can happen after a leader failover). Create a tracker so the
		// monitor doesn't incorrectly flag it as stale.
		t = &HeartbeatTracker{}
		r.trackers[req.WorkerId] = t
	}
	t.LastSeen = time.Now().UTC()
	t.MarkedOffline = false // reset on any successful heartbeat
	r.mu.Unlock()

	slog.Debug("heartbeat received", "worker_id", req.WorkerId)
	return &workerpb.HeartbeatResponse{Ok: true}, nil
}

// monitorLoop ticks every monitorInterval and checks for stale workers.
func (r *AgentRegistry) monitorLoop(ctx context.Context) {
	ticker := time.NewTicker(monitorInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			r.checkHeartbeats()
		}
	}
}

// checkHeartbeats marks workers offline via Raft when their heartbeat has been
// absent for longer than heartbeatTimeout. Only runs on the leader.
func (r *AgentRegistry) checkHeartbeats() {
	if r.raft.State() != hashiraft.Leader {
		return
	}

	now := time.Now().UTC()

	r.mu.Lock()
	var stale []string
	for id, t := range r.trackers {
		if !t.MarkedOffline && now.Sub(t.LastSeen) > heartbeatTimeout {
			t.MarkedOffline = true // set inside lock — prevents double-queueing
			stale = append(stale, id)
		}
	}
	r.mu.Unlock()

	// Apply offline commands outside the lock — Raft Apply can be slow.
	for _, id := range stale {
		slog.Warn("worker heartbeat timeout — marking offline", "worker_id", id)
		cmd, err := internalraft.MarshalCommand(internalraft.CmdUpdateWorkerStatus,
			internalraft.UpdateWorkerStatusPayload{ID: id, Status: "offline"})
		if err != nil {
			slog.Error("marshal offline command", "worker_id", id, "error", err)
			continue
		}
		if err := r.raft.Apply(cmd, raftApplyTimeout); err != nil {
			slog.Error("raft apply offline", "worker_id", id, "error", err)
			// Keep MarkedOffline=true — avoids spamming a struggling cluster.
			// The worker's re-registration will reset it.
		}
	}
}

// raftAddrToGRPC converts a Raft peer address (e.g. "cp-aws-1:7000") into
// the corresponding gRPC address (e.g. "cp-aws-1:50051") by replacing the port.
//
// When the Raft TCP transport resolves hostnames to IPs at startup, Leader()
// returns an IP-based address (e.g. "10.10.0.11:7000"). Workers on isolated
// Docker networks cannot route to IPs on other subnets, so we fall back to the
// leader's server ID (e.g. "cp-gcp-1") which Docker DNS resolves correctly on
// all networks.
func (r *AgentRegistry) raftAddrToGRPC(raftAddr string) string {
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
		id := r.raft.LeaderID()
		if id == "" {
			return ""
		}
		host = id
	}
	return net.JoinHostPort(host, r.grpcPort)
}
