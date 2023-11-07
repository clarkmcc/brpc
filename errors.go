package brpc

import "errors"

var (
	ErrClientNotConnected = errors.New("client not connected")
)

const (
	ErrorCodeCreatingYamuxClient = iota + 1
	ErrorCodeOpeningGrpcConnection
)

type ErrYamuxNegotiationFailed struct {
	code  int
	inner error
}

func (e ErrYamuxNegotiationFailed) Error() string {
	return e.inner.Error()
}
