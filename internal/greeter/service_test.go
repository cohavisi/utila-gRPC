package greeter

import (
	"context"
	"testing"

	greeterv1 "github.com/cohavisi/utila-gRPC/api/greeter/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestSayHello(t *testing.T) {
	service := &Service{ServerID: "pod-1"}
	reply, err := service.SayHello(context.Background(), &greeterv1.HelloRequest{Name: " reviewer "})
	if err != nil {
		t.Fatal(err)
	}
	if reply.GetMessage() != "Hello, reviewer!" || reply.GetServerId() != "pod-1" {
		t.Fatalf("unexpected response: %v", reply)
	}
}

func TestSayHelloRejectsEmptyName(t *testing.T) {
	service := &Service{ServerID: "pod-1"}
	for _, req := range []*greeterv1.HelloRequest{nil, {}, {Name: " \t"}} {
		if _, err := service.SayHello(context.Background(), req); status.Code(err) != codes.InvalidArgument {
			t.Errorf("SayHello(%v): got %v, want InvalidArgument", req, err)
		}
	}
}
