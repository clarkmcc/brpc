# Example
The following example illustrates the mechanics of brpc, however you'd never actually use it in this way (just send the name in the initial request). The goal is to simply illustrate the capabilities of this approach.

The following protobuf describes two services:
* **Greeter** - Is served by the serve, and greets whatever client connects to it, however, the request doesn't provide the name of the person to greet.
* **Identifier** - Is served by the client, and allows the server to ask the client for their name.

```protobuf
service Greeter {
  rpc Greet(GreetRequest) returns (GreetResponse);
}

service Identifier {
  rpc Identity(IdentityRequest) returns (IdentityResponse);
}

message GreetRequest {}
message GreetResponse {
  string greeting = 1;
}

message IdentityRequest {}
message IdentityResponse{
  string name = 1;
}
```

## Create the Server
The server supports Go generics and needs to know a couple of things:
* The type of the gRPC service that the server is going to serve.
* The type of the gRPC service that the client is going to serve.
* A function that builds the gRPC client we use to connect from the server to the client.
* A function that registers the server's gRPC service with brpc (if brpc is managing the gRPC server).

Fortunately, virtually all of this is generated for us by protoc, so it's just a matter of passing the types and constructors to brpc. Once the brpc server is created with the appropriate types and constructors, you'll notice that everything else is nearly identical to using a real gRPC server.

One other thing to note is if we want our custom service implementation `myServiceImplementation` to be able to access the client methods, we need to embed the `brpc.Server` into it. This allows our RPC methods to call `ClientFromContext` to get a client to call RPCs on.

Notice how we can now call the `client.Identity` method from the server.

```go
package main

import (
	"github.com/clarkmcc/brpc"
	"github.com/clarkmcc/brpc/internal/example"
)

func main() {
	server := brpc.NewServer(brpc.ServerConfig[example.GreeterServer, example.NamerClient]{
		ClientServiceBuilder: example.NewNamerClient,
		ServerServiceBuilder: func(server *brpc.Server[example.GreeterServer, example.NamerClient], registrar grpc.ServiceRegistrar) {
			example.RegisterGreeterServer(registrar, &GreeterService{Server: server})
		},
	})
	panic(server.Serve(":10000"))
}

type GreeterService struct {
	example.UnimplementedGreeterServer
	*brpc.Server[example.GreeterServer, example.NamerClient]
}

func (s *GreeterService) Greet(ctx context.Context, _ *example.GreetRequest) (*example.GreetResponse, error) {
	client, err := s.ClientFromContext(ctx)
	if err != nil {
		return nil, err
	}
	res, err := client.Name(ctx, &example.NameRequest{})
	if err != nil {
		return nil, err
	}
	return &example.GreetResponse{
		Greeting: fmt.Sprintf("Hello %v", res.GetName()),
	}, nil
}

```

# Create the Client
Creating the client requires less steps, we need to know:
* The type of the gRPC server we're connecting to.
* A function that registers the client's gRPC service with brpc.

Again, both ot these are generated for us by protoc. Similar to a real gRPC connection we:
1. Start by dialing the server, but we use `brpc.Dial` instead of `grpc.Dial`.
2. Create an instance of our typed gRPC client using `brpc.Client`.
3. Call server gRPC methods like normal. 

```go
package main

import (
	"github.com/clarkmcc/brpc"
	"github.com/clarkmcc/brpc/internal/example"
)

func main() {
	conn, err := brpc.Dial[example.GreeterServer]("127.0.0.1:10000", func(r grpc.ServiceRegistrar) {
		example.RegisterNamerServer(r, &myClientService{})
	})
	if err != nil {
		panic(err)
	}
	defer conn.Close()

	client, err := brpc.Client(conn, example.NewGreeterClient)
	if err != nil {
		return err
	}
	res, err := client.Greet(context.Background(), &example.GreetRequest{})
	if err != nil {
		return err
	}
	fmt.Printf("Got greeting: %v\n", res.GetGreeting())
}

type myClientService struct {
	example.UnimplementedNamerServer
}

func (s *myClientService) Name(_ context.Context, _ *example.NameRequest) (*example.NameResponse, error) {
	return &example.NameResponse{Name: "brpc"}, nil
}

```