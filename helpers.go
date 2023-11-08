package brpc

import (
	"context"
	"errors"
	"fmt"
	"github.com/google/uuid"
	"github.com/quic-go/quic-go"
	"go.uber.org/multierr"
	"google.golang.org/grpc"
	"io"
	"net"
)

func getClientID(ctx context.Context, conn quic.Connection) (id uuid.UUID, err error) {
	stream, err := conn.AcceptUniStream(ctx)
	if err != nil {
		return id, fmt.Errorf("accepting: %w", err)
	}
	// Server closes the client
	n, err := stream.Read(id[:])
	if err != nil && !errors.Is(err, io.EOF) {
		return id, fmt.Errorf("reading: %w", err)
	}
	if n != len(id) {
		return id, fmt.Errorf("read %v bytes, expected %v", n, len(id))
	}
	return id, nil
}

func sendClientID(ctx context.Context, conn quic.Connection) (id uuid.UUID, err error) {
	id = uuid.New()
	stream, err := conn.OpenUniStreamSync(ctx)
	if err != nil {
		return id, err
	}
	defer multierr.AppendFunc(&err, stream.Close)
	n, err := stream.Write(id[:])
	if err != nil {
		return id, err
	}
	if n != len(id) {
		return id, fmt.Errorf("wrote %v bytes, expected %v", n, len(id))
	}
	return id, nil
}

// dial is a wrapper around grpc.Dial(...) that handles tunneling over an already existing
// net.Conn. It does not require a target address, as the connection is already established.
func dial(stream quic.Stream, options ...grpc.DialOption) (*grpc.ClientConn, error) {
	return grpc.Dial("", append(options, withContextDialer(&quicConn{Stream: stream}))...)
}

// withContextDialer is a grpc.DialOption that allows you to provide a net.Conn to use
// for a gRPC client connection. When provided, users do not need to specify a
func withContextDialer(conn net.Conn) grpc.DialOption {
	return grpc.WithContextDialer(func(ctx context.Context, s string) (net.Conn, error) {
		if conn == nil {
			return nil, fmt.Errorf("no connection provided")
		}
		return conn, nil
	})
}
