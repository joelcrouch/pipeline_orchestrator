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
)

func main() {
	//logger
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
	slog.SetDefault(logger)

	//env config
	grpcAddr := envOr("GRPC_ADDR", ":50051")
	httpAddr := envOr("HTTP_ADDR", ":8080")
	nodeID := envOr("NODE_ID", "cp-unknown")

	slog.Info("control plane starting",
		"node_id", nodeID,
		"grpc_addr", grpcAddr,
		"http_addr", httpAddr,
	)

	// gRPC server
	lis, err := net.Listen("tcp", grpcAddr)
	if err != nil {
		slog.Error("failed to listen", "error", err)
		os.Exit(1)
	}

	grpcServer := grpc.NewServer()

	// Health check service — used by Docker HEALTHCHECK and workers
	healthSvc := health.NewServer()
	grpc_health_v1.RegisterHealthServer(grpcServer, healthSvc)
	healthSvc.SetServingStatus("", grpc_health_v1.HealthCheckResponse_SERVING)

	// Reflection — makes grpcurl work out of the box for debugging
	reflection.Register(grpcServer)

	// HTTP debug server
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprintf(w, `{"status":"ok","node_id":"%s"}`, nodeID)
	})
	mux.HandleFunc("/cluster-state", func(w http.ResponseWriter, r *http.Request) {
		// Placeholder — will be populated by agent registry in S1.4
		_, _ = fmt.Fprintf(w, `{"node_id":"%s","workers":[]}`, nodeID)
	})
	httpServer := &http.Server{
		Addr:    httpAddr,
		Handler: mux,
	}

	// Start servers
	go func() {
		slog.Info("gRPC server listening", "addr", grpcAddr)
		if err := grpcServer.Serve(lis); err != nil {
			slog.Error("gRPC server error", "error", err)
		}
	}()

	go func() {
		slog.Info("HTTP debug server listening", "addr", httpAddr)
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("HTTP server error", "error", err)
		}
	}()

	// Graceful shutdown
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	slog.Info("shutting down...")
	grpcServer.GracefulStop()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := httpServer.Shutdown(ctx); err != nil {
		slog.Error("http server shutdown error", "error", err)
	}
	slog.Info("shutdown complete")
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
