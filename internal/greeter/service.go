package greeter

import (
	"context"
	"strings"

	greeterv1 "github.com/cohavisi/utila-gRPC/api/greeter/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type Service struct {
	greeterv1.UnimplementedGreeterServer
	ServerID string
}

func (s *Service) SayHello(_ context.Context, req *greeterv1.HelloRequest) (*greeterv1.HelloReply, error) {
	name := strings.TrimSpace(req.GetName())
	if name == "" {
		return nil, status.Error(codes.InvalidArgument, "name must not be empty")
	}
	return &greeterv1.HelloReply{
		Message:  "Hello, " + name + "!",
		ServerId: s.ServerID,
	}, nil
}
