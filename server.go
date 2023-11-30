package brpc

import (
	"context"
	"errors"
	"fmt"
	"github.com/clarkmcc/brpc/internal/grpcsync"
	"github.com/google/uuid"
	"github.com/quic-go/quic-go"
	"go.uber.org/multierr"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"io"
	"log/slog"
	"reflect"
)

const metadataClientIDKey = "brpc-metadata-client-id"

// Server is a bidirectional gRPC server that allows you to plug in your own gRPC server,
// as well as a gRPC client which your gRPC server can use to call client RPCs.
//
// Server works by handling the initial connection negotiation, and then multiplexes all
// future communication, including client->server RPCs and server->client RPCs over a
// single TCP connection.
type Server[C any] struct {
	Logger *slog.Logger
	*grpc.Server

	clientServiceBuilder  func(conn grpc.ClientConnInterface) C
	registerServerService func(server *Server[C], registrar grpc.ServiceRegistrar)
	clients               *clientMap[C]
	quicListener          *quic.Listener
	listener              *multiListener
	shutdown              *grpcsync.Event
}

func (s *Server[C]) Serve(ctx context.Context, listener *quic.Listener) error {
	if s.Server == nil {
		return fmt.Errorf("server not provided")
	}

	go func() {
		for {
			select {
			case <-s.shutdown.Done():
				_ = listener.Close()
				return
			default:
			}

			conn, err := listener.Accept(ctx)
			if err != nil {
				if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
					return
				}
				s.Logger.Error("accepting connection", "error", err)
				continue
			}

			go s.handleConnection(ctx, conn)
		}
	}()

	return s.Server.Serve(s.listener)
}

func (s *Server[C]) handleConnection(ctx context.Context, conn quic.Connection) {
	go func() {
		select {
		case <-s.shutdown.Done():
			_ = conn.CloseWithError(quic.ApplicationErrorCode(100), "server shutdown")
		case <-conn.Context().Done():
			return
		}
	}()

	err := s.handler(ctx, conn)
	if err != nil {
		if errors.Is(err, io.EOF) {
			return
		}
		s.Logger.Error("handling connection", "error", err, "type", reflect.TypeOf(err).String())
	}
}

func (s *Server[C]) handler(ctx context.Context, conn quic.Connection) (err error) {
	// When this function returns, everything should be cleaned up
	defer multierr.AppendFunc(&err, func() error {
		return conn.CloseWithError(quic.ApplicationErrorCode(quic.NoError), "")
	})

	id, err := sendClientID(ctx, conn)
	if err != nil {
		return fmt.Errorf("sending client id: %w", err)
	}

	// Open a connection used for server->client RPCs and create a gRPC
	// client using that connection.
	grpcConn, err := conn.OpenStreamSync(ctx)
	if err != nil {
		return fmt.Errorf("opening server->client grpc connection: %w", err)
	}
	defer multierr.AppendFunc(&err, grpcConn.Close)
	grpcClient, err := dial(grpcConn, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return fmt.Errorf("dialing client's grpc server: %w", err)
	}
	defer multierr.AppendFunc(&err, grpcClient.Close)

	// Register this gRPC client into our client map so that when the user's
	// gRPC service implementation receives an RPC, it can look up the clients
	// gRPC client and connect to it.
	err = s.clients.add(id, s.clientServiceBuilder(grpcClient))
	if err != nil {
		return fmt.Errorf("registering client with id %s: %w", id, err)
	}
	defer s.clients.remove(id)
	defer s.Logger.Info("client disconnected", "id", id)
	s.listener.AddListener(&quicListener{conn: conn})
	<-conn.Context().Done()
	return nil
}

func (s *Server[C]) GracefulStop() {
	s.shutdown.Fire()
	s.Server.GracefulStop()
}

// ServerConfig allows you to configure the server. It is generic over S (the gRPC service
// interface) and C (the gRPC client interface). When building a brpc server, you'll need
// to provide these builders for the server and client services.
type ServerConfig[C any] struct {
	// ClientServiceBuilder is a function that provides a grpc.ClientConnInterface so that
	// you can then construct your gRPC client. This client allows you to call methods on
	// your client, as if your client were a server. If I had generated a client called
	// "ClientService" then I can pass the generated gRPC constructor directly.
	//
	// 		ClientServiceBuilder: example.NewClientServiceClient
	//
	ClientServiceBuilder func(cc grpc.ClientConnInterface) C

	// The gRPC server that we should forward RPC requests to
	Server *grpc.Server
}

// NewServer constructs
func NewServer[C any](config ServerConfig[C]) *Server[C] {
	return &Server[C]{
		Logger:               slog.Default(),
		Server:               config.Server,
		clients:              newClientMap[C](),
		clientServiceBuilder: config.ClientServiceBuilder,
		listener:             newMultiListener(),
		shutdown:             grpcsync.NewEvent(),
	}
}

// ClientFromContext returns a client
func (s *Server[C]) ClientFromContext(ctx context.Context) (client C, err error) {
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
