package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	greeterv1 "github.com/cohavisi/utila-gRPC/api/greeter/v1"
	"github.com/cohavisi/utila-gRPC/internal/greeter"
	"google.golang.org/grpc"
	"google.golang.org/grpc/admin"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/health"
	healthv1 "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/xds"
)

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))
	address := flag.String("listen", "0.0.0.0:50051", "application listen address")
	healthAddress := flag.String("health-listen", "0.0.0.0:50052", "internal health and CSDS address")
	shutdownTimeout := flag.Duration("shutdown-timeout", 20*time.Second, "maximum application drain time")
	flag.Parse()
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, *address, *healthAddress, *shutdownTimeout); err != nil {
		slog.Error("server failed", "error", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, address, healthAddress string, shutdownTimeout time.Duration) error {
	if shutdownTimeout <= 0 {
		return errors.New("shutdown-timeout must be positive")
	}
	if os.Getenv("GRPC_XDS_BOOTSTRAP") == "" {
		return errors.New("GRPC_XDS_BOOTSTRAP must point to the Cloud Service Mesh bootstrap file")
	}
	serverID, err := os.Hostname()
	if err != nil {
		return fmt.Errorf("determine server identity: %w", err)
	}
	healthService := health.NewServer()
	healthService.SetServingStatus("", healthv1.HealthCheckResponse_NOT_SERVING)
	healthService.SetServingStatus("liveness", healthv1.HealthCheckResponse_SERVING)
	server, err := xds.NewGRPCServer(xds.ServingModeCallback(func(addr net.Addr, change xds.ServingModeChangeArgs) {
		slog.Info("xDS serving mode changed", "address", addr.String(), "mode", change.Mode.String(), "error", change.Err)
		state := healthv1.HealthCheckResponse_NOT_SERVING
		if change.Mode == connectivity.ServingModeServing {
			state = healthv1.HealthCheckResponse_SERVING
		}
		healthService.SetServingStatus("", state)
	}))
	if err != nil {
		return fmt.Errorf("create xDS server: %w", err)
	}
	defer server.Stop()
	greeterv1.RegisterGreeterServer(server, &greeter.Service{ServerID: serverID})

	appListener, err := net.Listen("tcp4", address)
	if err != nil {
		return fmt.Errorf("listen for application traffic: %w", err)
	}
	defer appListener.Close()
	healthListener, err := net.Listen("tcp4", healthAddress)
	if err != nil {
		return fmt.Errorf("listen for health traffic: %w", err)
	}
	defer healthListener.Close()

	healthServer := grpc.NewServer()
	defer healthServer.Stop()
	healthv1.RegisterHealthServer(healthServer, healthService)
	closeAdmin, err := admin.Register(healthServer)
	if err != nil {
		return fmt.Errorf("register internal admin services: %w", err)
	}
	defer closeAdmin()

	slog.Warn("private-network plaintext demo: application mTLS is not enabled", "server_id", serverID)
	slog.Info("starting xDS server", "application", address, "health", healthAddress, "server_id", serverID)
	serveErrors := make(chan error, 2)
	go func() { serveErrors <- server.Serve(appListener) }()
	go func() { serveErrors <- healthServer.Serve(healthListener) }()

	var serveErr error
	select {
	case <-ctx.Done():
		slog.Info("shutdown requested")
	case serveErr = <-serveErrors:
		slog.Error("listener stopped", "error", serveErr)
	}
	healthService.Shutdown()
	drained := make(chan struct{})
	go func() {
		server.GracefulStop()
		close(drained)
	}()
	timer := time.NewTimer(shutdownTimeout)
	defer timer.Stop()
	select {
	case <-drained:
		slog.Info("application drained")
	case <-timer.C:
		slog.Warn("drain deadline exceeded; stopping active RPCs")
		server.Stop()
		<-drained
	}
	if serveErr != nil && !errors.Is(serveErr, grpc.ErrServerStopped) {
		return fmt.Errorf("serve: %w", serveErr)
	}
	return nil
}
