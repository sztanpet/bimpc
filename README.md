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

Encoding, with the connection stubbed out so only the codec is timed. `size`
is the whole message, envelope included:

| operation     | payload   | msgpack                 | json                     |
| ------------- | --------- | ----------------------- | ------------------------ |
| WriteMessage  | numbers   | 63 ns, 0 allocs, 32 B   | 268 ns, 0 allocs, 74 B   |
| WriteMessage  | string256 | 64 ns, 0 allocs, 282 B  | 443 ns, 0 allocs, 312 B  |
| ReadMessage   | numbers   | 252 ns, 176 B, 9 allocs | 1086 ns, 256 B, 7 allocs |
| ReadMessage   | string256 | 295 ns, 432 B, 9 allocs | 2594 ns, 496 B, 7 allocs |
| UnmarshalArgs | numbers   | 47 ns, 32 B, 1 alloc    | 773 ns, 248 B, 5 allocs  |
| UnmarshalArgs | string256 | 88 ns, 288 B, 2 allocs  | 2329 ns, 504 B, 6 allocs |

Decoding a payload is where generated code pulls away from reflection: 16x on
numbers, 27x on the string. Writing is 4x to 7x and allocates nothing.

Round trip, request out and reply back over loopback TCP:

| benchmark      | msgpack                  | json                      |
| -------------- | ------------------------ | ------------------------- |
| Call/numbers   | 35.5 µs, 880 B, 30 allocs  | 51.3 µs, 1488 B, 37 allocs |
| Call/string256 | 37.2 µs, 1928 B, 33 allocs | 58.2 µs, 2505 B, 39 allocs |
| CallParallel   | 14.1 µs                    | 19.7 µs                    |

And over a websocket, where the sizes include the frame header:

| benchmark              | msgpack                    | json                       |
| ---------------------- | -------------------------- | -------------------------- |
| WriteMessage/numbers   | 4.0 µs, 38 B               | 4.7 µs, 80 B               |
| WriteMessage/string256 | 3.7 µs, 290 B              | 4.6 µs, 320 B              |
| Call/numbers           | 37.9 µs, 992 B, 34 allocs  | 54.7 µs, 3488 B, 57 allocs |
| Call/string256         | 38.9 µs, 2040 B, 37 allocs | 61.8 µs, 4505 B, 59 allocs |
| CallParallel           | 15.8 µs                    | 22.0 µs                    |

Some honesty about all this:

- A round trip is 35 µs of which the codec is under one. What shows up in the
  Call numbers is mostly the smaller messages and the allocations, not the
  encoding speed. Anyone reading a 4x and expecting their RPCs to get 4x
  faster will be disappointed.
- Writing a websocket frame costs a syscall, around 4 µs, which swamps the
  difference entirely. Those two rows also move by 10% between runs. The
  reason to use this codec over a websocket is the bytes, not the CPU.
- Writing allocates nothing because the wire message and its payload buffer
  come from a pool. That is an average, not a guarantee: the garbage collector
  empties the pool, so a sender that goes quiet and then bursts pays for the
  buffers again.
- The read path still allocates more per message than `jsonmsg` does, and is
  still four times faster. That is the next thing worth fixing here.
- `wetsock` had to be wrapped in a mutex to be benchmarked at all: it does not
  serialise its writes, and birpc always sends from a goroutine, so gorilla
  panics after a few thousand calls. The wrapper is charged to it as a plain
  lock, which is the cheapest fix it could get.

## Example

`examples/chat` is a browser-to-browser chat room over websockets. Run it with

    ./examples/chat/run

and open two browser windows at http://localhost:8000/.
