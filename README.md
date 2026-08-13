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

## Benchmarks

Against birpc's own JSON codecs, `jsonmsg` and `wetsock`, over the same
connection with the same service and the same message types. Run them with

    go test -bench . -benchtime 2s ./generic/ ./websocket/

Median of five runs, go1.26.5, linux/amd64, Ryzen 5 5600G. `numbers` is two
int64s, `string256` is two int64s and a 256 byte string.

Encoding, with the connection stubbed out so only the codec is timed:

| operation             | payload   | msgpack       | json           |
| --------------------- | --------- | ------------- | -------------- |
| WriteMessage          | numbers   | 67 ns, 32 B   | 271 ns, 74 B   |
| WriteMessage          | string256 | 100 ns, 282 B | 440 ns, 312 B  |
| ReadMessage           | numbers   | 256 ns        | 1091 ns        |
| ReadMessage           | string256 | 302 ns        | 2620 ns        |
| UnmarshalArgs         | numbers   | 46 ns         | 787 ns         |
| UnmarshalArgs         | string256 | 87 ns         | 2355 ns        |

Decoding a payload is where generated code pulls away from reflection: 17x on
numbers, 27x on the string. Encoding is 4x. The byte counts are the whole
message, envelope included.

Round trip, request out and reply back over loopback TCP:

| benchmark          | msgpack | json    |
| ------------------ | ------- | ------- |
| Call/numbers       | 34.7 µs | 51.2 µs |
| Call/string256     | 36.3 µs | 58.1 µs |
| CallParallel       | 13.8 µs | 19.5 µs |

And over a websocket, where the sizes include the frame header:

| benchmark               | msgpack       | json          |
| ----------------------- | ------------- | ------------- |
| WriteMessage/numbers    | 4.0 µs, 38 B  | 4.6 µs, 80 B  |
| WriteMessage/string256  | 3.7 µs, 290 B | 4.8 µs, 320 B |
| Call/numbers            | 37.3 µs       | 53.8 µs       |
| Call/string256          | 38.9 µs       | 61.6 µs       |
| CallParallel            | 15.2 µs       | 21.4 µs       |

Some honesty about all this:

- A round trip is 35 µs of which the codec is under one. What shows up in the
  Call numbers is mostly the smaller messages and the allocations, not the
  encoding speed. Anyone reading a 4x and expecting their RPCs to get 4x
  faster will be disappointed.
- Writing a websocket frame costs a syscall, around 4 µs, which swamps the
  difference entirely. The reason to use this codec over a websocket is the
  bytes, not the CPU.
- `jsonmsg` allocates nothing per write, this codec allocates once, because
  the arguments are marshaled into a fresh buffer before being copied into the
  stream. That is the one column where JSON wins and it is fixable.
- `wetsock` had to be wrapped in a mutex to be benchmarked at all: it does not
  serialise its writes, and birpc always sends from a goroutine, so gorilla
  panics after a few thousand calls. The wrapper is charged to it as a plain
  lock, which is the cheapest fix it could get.

## Example

`examples/chat` is a browser-to-browser chat room over websockets. Run it with

    ./examples/chat/run

and open two browser windows at http://localhost:8000/.
