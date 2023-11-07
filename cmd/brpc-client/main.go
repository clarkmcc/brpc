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
	conn, err := brpc.Dial[example.GreeterServer](":10000", func(r grpc.ServiceRegistrar) {
		example.RegisterIdentifierServer(r, &service{})
	})
	if err != nil {
		return err
	}

	client, err := brpc.Client(conn, example.NewGreeterClient)
	if err != nil {
		return err
	}
	res, err := client.Greet(context.Background(), &example.GreetRequest{})
	if err != nil {
		return err
	}
	fmt.Printf("Got greeting: %v\n", res.GetGreeting())
	err = conn.Close()
	if err != nil {
		return fmt.Errorf("closing: %v", err)
	}
	return nil
}

type service struct {
	example.UnimplementedIdentifierServer
}

func (s *service) Identity(ctx context.Context, req *example.IdentityRequest) (*example.IdentityResponse, error) {
	return &example.IdentityResponse{Name: "brpc"}, nil
}
