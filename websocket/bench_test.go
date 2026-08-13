package bimpcws

import (
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/gorilla/websocket"
	"github.com/sztanpet/bimpc/internal/testmsg"
	"github.com/tv42/birpc"
	"github.com/tv42/birpc/wetsock"
)

// birpc ships wetsock for exactly this transport, so that is what to measure
// against: same connection, same service, same payload, different encoding.
var wsCodecs = []struct {
	name string
	new  func(*websocket.Conn) birpc.Codec
}{
	{"msgpack", func(ws *websocket.Conn) birpc.Codec { return NewCodec(ws) }},
	{"json", func(ws *websocket.Conn) birpc.Codec { return &lockedCodec{Codec: wetsock.NewCodec(ws)} }},
}

// lockedCodec serialises WriteMessage.
//
// birpc.Codec says WriteMessage may be called concurrently and that codecs
// have to protect themselves; wetsock hands the message straight to WriteJSON
// with no lock at all, and gorilla panics with "concurrent write to websocket
// connection" as soon as two writes overlap. That is not hypothetical:
// Endpoint.Go always sends from a fresh goroutine, so even a caller doing one
// blocking Call after another has the next send racing the tail of the
// previous one, and an unwrapped wetsock takes the process down within a few
// thousand calls.
//
// Wrapping it keeps the benchmark about the encoding rather than about the
// bug, and hands wetsock the benefit of the doubt: this lock is the least it
// would cost to fix.
type lockedCodec struct {
	birpc.Codec
	mu sync.Mutex
}

func (c *lockedCodec) WriteMessage(msg *birpc.Message) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.Codec.WriteMessage(msg)
}

var benchPayloads = []struct {
	name string
	args testmsg.Args
}{
	{"numbers", testmsg.Args{A: 1234567890, B: -987654321}},
	{"string256", testmsg.Args{A: 1, B: 2, S: strings.Repeat("x", 256)}},
}

// B/msg here is the whole frame, header included, which is what a browser
// actually pays for.
func BenchmarkWriteMessage(b *testing.B) {
	for _, c := range wsCodecs {
		for _, p := range benchPayloads {
			b.Run(c.name+"/"+p.name, func(b *testing.B) {
				var sent int64
				ws := benchDial(b, &sent, func(ws *websocket.Conn) {
					for {
						if _, _, err := ws.NextReader(); err != nil {
							return
						}
					}
				})

				codec := c.new(ws)
				msg := &birpc.Message{ID: 1, Func: "Arith.Add", Args: p.args}

				b.ReportAllocs()
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					if err := codec.WriteMessage(msg); err != nil {
						b.Fatal(err)
					}
				}
				b.StopTimer()
				b.ReportMetric(float64(atomic.LoadInt64(&sent))/float64(b.N), "B/msg")
			})
		}
	}
}

func BenchmarkCall(b *testing.B) {
	for _, c := range wsCodecs {
		for _, p := range benchPayloads {
			b.Run(c.name+"/"+p.name, func(b *testing.B) {
				client := benchClient(b, c.new)

				b.ReportAllocs()
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					var reply testmsg.Result
					if err := client.Call("Arith.Add", p.args, &reply); err != nil {
						b.Fatal(err)
					}
				}
			})
		}
	}
}

func BenchmarkCallParallel(b *testing.B) {
	for _, c := range wsCodecs {
		b.Run(c.name, func(b *testing.B) {
			client := benchClient(b, c.new)
			args := benchPayloads[0].args

			b.ReportAllocs()
			b.ResetTimer()
			b.RunParallel(func(pb *testing.PB) {
				for pb.Next() {
					var reply testmsg.Result
					if err := client.Call("Arith.Add", args, &reply); err != nil {
						b.Fatal(err)
					}
				}
			})
		})
	}
}

// benchClient serves testmsg.Arith over a websocket and returns an endpoint
// talking to it
func benchClient(b *testing.B, newCodec func(*websocket.Conn) birpc.Codec) *birpc.Endpoint {
	b.Helper()

	registry := birpc.NewRegistry()
	registry.RegisterService(&testmsg.Arith{})

	ws := benchDial(b, nil, func(ws *websocket.Conn) {
		birpc.NewEndpoint(newCodec(ws), registry).Serve()
	})

	client := birpc.NewEndpoint(newCodec(ws), nil)
	go client.Serve()

	// warm up, so the first timed call is not paying for the handshake
	var reply testmsg.Result
	if err := client.Call("Arith.Add", testmsg.Args{}, &reply); err != nil {
		b.Fatal(err)
	}
	return client
}

// benchDial starts a server that hands accepted connections to handle and
// dials it. When sent is non-nil every byte written to the wire is counted
// into it, framing included.
func benchDial(b *testing.B, sent *int64, handle func(*websocket.Conn)) *websocket.Conn {
	b.Helper()

	var up websocket.Upgrader
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ws, err := up.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		handle(ws)
	}))

	dialer := *websocket.DefaultDialer
	if sent != nil {
		dialer.NetDial = func(network, addr string) (net.Conn, error) {
			conn, err := net.Dial(network, addr)
			if err != nil {
				return nil, err
			}
			return &countingConn{Conn: conn, n: sent}, nil
		}
	}

	ws, _, err := dialer.Dial("ws"+strings.TrimPrefix(srv.URL, "http"), nil)
	if err != nil {
		srv.Close()
		b.Fatalf("dial: %v", err)
	}

	b.Cleanup(func() {
		ws.Close()
		srv.Close()
	})

	// the handshake is not part of what we are counting
	if sent != nil {
		atomic.StoreInt64(sent, 0)
	}
	return ws
}

type countingConn struct {
	net.Conn
	n *int64
}

func (c *countingConn) Write(p []byte) (int, error) {
	n, err := c.Conn.Write(p)
	atomic.AddInt64(c.n, int64(n))
	return n, err
}
