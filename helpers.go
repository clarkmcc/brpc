package brpc

import (
	"fmt"
	"github.com/google/uuid"
	"net"
)

func negotiateConnIdClient(dialer func() (net.Conn, error), uuid uuid.UUID) error {
	conn, err := dialer()
	if err != nil {
		return fmt.Errorf("dialing: %w", err)
	}
	_, err = conn.Write(uuid[:])
	if err != nil {
		return err
	}
	return conn.Close()
}

func negotiateConnIdServer(listener net.Listener) (id uuid.UUID, err error) {
	conn, err := listener.Accept()
	if err != nil {
		return id, err
	}
	_, err = conn.Read(id[:])
	if err != nil {
		return id, err
	}
	return id, nil
}
