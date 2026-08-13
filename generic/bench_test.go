package bimpcgen

import (
	"io"
	"net"
	"strings"
	"testing"

	"github.com/sztanpet/bimpc/internal/testmsg"
	"github.com/tv42/birpc"
	"github.com/tv42/birpc/jsonmsg"
)

// The comparison is against birpc's own codec over the same connection type,
// with the same service, the same payloads and the same message types. The
// only difference is what goes on the wire.
var codecs = []struct {
	name string
	new  func(io.ReadWriteCloser) birpc.Codec
}{
	{"msgpack", func(conn io.ReadWriteCloser) birpc.Codec { return NewCodec(conn) }},
	{"json", func(conn io.ReadWriteCloser) birpc.Codec { return jsonmsg.NewCodec(conn) }},
}

// two shapes, because the answer differs: numbers are where a binary format
// wins, a long string is the same bytes either way
var payloads = []struct {
	name string
	args testmsg.Args
}{
	{"numbers", testmsg.Args{A: 1234567890, B: -987654321}},
	{"string256", testmsg.Args{A: 1, B: 2, S: strings.Repeat("x", 256)}},
}

func BenchmarkWriteMessage(b *testing.B) {
	for _, c := range codecs {
		for _, p := range payloads {
			b.Run(c.name+"/"+p.name, func(b *testing.B) {
				conn := &nullConn{}
				codec := c.new(conn)
				msg := &birpc.Message{ID: 1, Func: "Arith.Add", Args: p.args}

				b.ReportAllocs()
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					if err := codec.WriteMessage(msg); err != nil {
						b.Fatal(err)
					}
				}
				b.StopTimer()
				b.ReportMetric(float64(conn.written)/float64(b.N), "B/msg")
			})
		}
	}
}

func BenchmarkReadMessage(b *testing.B) {
	for _, c := range codecs {
		for _, p := range payloads {
			b.Run(c.name+"/"+p.name, func(b *testing.B) {
				codec := c.new(&repeatConn{msg: encodeOne(b, c.new, p.args)})

				b.ReportAllocs()
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					var msg birpc.Message
					if err := codec.ReadMessage(&msg); err != nil {
						b.Fatal(err)
					}
				}
			})
		}
	}
}

// Decoding the payload is the half of the work that reflection based codecs
// pay for and generated ones do not.
func BenchmarkUnmarshalArgs(b *testing.B) {
	for _, c := range codecs {
		for _, p := range payloads {
			b.Run(c.name+"/"+p.name, func(b *testing.B) {
				codec := c.new(&repeatConn{msg: encodeOne(b, c.new, p.args)})

				var msg birpc.Message
				if err := codec.ReadMessage(&msg); err != nil {
					b.Fatal(err)
				}

				// hoisted: a fresh one per iteration escapes into the
				// interface and would be counted against the codec
				var args testmsg.Args

				b.ReportAllocs()
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					if err := codec.UnmarshalArgs(&msg, &args); err != nil {
						b.Fatal(err)
					}
				}
			})
		}
	}
}

// End to end over loopback tcp: request out, service call, reply back. This is
// the number that matters to anybody choosing a codec, and it is the one where
// the difference is smallest, because most of it is the network and the
// scheduler.
func BenchmarkCall(b *testing.B) {
	for _, c := range codecs {
		for _, p := range payloads {
			b.Run(c.name+"/"+p.name, func(b *testing.B) {
				client := benchEndpoint(b, c.new)

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

// The same, with calls in flight from several goroutines, which is where the
// codec's write lock shows up.
func BenchmarkCallParallel(b *testing.B) {
	for _, c := range codecs {
		b.Run(c.name, func(b *testing.B) {
			client := benchEndpoint(b, c.new)
			args := payloads[0].args

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

// benchEndpoint serves testmsg.Arith over loopback tcp and returns an endpoint
// talking to it
func benchEndpoint(b *testing.B, newCodec func(io.ReadWriteCloser) birpc.Codec) *birpc.Endpoint {
	b.Helper()

	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		b.Fatal(err)
	}

	registry := birpc.NewRegistry()
	registry.RegisterService(&testmsg.Arith{})

	served := make(chan net.Conn, 1)
	go func() {
		conn, err := l.Accept()
		if err != nil {
			return
		}
		served <- conn
		birpc.NewEndpoint(newCodec(conn), registry).Serve()
	}()

	conn, err := net.Dial("tcp", l.Addr().String())
	if err != nil {
		l.Close()
		b.Fatal(err)
	}

	client := birpc.NewEndpoint(newCodec(conn), nil)
	go client.Serve()

	b.Cleanup(func() {
		conn.Close()
		l.Close()
		select {
		case c := <-served:
			c.Close()
		default:
		}
	})

	// warm the connection up so the first timed call is not paying for the
	// handshake
	var reply testmsg.Result
	if err := client.Call("Arith.Add", testmsg.Args{}, &reply); err != nil {
		b.Fatal(err)
	}
	return client
}

// encodeOne returns the wire bytes of one request as written by the codec
// under test
func encodeOne(b *testing.B, newCodec func(io.ReadWriteCloser) birpc.Codec, args testmsg.Args) []byte {
	b.Helper()

	conn := &nullConn{keep: true}
	if err := newCodec(conn).WriteMessage(&birpc.Message{ID: 1, Func: "Arith.Add", Args: args}); err != nil {
		b.Fatal(err)
	}
	return conn.buf
}

// nullConn counts what is written to it, and optionally keeps it
type nullConn struct {
	written int
	keep    bool
	buf     []byte
}

func (c *nullConn) Read([]byte) (int, error) { return 0, io.EOF }
func (c *nullConn) Close() error             { return nil }

func (c *nullConn) Write(p []byte) (int, error) {
	c.written += len(p)
	if c.keep {
		c.buf = append(c.buf, p...)
	}
	return len(p), nil
}

// repeatConn serves the same message over and over, so a decoder never runs
// out of input
type repeatConn struct {
	msg []byte
	off int
}

func (c *repeatConn) Write(p []byte) (int, error) { return len(p), nil }
func (c *repeatConn) Close() error                { return nil }

func (c *repeatConn) Read(p []byte) (int, error) {
	n := copy(p, c.msg[c.off:])
	c.off += n
	if c.off == len(c.msg) {
		c.off = 0
	}
	return n, nil
}
