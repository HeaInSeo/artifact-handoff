package main

import (
	"context"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/HeaInSeo/artifact-handoff/pkg/inventory"
	"github.com/HeaInSeo/artifact-handoff/pkg/resolver"
	"google.golang.org/grpc"
	"google.golang.org/grpc/keepalive"
)

func main() {
	httpAddr := envOrDefault("AH_ADDR", ":8080")
	grpcAddr := envOrDefault("AH_GRPC_ADDR", ":9090")
	storeDSN := envOrDefault("AH_STORE_DSN", "memory")
	if storeDSN == "memory" || storeDSN == "" {
		log.Printf("WARNING: AH_STORE_DSN not set or is 'memory' — data will not survive restarts")
	}

	store, closeStore, err := inventory.OpenStore(storeDSN)
	if err != nil {
		log.Fatal(err)
	}
	defer closeStore()

	service, err := resolver.NewService(store)
	if err != nil {
		log.Fatal(err)
	}
	handler := resolver.NewHTTPHandler(service)
	httpServer := &http.Server{
		Addr:              httpAddr,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
	}
	grpcServer := grpc.NewServer(
		grpc.MaxRecvMsgSize(4*1024*1024),
		grpc.KeepaliveParams(keepalive.ServerParameters{
			MaxConnectionIdle: 5 * time.Minute,
			Time:              2 * time.Minute,
			Timeout:           20 * time.Second,
		}),
		grpc.KeepaliveEnforcementPolicy(keepalive.EnforcementPolicy{
			MinTime:             30 * time.Second,
			PermitWithoutStream: true,
		}),
	)
	resolver.RegisterGRPCService(grpcServer, service)
	grpcListener, err := net.Listen("tcp", grpcAddr)
	if err != nil {
		log.Fatal(err)
	}

	errCh := make(chan error, 2)
	go func() {
		log.Printf("artifact-handoff resolver starting http server on %s", httpAddr)
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
	}()
	go func() {
		log.Printf("artifact-handoff resolver starting grpc server on %s", grpcAddr)
		if err := grpcServer.Serve(grpcListener); err != nil {
			errCh <- err
		}
	}()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	select {
	case sig := <-sigCh:
		log.Printf("artifact-handoff resolver shutting down on signal %s", sig)
	case err := <-errCh:
		log.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = httpServer.Shutdown(ctx)
	grpcDone := make(chan struct{})
	go func() { grpcServer.GracefulStop(); close(grpcDone) }()
	select {
	case <-grpcDone:
	case <-ctx.Done():
		grpcServer.Stop()
	}
}

func envOrDefault(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
