package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	greeterv1 "github.com/cohavisi/utila-gRPC/api/greeter/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	_ "google.golang.org/grpc/xds"
)

type config struct {
	target   string
	name     string
	count    int
	interval time.Duration
	timeout  time.Duration
}

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))
	cfg := config{}
	flag.StringVar(&cfg.target, "target", "xds:///greeter.utila.internal:50051", "Cloud Service Mesh xDS target")
	flag.StringVar(&cfg.name, "name", "reviewer", "name to greet")
	flag.IntVar(&cfg.count, "count", 100, "number of calls; 0 runs until interrupted")
	flag.DurationVar(&cfg.interval, "interval", 100*time.Millisecond, "delay between calls")
	flag.DurationVar(&cfg.timeout, "timeout", 10*time.Second, "per-call deadline including discovery")
	flag.Parse()
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, cfg); err != nil {
		if errors.Is(err, context.Canceled) {
			slog.Info("client stopped")
			return
		}
		slog.Error("client failed", "error", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, cfg config) error {
	if !strings.HasPrefix(cfg.target, "xds:///") || len(cfg.target) == len("xds:///") {
		return errors.New("target must be a non-empty xds:/// service URI")
	}
	if cfg.count < 0 || cfg.interval < 0 || cfg.timeout <= 0 || strings.TrimSpace(cfg.name) == "" {
		return errors.New("count and interval must be non-negative; timeout and name must be non-empty/positive")
	}
	if os.Getenv("GRPC_XDS_BOOTSTRAP") == "" {
		return errors.New("GRPC_XDS_BOOTSTRAP must point to the Cloud Service Mesh bootstrap file")
	}
	conn, err := grpc.NewClient(cfg.target, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return fmt.Errorf("create xDS channel: %w", err)
	}
	defer conn.Close()
	client := greeterv1.NewGreeterClient(conn)
	failures := 0
	served := make(map[string]int)
	slog.Warn("private-network plaintext demo: application mTLS is not enabled")
	defer func() { slog.Info("call summary", "served_by", served, "failures", failures) }()
	for n := 0; cfg.count == 0 || n < cfg.count; n++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		callCtx, cancel := context.WithTimeout(ctx, cfg.timeout)
		reply, callErr := client.SayHello(callCtx, &greeterv1.HelloRequest{Name: cfg.name}, grpc.WaitForReady(true))
		cancel()
		if err := ctx.Err(); err != nil {
			return err
		}
		if callErr != nil {
			failures++
			slog.Error("SayHello failed", "call", n+1, "error", callErr)
		} else {
			served[reply.GetServerId()]++
			slog.Info("SayHello succeeded", "call", n+1, "message", reply.GetMessage(), "server_id", reply.GetServerId())
		}
		if cfg.count != 0 && n+1 == cfg.count {
			break
		}
		timer := time.NewTimer(cfg.interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
	if failures > 0 {
		return fmt.Errorf("%d RPCs failed", failures)
	}
	return nil
}
