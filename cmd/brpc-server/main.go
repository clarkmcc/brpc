package main

import (
	"context"
	"fmt"
	"github.com/clarkmcc/brpc"
	"github.com/clarkmcc/brpc/internal/example"
	"github.com/quic-go/quic-go"
	"google.golang.org/grpc"
)

func main() {
	if err := run(); err != nil {
		panic(err)
	}
}

func run() error {
	// Create a quic listener
	l, err := quic.ListenAddr(":10000", example.TLSConfig(), nil)
	if err != nil {
		return err
	}

	// Create the gRPC server, the bRPC server, and register our gRPC service
	srv := grpc.NewServer()
	server := brpc.NewServer(brpc.ServerConfig[example.NamerClient]{
		ClientServiceBuilder: example.NewNamerClient,
		Server:               srv,
	})
	example.RegisterGreeterServer(srv, &GreeterService{Server: server})

	return server.Serve(context.Background(), l)
}

type GreeterService struct {
	example.UnimplementedGreeterServer
	*brpc.Server[example.NamerClient]
}

func (s *GreeterService) Greet(ctx context.Context, _ *example.GreetRequest) (*example.GreetResponse, error) {
	client, err := s.ClientFromContext(ctx)
	if err != nil {
		return nil, err
	}
	res, err := client.Name(ctx, &example.NameRequest{})
	if err != nil {
		return nil, err
	}
	return &example.GreetResponse{
		Greeting: fmt.Sprintf("Hello %v", res.GetName()),
	}, nil
}
