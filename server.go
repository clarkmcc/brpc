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
	"reflect"
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
	listener              *multiListener
}

func (s *Server[S, C]) ListenAndServe(address string) error {
	listener, err := net.Listen("tcp", address)
	if err != nil {
		return err
	}
	for {
		conn, err := listener.Accept()
		if err != nil {
			return err
		}
		go s.handleConnection(conn)
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

	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				s.Logger.Error("accepting connection", "error", err)
				continue
			}
			go s.handleConnection(conn)
		}
	}()

	return srv.Serve(s.listener)
}

func (s *Server[S, C]) handleConnection(conn net.Conn) {
	err := s.negotiate(conn)
	if err != nil {
		if errors.Is(err, io.EOF) {
			return
		}
		s.Logger.Error("handling connection", "error", err, "type", reflect.TypeOf(err).String())
	}
}

func (s *Server[S, C]) negotiate(conn net.Conn) (err error) {
	defer conn.Close()
	session, err := yamux.Server(conn, nil)
	if err != nil {
		return fmt.Errorf("creating yamux server: %w", err)
	}

	id, err := sendClientID(session.Open)
	if err != nil {
		return fmt.Errorf("sending client id: %w", err)
	}

	grpcConn, err := session.Open()
	if err != nil {
		return fmt.Errorf("opening server->client grpc connection: %w", err)
	}
	defer grpcConn.Close()
	grpcSession, err := yamux.Client(grpcConn, nil)
	if err != nil {
		return fmt.Errorf("creating double-multiplexed session for server->client grpc: %w", err)
	}
	defer grpcSession.Close()
	grpcChildConn, err := grpcSession.Open()
	if err != nil {
		return fmt.Errorf("opening double-multiplexed server->client gprc connection: %w", err)
	}
	defer grpcChildConn.Close()

	// Create our gRPC client that allows for server->client RPCs
	grpcClient, err := dial(grpcChildConn, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return fmt.Errorf("dialing client's grpc server: %w", err)
	}
	defer grpcClient.Close()

	// Register this gRPC client into our client map so that when the user's
	// gRPC service implementation receives an RPC, it can look up the clients
	// gRPC client and connect to it.
	err = s.clients.add(id, s.clientServiceBuilder(grpcClient))
	if err != nil {
		return fmt.Errorf("registering client with id %s: %w", id, err)
	}
	defer s.clients.remove(id)
	defer s.Logger.Info("client disconnected", "id", id)
	s.listener.AddListener(session)
	<-session.CloseChan()
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
		listener:              newMultiListener(),
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
