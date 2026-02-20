package main

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	"google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/reflection"

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

	slog.Info("control plane starting",
		"node_id", nodeID,
		"grpc_addr", grpcAddr,
		"http_addr", httpAddr,
		"raft_addr", raftAddr,
		"raft_data_dir", raftDataDir,
		"raft_bootstrap", raftBootstrap,
	)

	// ── Raft node ────────────────────────────────────────────────
	raftNode, err := internalraft.NewRaftNode(internalraft.Config{
		NodeID:    nodeID,
		RaftAddr:  raftAddr,
		DataDir:   raftDataDir,
		Bootstrap: raftBootstrap,
	}, &internalraft.PipelineFSM{})
	if err != nil {
		slog.Error("failed to start raft node", "error", err)
		os.Exit(1)
	}
	slog.Info("raft node started", "state", raftNode.State().String())

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
	reflection.Register(grpcServer)

	// ── HTTP debug server ────────────────────────────────────────
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprintf(w, `{"status":"ok","node_id":"%s"}`, nodeID)
	})
	mux.HandleFunc("/cluster-state", func(w http.ResponseWriter, r *http.Request) {
		// Populated by agent registry in S1.4
		_, _ = fmt.Fprintf(w, `{"node_id":"%s","workers":[]}`, nodeID)
	})
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

// envOr returns the value of the environment variable key,
// or fallback if the variable is unset or empty.
func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// boolEnv returns true only when the environment variable is exactly "true".
func boolEnv(key string) bool {
	return os.Getenv(key) == "true"
}

// package main

// import (
// 	"context"
// 	"fmt"
// 	"log/slog"
// 	"net"
// 	"net/http"
// 	"os"
// 	"os/signal"
// 	"syscall"
// 	"time"

// 	"google.golang.org/grpc"
// 	"google.golang.org/grpc/health"
// 	"google.golang.org/grpc/health/grpc_health_v1"
// 	"google.golang.org/grpc/reflection"

// )

// func main() {
// 	//logger
// 	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
// 		Level: slog.LevelInfo,
// 	}))
// 	slog.SetDefault(logger)

// 	//env config
// 	grpcAddr := envOr("GRPC_ADDR", ":50051")
// 	httpAddr := envOr("HTTP_ADDR", ":8080")
// 	nodeID := envOr("NODE_ID", "cp-unknown")

// 	slog.Info("control plane starting",
// 		"node_id", nodeID,
// 		"grpc_addr", grpcAddr,
// 		"http_addr", httpAddr,
// 	)

// 	// gRPC server
// 	lis, err := net.Listen("tcp", grpcAddr)
// 	if err != nil {
// 		slog.Error("failed to listen", "error", err)
// 		os.Exit(1)
// 	}

// 	grpcServer := grpc.NewServer()

// 	// Health check service — used by Docker HEALTHCHECK and workers
// 	healthSvc := health.NewServer()
// 	grpc_health_v1.RegisterHealthServer(grpcServer, healthSvc)
// 	healthSvc.SetServingStatus("", grpc_health_v1.HealthCheckResponse_SERVING)

// 	// Reflection — makes grpcurl work out of the box for debugging
// 	reflection.Register(grpcServer)

// 	// HTTP debug server
// 	mux := http.NewServeMux()
// 	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
// 		_, _ = fmt.Fprintf(w, `{"status":"ok","node_id":"%s"}`, nodeID)
// 	})
// 	mux.HandleFunc("/cluster-state", func(w http.ResponseWriter, r *http.Request) {
// 		// Placeholder — will be populated by agent registry in S1.4
// 		_, _ = fmt.Fprintf(w, `{"node_id":"%s","workers":[]}`, nodeID)
// 	})
// 	httpServer := &http.Server{
// 		Addr:    httpAddr,
// 		Handler: mux,
// 	}

// 	// Start servers
// 	go func() {
// 		slog.Info("gRPC server listening", "addr", grpcAddr)
// 		if err := grpcServer.Serve(lis); err != nil {
// 			slog.Error("gRPC server error", "error", err)
// 		}
// 	}()

// 	go func() {
// 		slog.Info("HTTP debug server listening", "addr", httpAddr)
// 		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
// 			slog.Error("HTTP server error", "error", err)
// 		}
// 	}()

// 	// Graceful shutdown
// 	quit := make(chan os.Signal, 1)
// 	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
// 	<-quit

// 	slog.Info("shutting down...")
// 	grpcServer.GracefulStop()

// 	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
// 	defer cancel()
// 	if err := httpServer.Shutdown(ctx); err != nil {
// 		slog.Error("http server shutdown error", "error", err)
// 	}
// 	slog.Info("shutdown complete")
// }

// func envOr(key, fallback string) string {
// 	if v := os.Getenv(key); v != "" {
// 		return v
// 	}
// 	return fallback
// }
