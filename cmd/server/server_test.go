package main

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestRunRequiresBootstrap(t *testing.T) {
	t.Setenv("GRPC_XDS_BOOTSTRAP", "")
	err := run(context.Background(), "127.0.0.1:0", "127.0.0.1:0", time.Second)
	if err == nil || !strings.Contains(err.Error(), "GRPC_XDS_BOOTSTRAP") {
		t.Fatalf("expected explicit missing-bootstrap error, got %v", err)
	}
}
