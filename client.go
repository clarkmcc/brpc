package brpc

import (
	"context"
	"crypto/tls"
	"fmt"
	"github.com/google/uuid"
	"github.com/hashicorp/yamux"
	"github.com/quic-go/quic-go"
	"go.uber.org/multierr"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"net"
)

var DefaultDialer net.Dialer

// ServiceRegisterFunc is a function responsible for registering a gRPC service
// that is served by a brpc client, for a brpc server. Clients must provide one
// of these when dialing a brpc server.
type ServiceRegisterFunc[Service any] func(registrar grpc.ServiceRegistrar)

// ClientConn is a bidirectional gRPC connection that is generic over S, the gRPC
// server that we're connecting to. Callers use this connection to
//  1. Serve a gRPC server that is accessible to a brpc server.
//  2. Construct a gRPC client that can call the gRPC server.
type ClientConn struct {
	Dialer func(ctx context.Context, target string) (quic.Connection, error)
	*grpc.ClientConn

	conn    quic.Connection // The underlying net.Conn obtained from the Dialer
	session *yamux.Session  // A session that multiplexes all communication
	//grpcConn   quic.Stream     // A net.Conn over session reserved for client->server RPCs
	//grpcStream quic.Stream
	server *grpc.Server // The gRPC server that is served over the grpcConn for server->client RPCs
	uuid   uuid.UUID    // The client ID assigned by the server. Must be present on all client->server RPCs.
}

func Dial(target string, config *tls.Config) (*ClientConn, error) {
	return DialContext(context.Background(), target, config)
}

func DialContext(ctx context.Context, target string, config *tls.Config) (*ClientConn, error) {
	c := &ClientConn{
		Dialer: func(ctx context.Context, target string) (quic.Connection, error) {
			return quic.DialAddr(ctx, target, config, nil)
		},
	}
	return c, c.connect(ctx, target)
}

func (c *ClientConn) connect(ctx context.Context, target string) (err error) {
	c.conn, err = c.Dialer(ctx, target)
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			multierr.AppendFunc(&err, func() error {
				return c.conn.CloseWithError(quic.ApplicationErrorCode(quic.InternalError), err.Error())
			})
		}
	}()

	c.uuid, err = getClientID(ctx, c.conn)
	if err != nil {
		return fmt.Errorf("getting client id from server: %w", err)
	}

	// Open a stream for the client->server gRPC connection
	conn, err := c.conn.OpenStreamSync(ctx)
	if err != nil {
		return fmt.Errorf("opening multiplexed client->server gprc connection: %w", err)
	}
	c.ClientConn, err = dial(conn,
		c.WithUnaryConnectionIdentifier(),
		c.WithStreamConnectionIdentifier(),
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return fmt.Errorf("dialing client->server grpc connection: %w", err)
	}

	//c.server = grpc.NewServer()
	//register(c.server)

	// Start serving the client's gRPC server
	//go c.serve()
	return nil
}

func (c *ClientConn) serve() error {
	return c.server.Serve(&quicListener{conn: c.conn})
}

func (c *ClientConn) Close() error {
	// Close the gRPC server so that .Serve doesn't freak out
	// Then we close session, which closes all connections made
	// over the session, as well as the underlying connection.
	if c.server != nil {
		// This also closes c.conn
		c.server.GracefulStop()
	}
	return nil //c.session.Close()
}

// WithUnaryConnectionIdentifier is a grpc.DialOption that adds the client's UUID to
// all unary requests. This is required if the server intends to call back to
// the client's gRPC server.
func (c *ClientConn) WithUnaryConnectionIdentifier() grpc.DialOption {
	return grpc.WithUnaryInterceptor(func(ctx context.Context, method string, req, reply any, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
		ctx = metadata.AppendToOutgoingContext(ctx, metadataClientIDKey, c.uuid.String())
		return invoker(ctx, method, req, reply, cc, opts...)
	})
}

// WithStreamConnectionIdentifier is a grpc.DialOption that adds the client's UUID to
// all stream requests. This is required if the server intends to call back to
// the client's gRPC server.
func (c *ClientConn) WithStreamConnectionIdentifier() grpc.DialOption {
	return grpc.WithStreamInterceptor(func(ctx context.Context, desc *grpc.StreamDesc, cc *grpc.ClientConn, method string, streamer grpc.Streamer, opts ...grpc.CallOption) (grpc.ClientStream, error) {
		ctx = metadata.AppendToOutgoingContext(ctx, metadataClientIDKey, c.uuid.String())
		return streamer(ctx, desc, cc, method, opts...)
	})
}

//// Client constructs a gRPC client for ClientService. It accepts the brpc.ClientConn
//// and a constructor function generated by protoc.
//func Client[ClientService any](conn *ClientConn, fn func(cc grpc.ClientConnInterface) ClientService) (ClientService, error) {
//	var def ClientService
//	c, err := dial(conn.grpcConn,
//		conn.WithUnaryConnectionIdentifier(),
//		conn.WithStreamConnectionIdentifier(),
//		grpc.WithTransportCredentials(insecure.NewCredentials()))
//	if err != nil {
//		return def, err
//	}
//	return fn(c), nil
//}

func ServeClientService[C any](shutdown <-chan struct{}, c *ClientConn, register ServiceRegisterFunc[C]) error {
	c.server = grpc.NewServer()
	register(c.server)
	go func() {
		<-shutdown
		c.server.GracefulStop()
	}()
	return c.serve()
}
