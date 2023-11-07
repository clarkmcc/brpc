package main

import (
	context "context"
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
	client, err := brpc.Dial[example.ClientServiceServer](":10000", func(r grpc.ServiceRegistrar) {
		example.RegisterClientServiceServer(r, &service{})
	})
	if err != nil {
		return err
	}
	defer client.Close()

	go func() {
		err := client.Serve()
		if err != nil {
			panic(err)
		}
	}()

	clientClient, err := brpc.ConstructClient(client, example.NewServiceClient)
	if err != nil {
		return err
	}
	_, err = clientClient.ExampleMethod(context.Background(), &example.Empty{})
	if err != nil {
		return err
	}
	fmt.Println("Called server method")

	return nil
}

type service struct {
	example.UnimplementedClientServiceServer
}

func (s *service) ExampleClientMethod(ctx context.Context, req *example.Empty) (*example.Empty, error) {
	fmt.Println("Server called example method")
	return &example.Empty{}, nil
}
