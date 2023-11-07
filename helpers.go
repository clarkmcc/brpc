package brpc

import (
	"context"
	"fmt"
	"github.com/google/uuid"
	"go.uber.org/multierr"
	"google.golang.org/grpc"
	"net"
)

func getClientID(listener net.Listener) (id uuid.UUID, err error) {
	conn, err := listener.Accept()
	if err != nil {
		return id, err
	}
	// Server closes the client
	n, err := conn.Read(id[:])
	if err != nil {
		return id, err
	}
	if n != len(id) {
		return id, fmt.Errorf("read %v bytes, expected %v", n, len(id))
	}
	return id, nil
}

func sendClientID(dialer func() (net.Conn, error)) (id uuid.UUID, err error) {
	id = uuid.New()
	conn, err := dialer()
	if err != nil {
		return id, err
	}
	defer multierr.AppendFunc(&err, conn.Close)
	n, err := conn.Write(id[:])
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
func dial(conn net.Conn, options ...grpc.DialOption) (*grpc.ClientConn, error) {
	return grpc.Dial("", append(options, withContextDialer(conn))...)
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
