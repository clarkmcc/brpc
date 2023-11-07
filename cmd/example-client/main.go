package main

import (
	context "context"
	"fmt"
	"github.com/clarkmcc/brpc/internal/example"
	"github.com/hashicorp/yamux"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"net"
)

func main() {
	if err := run(); err != nil {
		panic(err)
	}
}

func run() error {
	conn, err := net.Dial("tcp", ":10000")
	if err != nil {
		return err
	}
	session, err := yamux.Client(conn, nil)
	if err != nil {
		return err
	}
	stream, err := session.Open()
	if err != nil {
		return err
	}

	grpcConn, err := grpc.Dial("", grpc.WithContextDialer(func(ctx context.Context, s string) (net.Conn, error) {
		return stream, nil
	}), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return err
	}
	client := example.NewServiceClient(grpcConn)
	_, err = client.ExampleMethod(context.Background(), &example.Empty{})
	if err != nil {
		return err
	}

	fmt.Println("Waiting for server to open client with client")
	//conn, err = session.Accept()
	//if err != nil {
	//	return err
	//}
	//fmt.Println("Server opened client with client")

	srv := grpc.NewServer()
	example.RegisterClientServiceServer(srv, &service{})
	err = srv.Serve(session)
	if err != nil {
		return err
	}

	return grpcConn.Close()
}

type service struct {
	example.UnimplementedClientServiceServer
}

func (s *service) ExampleClientMethod(ctx context.Context, req *example.Empty) (*example.Empty, error) {
	fmt.Println("Server called example method")
	return &example.Empty{}, nil
}
