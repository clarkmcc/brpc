package main

import (
	"context"
	"fmt"
	"github.com/clarkmcc/brpc/internal/example"
	"github.com/hashicorp/yamux"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"net"
	"os"
	"os/signal"
	"sync/atomic"
)

func main() {
	err := serve()
	if err != nil {
		panic(err)
	}
}

func serve() error {
	listener, err := net.Listen("tcp", ":10000")
	if err != nil {
		return err
	}

	cl := newChanListener()
	fmt.Println("Starting gRPC")
	srv := grpc.NewServer(grpc.UnaryInterceptor(func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (resp any, err error) {
		fmt.Printf("%T\n", info.Server)
		return handler(ctx, req)
	}))
	example.RegisterServiceServer(srv, &service{})
	go func() {
		err = srv.Serve(cl)
		if err != nil {
			panic(fmt.Errorf("serving gRPC: %w", err))
		}
	}()

	fmt.Println("waiting for primary connection")
	conn, err := listener.Accept()
	if err != nil {
		return err
	}
	session, err := yamux.Server(conn, nil)
	if err != nil {
		return err
	}

	fmt.Println("accepting secondary grpc connection")
	conn, err = session.Accept()
	if err != nil {
		return err
	}
	cl.push(conn)

	fmt.Println("Attempting to connect to client server")
	conn, err = session.Open()
	if err != nil {
		return err
	}
	fmt.Println("Connected to client server")
	grpcConn, err := grpc.Dial("", grpc.WithContextDialer(func(ctx context.Context, s string) (net.Conn, error) {
		return conn, nil
	}), grpc.WithTransportCredentials(insecure.NewCredentials()))
	client := example.NewClientServiceClient(grpcConn)
	_, err = client.ExampleClientMethod(context.Background(), &example.Empty{})
	if err != nil {
		return err
	}
	err = grpcConn.Close()
	if err != nil {
		return err
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, os.Kill)
	defer cancel()
	<-ctx.Done()
	return nil
}

var _ net.Listener = &channelListener{}

type channelListener struct {
	conn   chan net.Conn
	closed atomic.Bool
}

func (c *channelListener) Accept() (net.Conn, error) {
	if c.closed.Load() {
		return nil, fmt.Errorf("listener closed")
	}
	return <-c.conn, nil
}

func (c *channelListener) Close() error {
	if !c.closed.Load() {
		close(c.conn)
		c.closed.Store(true)
	}
	return nil
}

func (c *channelListener) Addr() net.Addr {
	return &net.TCPAddr{}
}

func (c *channelListener) watch(l net.Listener) error {
	for {
		conn, err := l.Accept()
		if err != nil {
			return err
		}
		c.push(conn)
	}
}

func (c *channelListener) push(conn net.Conn) {
	if !c.closed.Load() {
		c.conn <- conn
	}
}

func newChanListener() *channelListener {
	return &channelListener{
		conn: make(chan net.Conn),
	}
}

type service struct {
	example.UnimplementedServiceServer
}

func (s *service) ExampleMethod(ctx context.Context, req *example.Empty) (*example.Empty, error) {
	fmt.Println("Client called server method")
	return &example.Empty{}, nil
}
