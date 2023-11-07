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
	server := brpc.NewServer(brpc.ServerConfig[example.ServiceServer, example.ClientServiceClient]{
		ClientServiceBuilder: example.NewClientServiceClient,
		ServerServiceBuilder: func(server *brpc.Server[example.ServiceServer, example.ClientServiceClient], registrar grpc.ServiceRegistrar) {
			example.RegisterServiceServer(registrar, &service{Server: server})
		},
	})
	return server.Serve(":10000")
}

type service struct {
	example.UnimplementedServiceServer
	*brpc.Server[example.ServiceServer, example.ClientServiceClient]
}

func (s *service) ExampleMethod(ctx context.Context, req *example.Empty) (*example.Empty, error) {
	fmt.Println("Client called server method")
	client := s.ClientFromContext(ctx)
	_, err := client.ExampleClientMethod(ctx, &example.Empty{})
	if err != nil {
		fmt.Println("error calling client from server")
		return nil, err
	}
	fmt.Println("called client from server")

	return &example.Empty{}, nil
}
