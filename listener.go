package brpc

import (
	"errors"
	"go.uber.org/multierr"
	"io"
	"log/slog"
	"net"
	"os"
	"sync"
	"syscall"
)

var _ net.Listener = &multiListener{}

// multiListener is an implementation of net.Listener that wraps many net.Listeners
// which can be added to the multiListener. Callers can add new net.Listeners and
// other callers can block on Accept() to receive a net.Conn from any of the added
// net.Listeners.
//
// In our case, brpc servers use this because we want to handle incoming connections
// ourselves first (to negotiate a yamux session, pass client ids, etc), and then
// pass the yamux.Session (which implements net.Listener) into the multiListener
// so that our gRPC server can accept all future connections from the yamux.Session.
type multiListener struct {
	listenersLock sync.Mutex
	listeners     []net.Listener
	connChan      chan net.Conn
	errChan       chan error // errors on this channel
	closeChan     chan struct{}
	wg            sync.WaitGroup
	logger        *slog.Logger
}

func newMultiListener() *multiListener {
	return &multiListener{
		connChan:  make(chan net.Conn),
		errChan:   make(chan error, 1), // buffered channel for at least one error
		closeChan: make(chan struct{}),
		logger:    slog.Default(),
	}
}

func (ml *multiListener) AddListener(l net.Listener) {
	ml.listenersLock.Lock()
	ml.listeners = append(ml.listeners, l)
	ml.listenersLock.Unlock()

	ml.wg.Add(1)
	go func() {
		defer ml.wg.Done()
		for {
			conn, err := l.Accept()
			if err != nil {
				if !isTransientError(err) {
					slog.Warn("error accepting connection", "error", err)
				}
				return
			}

			select {
			case ml.connChan <- conn:
			case <-ml.closeChan:
				return
			}
		}
	}()
}

func (ml *multiListener) Accept() (net.Conn, error) {
	select {
	case conn := <-ml.connChan:
		return conn, nil
	case err := <-ml.errChan:
		return nil, err
	case <-ml.closeChan:
		return nil, net.ErrClosed
	}
}

func (ml *multiListener) Close() error {
	close(ml.closeChan)
	ml.wg.Wait()
	var err error
	for _, l := range ml.listeners {
		err = multierr.Append(err, l.Close())
	}
	return err
}

func (ml *multiListener) Addr() net.Addr {
	return &net.TCPAddr{}
}

func isTransientError(err error) bool {
	// Directly check for net.ErrClosed or io.EOF
	if errors.Is(err, net.ErrClosed) || errors.Is(err, io.EOF) {
		return true
	}

	// Check for a net.OpError and a nested os.SyscallError indicating ECONNRESET
	var opErr *net.OpError
	if errors.As(err, &opErr) && opErr.Err != nil {
		var syscallErr *os.SyscallError
		if errors.As(opErr.Err, &syscallErr) && syscallErr.Err == syscall.ECONNRESET {
			return true
		}
	}

	return false
}
