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
)

func main() {
	httpAddr := envOrDefault("AH_ADDR", ":8080")
	grpcAddr := envOrDefault("AH_GRPC_ADDR", ":9090")

	store := inventory.NewMemoryStore()
	service := resolver.NewService(store)
	handler := resolver.NewHTTPHandler(service)
	httpServer := &http.Server{
		Addr:              httpAddr,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
	}
	grpcServer := grpc.NewServer()
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
	grpcServer.GracefulStop()
}

func envOrDefault(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
