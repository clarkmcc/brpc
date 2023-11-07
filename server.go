package brpc

import (
	"context"
	"errors"
	"fmt"
	"github.com/google/uuid"
	"github.com/hashicorp/yamux"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"io"
	"log/slog"
	"net"
)

const metadataKey = "brpc-metadata-client-id"

type Server[S, C any] struct {
	Logger *slog.Logger

	clientServiceBuilder  func(conn grpc.ClientConnInterface) C
	registerServerService func(server *Server[S, C], registrar grpc.ServiceRegistrar)
	clients               *clientMap[C]
}

func (s *Server[S, C]) ListenAndServe(address string, server *grpc.Server) error {
	listener, err := net.Listen("tcp", address)
	if err != nil {
		return err
	}
	for {
		conn, err := listener.Accept()
		if err != nil {
			return err
		}
		go s.handleConnection(conn, server)
	}
}

func (s *Server[S, C]) Serve(address string) error {
	listener, err := net.Listen("tcp", address)
	if err != nil {
		return err
	}
	srv := grpc.NewServer()
	s.registerServerService(s, srv)

	for {
		conn, err := listener.Accept()
		if err != nil {
			return err
		}
		go s.handleConnection(conn, srv)
	}
}

func (s *Server[S, C]) handleConnection(conn net.Conn, server *grpc.Server) {
	err := s.negotiate(conn, server)
	if err != nil {
		if errors.Is(err, io.EOF) {
			fmt.Println("connection closed")
			return
		}
		panic(err)
	}
}

func (s *Server[S, C]) negotiate(conn net.Conn, server *grpc.Server) error {
	session, err := yamux.Server(conn, nil)
	if err != nil {
		return err
	}

	id, err := negotiateConnIdServer(session)
	if err != nil {
		return fmt.Errorf("negotiating client id: %w", err)
	}
	defer s.clients.remove(id)

	grpcConn, err := session.Open()
	if err != nil {
		return err
	}
	grpcSession, err := yamux.Client(grpcConn, nil)
	if err != nil {
		return err
	}
	grpcChildConn, err := grpcSession.Open()
	if err != nil {
		return err
	}

	grpcClient, err := grpc.Dial("", ContextDialer(grpcChildConn), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return err
	}
	s.clients.add(id, s.clientServiceBuilder(grpcClient))

	err = server.Serve(session)
	if err != nil {
		return err
	}
	return nil
}

func ContextDialer(conn net.Conn) grpc.DialOption {
	return grpc.WithContextDialer(func(ctx context.Context, s string) (net.Conn, error) {
		return conn, nil
	})
}

type ServerConfig[S, C any] struct {
	ServerServiceBuilder func(server *Server[S, C], registrar grpc.ServiceRegistrar)
	ClientServiceBuilder func(cc grpc.ClientConnInterface) C
}

func NewServer[S, C any](config ServerConfig[S, C]) *Server[S, C] {
	return &Server[S, C]{
		Logger:                slog.Default(),
		clients:               newClientMap[C](),
		clientServiceBuilder:  config.ClientServiceBuilder,
		registerServerService: config.ServerServiceBuilder,
	}
}

func (s *Server[S, C]) ClientFromContext(ctx context.Context) C {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		panic("metadata not found")
	}
	id := uuid.MustParse(md.Get(metadataKey)[0])
	client, ok := s.clients.get(id)
	if !ok {
		panic("client not found")
	}
	return client
}
