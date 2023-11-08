# brpc
A low-invasive, bidirectional gRPC framework for Go. This library is a proof of concept that allows your clients to connect to a gRPC server, and then expose a gRPC server of their own, allowing the real gRPC server to call RPCs on the client.

```go
func (s *GreeterService) Greet(ctx context.Context, _ *example.GreetRequest) (*example.GreetResponse, error) {
	// The client provides a gRPC service. Let's extract it from the context.
	client, err := s.ClientFromContext(ctx)
	if err != nil {
		return nil, err
	}
	// Let's call the client's Name method, an RPC on the client's gRPC service.
	res, err := client.Name(ctx, &example.NameRequest{})
	if err != nil {
		return nil, err
	}
	return &example.GreetResponse{
		Greeting: fmt.Sprintf("Hello %v", res.GetName()),
	}, nil
}
```

## Features
* **Bidirectional** - Clients can expose a gRPC server of their own, allowing the real gRPC server to call RPCs on the client.
* **Low-invasive** - Takes advantage of all the generated types and functions from `protoc`, you just need to plug everything into brpc.
* **Single connection** - All connections are multiplexed across a single QUIC connection.
* **Go generics** - Uses Go generics to make it easy to plug everything together correctly.

## Internals
This library uses a single QUIC connection and all other connections are multiplexed across this connection. Clients receive connection IDs from the server which they then provide with every subsequent client-to-server RPC request, and the brpc server exposes the client's RPC methods inside your gRPC service so that you can call them from the server.


## Example
See [EXAMPLE.md](EXAMPLE.md) for a full example.