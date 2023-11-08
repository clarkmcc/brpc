package main

import (
	"context"
	"fmt"
	"github.com/clarkmcc/brpc"
	"github.com/clarkmcc/brpc/internal/example"
	"google.golang.org/grpc"
)

func main() {
	if err := run(); err != nil {
		panic(err)
	}
}

func run() error {
	server := brpc.NewServer(brpc.ServerConfig[example.GreeterServer, example.IdentifierClient]{
		ClientServiceBuilder: example.NewIdentifierClient,
		ServerServiceBuilder: func(server *brpc.Server[example.GreeterServer, example.IdentifierClient], registrar grpc.ServiceRegistrar) {
			example.RegisterGreeterServer(registrar, &GreeterService{Server: server})
		},
	})
	return server.Serve(context.Background(), ":10000")
}

type GreeterService struct {
	example.UnimplementedGreeterServer
	*brpc.Server[example.GreeterServer, example.IdentifierClient]
}

func (s *GreeterService) Greet(ctx context.Context, _ *example.GreetRequest) (*example.GreetResponse, error) {
	client, err := s.ClientFromContext(ctx)
	if err != nil {
		return nil, err
	}
	res, err := client.Identity(ctx, &example.IdentityRequest{})
	if err != nil {
		return nil, err
	}
	return &example.GreetResponse{
		Greeting: fmt.Sprintf("Hello %v", res.GetName()),
	}, nil
}
