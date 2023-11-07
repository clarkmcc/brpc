package brpc

import (
	"context"
	"errors"
	"fmt"
	"github.com/google/uuid"
	"github.com/hashicorp/yamux"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"io"
	"log/slog"
	"net"
)

const metadataClientIDKey = "brpc-metadata-client-id"

// Server is a bidirectional gRPC server that allows you to plug in your own gRPC server,
// as well as a gRPC client which your gRPC server can use to call client RPCs.
//
// Server works by handling the initial connection negotiation, and then multiplexes all
// future communication, including client->server RPCs and server->client RPCs over a
// single TCP connection.
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
	if s.registerServerService == nil {
		return errors.New("server service builder not provided")
	}
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
			return
		}
		s.Logger.Error("handling connection", "error", err)
	}
}

func (s *Server[S, C]) negotiate(conn net.Conn, server *grpc.Server) error {
	session, err := yamux.Server(conn, nil)
	if err != nil {
		return err
	}
	defer session.Close()

	id, err := sendClientID(session.Open)
	if err != nil {
		return fmt.Errorf("negotiating client id: %w", err)
	}

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

	grpcClient, err := dial(grpcChildConn, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return err
	}
	err = s.clients.add(id, s.clientServiceBuilder(grpcClient))
	if err != nil {
		return err
	}
	defer s.clients.remove(id)

	err = server.Serve(session)
	if err != nil {
		return err
	}
	return nil
}

// ServerConfig allows you to configure the server. It is generic over S (the gRPC service
// interface) and C (the gRPC client interface). When building a brpc server, you'll need
// to provide these builders for the server and client services.
type ServerConfig[S, C any] struct {
	// ServerServiceBuilder is a function that registers the server's gRPC service with
	// the brpc server. It needs to be provided only if brpc is managing the gRPC server.
	// If you're providing your own gRPC server by calling ListenAndServe, then this option
	// can be omitted.
	//
	// The Server is also provided because it allows access to the gRPC client used to
	// allow the server to call client RPCs. Users may choose to embed a reference to ths
	// server in their own server implementation, so that they can call client RPCs in
	// response to the client calling server RPCs.
	ServerServiceBuilder func(server *Server[S, C], registrar grpc.ServiceRegistrar)

	// ClientServiceBuilder is a function that provides a grpc.ClientConnInterface so that
	// you can then construct your gRPC client. This client allows you to call methods on
	// your client, as if your client were a server. If I had generated a client called
	// "ClientService" then I can pass the generated gRPC constructor directly.
	//
	// 		ClientServiceBuilder: example.NewClientServiceClient
	//
	ClientServiceBuilder func(cc grpc.ClientConnInterface) C
}

// NewServer constructs
func NewServer[S, C any](config ServerConfig[S, C]) *Server[S, C] {
	return &Server[S, C]{
		Logger:                slog.Default(),
		clients:               newClientMap[C](),
		clientServiceBuilder:  config.ClientServiceBuilder,
		registerServerService: config.ServerServiceBuilder,
	}
}

// ClientFromContext returns a client
func (s *Server[S, C]) ClientFromContext(ctx context.Context) (client C, err error) {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return client, status.Error(codes.InvalidArgument, "metadata not provided")
	}
	ids := md.Get(metadataClientIDKey)
	if len(ids) == 0 {
		return client, status.Error(codes.InvalidArgument, "client id not provided")
	}
	id, err := uuid.Parse(ids[0])
	if err != nil {
		return client, status.Error(codes.InvalidArgument, "invalid client id")
	}
	s.Logger.Info("getting client", "id", id)
	client, ok = s.clients.get(id)
	if !ok {
		return client, status.Error(codes.NotFound, "client not found")
	}
	return client, nil
}
