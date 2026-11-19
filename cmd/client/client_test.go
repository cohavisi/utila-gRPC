package main

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestRunRejectsNonXDSAndInvalidCallConfiguration(t *testing.T) {
	tests := []struct {
		name string
		cfg  config
	}{
		{"dns target", config{target: "dns:///greeter", name: "test", count: 1, timeout: time.Second}},
		{"empty service", config{target: "xds:///", name: "test", count: 1, timeout: time.Second}},
		{"negative count", config{target: "xds:///greeter", name: "test", count: -1, timeout: time.Second}},
		{"no deadline", config{target: "xds:///greeter", name: "test", count: 1}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := run(context.Background(), test.cfg); err == nil {
				t.Fatal("invalid configuration succeeded")
			}
		})
	}
}

func TestRunRequiresBootstrap(t *testing.T) {
	t.Setenv("GRPC_XDS_BOOTSTRAP", "")
	err := run(context.Background(), config{target: "xds:///greeter", name: "test", count: 1, timeout: time.Second})
	if err == nil || !strings.Contains(err.Error(), "GRPC_XDS_BOOTSTRAP") {
		t.Fatalf("got %v, want explicit bootstrap error", err)
	}
}
