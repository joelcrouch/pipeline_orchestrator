package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	"google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/reflection"

	"github.com/joelcrouch/pipeline-orchestrator/control-plane/internal/agent"
	workerpb "github.com/joelcrouch/pipeline-orchestrator/control-plane/internal/gen/worker"
	"github.com/joelcrouch/pipeline-orchestrator/control-plane/internal/metrics"
	internalraft "github.com/joelcrouch/pipeline-orchestrator/control-plane/internal/raft"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
	slog.SetDefault(logger)

	nodeID := envOr("NODE_ID", "cp-unknown")
	grpcAddr := envOr("GRPC_ADDR", ":50051")
	httpAddr := envOr("HTTP_ADDR", ":8080")
	raftAddr := envOr("RAFT_ADDR", ":7000")
	raftDataDir := envOr("RAFT_DATA_DIR", "/data/raft")
	raftBootstrap := boolEnv("RAFT_BOOTSTRAP")
	raftPeers := splitCSV(os.Getenv("RAFT_PEERS"))

	slog.Info("control plane starting",
		"node_id", nodeID,
		"grpc_addr", grpcAddr,
		"http_addr", httpAddr,
		"raft_addr", raftAddr,
		"raft_data_dir", raftDataDir,
		"raft_bootstrap", raftBootstrap,
		"raft_peers", raftPeers,
	)

	// ── Raft node ────────────────────────────────────────────────
	fsm := internalraft.NewPipelineFSM()
	raftNode, err := internalraft.NewRaftNode(internalraft.Config{
		NodeID:    nodeID,
		RaftAddr:  raftAddr,
		DataDir:   raftDataDir,
		Bootstrap: raftBootstrap,
		Peers:     raftPeers,
	}, fsm)
	if err != nil {
		slog.Error("failed to start raft node", "error", err)
		os.Exit(1)
	}
	slog.Info("raft node started", "state", raftNode.State().String())

	// ── Agent registry (worker registration + heartbeat) ─────────
	_, grpcPort, err := net.SplitHostPort(grpcAddr)
	if err != nil {
		grpcPort = "50051"
	}
	registry := agent.NewAgentRegistry(raftNode, grpcPort)
	registryCtx, registryCancel := context.WithCancel(context.Background())
	registry.Start(registryCtx)

	// ── Prometheus stats polling (every 5s) ──────────────────────
	statsCtx, statsCancel := context.WithCancel(context.Background())
	go func() {
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		var lastTerm uint64
		for {
			select {
			case <-statsCtx.Done():
				return
			case <-ticker.C:
				stats := raftNode.Stats()
				metrics.RaftState.Set(raftNode.StateFloat())
				if termStr, ok := stats["term"]; ok {
					if term, err := strconv.ParseUint(termStr, 10, 64); err == nil {
						metrics.RaftTerm.Set(float64(term))
						if term > lastTerm && lastTerm > 0 {
							metrics.RaftElectionsTotal.Inc()
						}
						lastTerm = term
					}
				}
			}
		}
	}()

	// ── gRPC server ──────────────────────────────────────────────
	lis, err := net.Listen("tcp", grpcAddr)
	if err != nil {
		slog.Error("failed to listen on grpc addr", "addr", grpcAddr, "error", err)
		os.Exit(1)
	}
	grpcServer := grpc.NewServer()
	healthSvc := health.NewServer()
	grpc_health_v1.RegisterHealthServer(grpcServer, healthSvc)
	healthSvc.SetServingStatus("", grpc_health_v1.HealthCheckResponse_SERVING)
	workerpb.RegisterWorkerServiceServer(grpcServer, registry)
	reflection.Register(grpcServer)

	// ── HTTP debug server ────────────────────────────────────────
	mux := http.NewServeMux()

	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprintf(w, `{"status":"ok","node_id":"%s"}`, nodeID)
	})

	mux.HandleFunc("/raft-state", func(w http.ResponseWriter, r *http.Request) {
		stats := raftNode.Stats()
		term, ok := stats["term"]
		if !ok {
			term = "0"
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w,
			`{"node_id":"%s","state":"%s","leader":"%s","term":%s}`,
			nodeID, raftNode.State().String(), raftNode.Leader(), term,
		)
	})

	mux.HandleFunc("/cluster-state", func(w http.ResponseWriter, r *http.Request) {
		workers := fsm.Workers()
		list := make([]*internalraft.WorkerInfo, 0, len(workers))
		for _, info := range workers {
			list = append(list, info)
		}
		resp := struct {
			NodeID  string                     `json:"node_id"`
			State   string                     `json:"state"`
			Workers []*internalraft.WorkerInfo `json:"workers"`
		}{
			NodeID:  nodeID,
			State:   raftNode.State().String(),
			Workers: list,
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})

	mux.Handle("/metrics", promhttp.Handler())

	httpServer := &http.Server{Addr: httpAddr, Handler: mux}

	go func() {
		slog.Info("gRPC server listening", "addr", grpcAddr)
		if err := grpcServer.Serve(lis); err != nil {
			slog.Error("gRPC server error", "error", err)
		}
	}()
	go func() {
		slog.Info("HTTP server listening", "addr", httpAddr)
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("HTTP server error", "error", err)
		}
	}()

	// ── Graceful shutdown ────────────────────────────────────────
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	slog.Info("shutting down...")
	registryCancel()
	statsCancel()
	grpcServer.GracefulStop()

	if err := raftNode.Shutdown(); err != nil {
		slog.Error("raft shutdown error", "error", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := httpServer.Shutdown(ctx); err != nil {
		slog.Error("http server shutdown error", "error", err)
	}
	slog.Info("shutdown complete")
}

// envOr returns the value of key, or fallback if unset/empty.
func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// boolEnv returns true only when the env var is exactly "true".
func boolEnv(key string) bool {
	return os.Getenv(key) == "true"
}

// splitCSV splits a comma-separated string, returning nil for empty input.
func splitCSV(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(s, ",")
}
