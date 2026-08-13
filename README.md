# MessagePack codecs for the Bi-directional RPC library for Go

Codecs for [tv42/birpc](https://github.com/tv42/birpc) that put
[MessagePack](https://msgpack.org/) on the wire instead of JSON, using
[tinylib/msgp](https://github.com/tinylib/msgp) for generated,
reflection-free en/decoding.

    go get github.com/sztanpet/bimpc

Everything here is based upon existing parts of https://github.com/tv42/birpc

## Codecs

| Package                                 | Transport            |
| --------------------------------------- | -------------------- |
| `github.com/sztanpet/bimpc/websocket`   | WebSockets           |
| `github.com/sztanpet/bimpc/redis`       | Redis pub-sub        |
| `github.com/sztanpet/bimpc/generic`     | any io.ReadWriteCloser |

Each one exposes the same two constructors:

```go
registry := birpc.NewRegistry()
registry.RegisterService(&myService{})

endpoint := bimpcws.NewEndpoint(registry, ws)   // websocket
endpoint := bimpcgen.NewEndpoint(registry, conn) // any connection
endpoint := bimpcrds.NewEndpoint(registry, db, "in.channel", "out.channel")

if err := endpoint.Serve(); err != nil {
	log.Print(err)
}
```

The redis codec takes two channels, one to subscribe to and one to publish on.
They must differ: redis delivers a published message to every subscriber of
that channel, the publisher included, so an endpoint listening where it
publishes would serve its own requests. Two endpoints talk by mirroring them.

The websocket and redis codecs also implement `birpc.FillArgser`, so a service
method can ask for the `*websocket.Conn` or the `*redis.Database` it was called
over by taking one as a third argument.

## Types on the wire

Arguments and replies MUST implement `msgp.Marshaler` and `msgp.Unmarshaler`,
which means running msgp over them:

```go
//go:generate msgp

type Args struct {
	A int64 `msg:"a"`
	B int64 `msg:"b"`
}
```

A payload that does not implement them is refused rather than sent as an empty
field.

## Wire format

A message is a five element array, and an error is a one element array:

    [id, fn, args, result, error]
    [msg]

`args` and `result` are the raw MessagePack the type marshaled itself into,
whatever that happens to be. A non-Go peer only needs to be able to build and
read those two arrays; `examples/chat/chat.js` does it in about fifteen lines.

## Example

`examples/chat` is a browser-to-browser chat room over websockets. Run it with

    ./examples/chat/run

and open two browser windows at http://localhost:8000/.
