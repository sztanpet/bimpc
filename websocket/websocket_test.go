package bimpcws

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"net/rpc"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/sztanpet/bimpc/internal/testmsg"
	"github.com/sztanpet/bimpc/mpc"
	"github.com/tinylib/msgp/msgp"
	"github.com/tv42/birpc"
)

const timeout = 5 * time.Second

var upgrader = websocket.Upgrader{}

// Peer is a service asking for the connection it was called over, which the
// codec has to hand it through FillArgs
type Peer struct {
	mu   sync.Mutex
	seen *websocket.Conn
}

// Who reports the address of the caller
func (p *Peer) Who(args *testmsg.Args, reply *testmsg.Result) error {
	return nil
}

// Whoami is the FillArgs variant, ws is filled in by the codec
func (p *Peer) Whoami(args *testmsg.Args, reply *testmsg.Result, ws *websocket.Conn) error {
	p.mu.Lock()
	p.seen = ws
	p.mu.Unlock()

	if ws == nil {
		return errors.New("codec did not fill in the connection")
	}
	reply.S = ws.RemoteAddr().String()
	return nil
}

func (p *Peer) conn() *websocket.Conn {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.seen
}

// serve starts an httptest server that upgrades and hands the connection to
// handle, and returns a dialed client connection
func serve(t *testing.T, handle func(*websocket.Conn)) *websocket.Conn {
	t.Helper()

	done := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ws, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Errorf("upgrade: %v", err)
			return
		}
		defer close(done)
		handle(ws)
	}))

	ws, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(srv.URL, "http"), nil)
	if err != nil {
		srv.Close()
		t.Fatalf("dial: %v", err)
	}

	t.Cleanup(func() {
		ws.Close()
		select {
		case <-done:
		case <-time.After(timeout):
			t.Error("server handler did not return")
		}
		srv.Close()
	})
	return ws
}

// newPair serves svc over a websocket and returns an endpoint talking to it
func newPair(t *testing.T, svc interface{}) *birpc.Endpoint {
	t.Helper()

	registry := birpc.NewRegistry()
	registry.RegisterService(svc)

	ws := serve(t, func(ws *websocket.Conn) {
		NewEndpoint(registry, ws).Serve()
	})

	client := NewEndpoint(nil, ws)
	go client.Serve()
	return client
}

func call(t *testing.T, e *birpc.Endpoint, fn string, args, reply interface{}) error {
	t.Helper()

	c := e.Go(fn, args, reply, make(chan *rpc.Call, 1))
	select {
	case <-c.Done:
		return c.Error
	case <-time.After(timeout):
		t.Fatalf("%s timed out", fn)
		return nil
	}
}

func TestCall(t *testing.T) {
	svc := &testmsg.Arith{}
	client := newPair(t, svc)

	var reply testmsg.Result
	if err := call(t, client, "Arith.Add", testmsg.Args{A: 2, B: 40, S: "hi"}, &reply); err != nil {
		t.Fatal(err)
	}
	if reply.C != 42 || reply.S != "hi" {
		t.Errorf("got %+v", reply)
	}
}

// Used to panic the client: the error reply was written through a nil pointer.
func TestCallReturningError(t *testing.T) {
	client := newPair(t, &testmsg.Arith{})

	var reply testmsg.Result
	err := call(t, client, "Arith.Explode", testmsg.Args{}, &reply)
	if err == nil {
		t.Fatal("expected an error")
	}
	if err.Error() != testmsg.ErrExploded.Error() {
		t.Errorf("got %q", err)
	}

	var reply2 testmsg.Result
	if err := call(t, client, "Arith.Add", testmsg.Args{A: 1, B: 2}, &reply2); err != nil {
		t.Fatalf("connection did not survive the error: %v", err)
	}
	if reply2.C != 3 {
		t.Errorf("got %+v", reply2)
	}
}

func TestCallUnknownFunction(t *testing.T) {
	client := newPair(t, &testmsg.Arith{})

	var reply testmsg.Result
	err := call(t, client, "Arith.Nope", testmsg.Args{}, &reply)
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "No such function") {
		t.Errorf("got %q", err)
	}
}

func TestFillArgsHandsOverTheConnection(t *testing.T) {
	svc := &Peer{}
	client := newPair(t, svc)

	var reply testmsg.Result
	if err := call(t, client, "Peer.Whoami", testmsg.Args{}, &reply); err != nil {
		t.Fatal(err)
	}
	if reply.S == "" {
		t.Error("no peer address in the reply")
	}
	if svc.conn() == nil {
		t.Error("service was called with a nil connection")
	}
}

func TestConcurrentCalls(t *testing.T) {
	const workers = 8
	const calls = 20

	svc := &testmsg.Arith{}
	client := newPair(t, svc)

	var wg sync.WaitGroup
	errs := make(chan error, workers*calls)
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for i := 0; i < calls; i++ {
				var reply testmsg.Result
				c := client.Go("Arith.Add", testmsg.Args{A: int64(w), B: int64(i)}, &reply, make(chan *rpc.Call, 1))
				select {
				case <-c.Done:
				case <-time.After(timeout):
					errs <- errors.New("timed out")
					return
				}
				if c.Error != nil {
					errs <- c.Error
					return
				}
				if reply.C != int64(w+i) {
					errs <- errors.New("wrong reply, frames got interleaved")
					return
				}
			}
		}(w)
	}
	wg.Wait()
	close(errs)

	for err := range errs {
		t.Error(err)
	}
	if n := svc.Calls.Get(); n != workers*calls {
		t.Errorf("service called %d times, want %d", n, workers*calls)
	}
}

// The javascript client decodes one message per frame, so every message has to
// be exactly one binary frame, never batched and never split.
func TestOneMessagePerBinaryFrame(t *testing.T) {
	const n = 3

	frames := make(chan []byte, n)
	ws := serve(t, func(ws *websocket.Conn) {
		for i := 0; i < n; i++ {
			mt, b, err := ws.ReadMessage()
			if err != nil {
				t.Errorf("read: %v", err)
				return
			}
			if mt != websocket.BinaryMessage {
				t.Errorf("frame %d is of type %d, want binary", i, mt)
			}
			frames <- b
		}
	})

	c := NewCodec(ws)
	for i := 1; i <= n; i++ {
		err := c.WriteMessage(&birpc.Message{ID: uint64(i), Func: "Arith.Add", Args: testmsg.Args{A: int64(i)}})
		if err != nil {
			t.Fatal(err)
		}
	}

	for i := 1; i <= n; i++ {
		select {
		case b := <-frames:
			var m mpc.Message
			left, err := m.UnmarshalMsg(b)
			if err != nil {
				t.Fatalf("frame %d: %v", i, err)
			}
			if len(left) != 0 {
				t.Errorf("frame %d carries %d extra bytes, messages got batched", i, len(left))
			}
			if m.ID != uint64(i) {
				t.Errorf("frame %d has id %d", i, m.ID)
			}
		case <-time.After(timeout):
			t.Fatalf("frame %d never arrived", i)
		}
	}
}

func TestReadMessageRejectsTextFrames(t *testing.T) {
	ws := serve(t, func(ws *websocket.Conn) {
		ws.WriteMessage(websocket.TextMessage, []byte(`{"id":1}`))
		// hold the connection open until the client is done with it
		ws.ReadMessage()
	})

	var msg birpc.Message
	err := NewCodec(ws).ReadMessage(&msg)
	if !errors.Is(err, ErrInvalidMsg) {
		t.Errorf("got %v, want ErrInvalidMsg", err)
	}
}

// A dead connection has to be reported as such. Checking the message type
// before the error turned every disconnect into ErrInvalidMsg, which looks
// like a broken peer instead of a closed socket.
func TestReadMessageReportsClosedConnection(t *testing.T) {
	ws := serve(t, func(ws *websocket.Conn) {
		ws.Close()
	})

	var msg birpc.Message
	err := NewCodec(ws).ReadMessage(&msg)
	if err == nil {
		t.Fatal("no error from a closed connection")
	}
	if errors.Is(err, ErrInvalidMsg) {
		t.Fatalf("a closed connection was reported as a bad message type: %v", err)
	}
}

func TestReadMessageRejectsGarbage(t *testing.T) {
	ws := serve(t, func(ws *websocket.Conn) {
		ws.WriteMessage(websocket.BinaryMessage, []byte{0x81, 0xa2, 0x69, 0x64, 0x01})
		ws.ReadMessage()
	})

	var msg birpc.Message
	err := NewCodec(ws).ReadMessage(&msg)
	if err == nil {
		t.Fatal("accepted a map encoded message")
	}
	if errors.Is(err, ErrInvalidMsg) {
		t.Errorf("wrong error for a decoding failure: %v", err)
	}
}

func TestCodecRoundTrip(t *testing.T) {
	msgs := make(chan birpc.Message, 4)
	ws := serve(t, func(ws *websocket.Conn) {
		c := NewCodec(ws)
		for {
			var m birpc.Message
			if err := c.ReadMessage(&m); err != nil {
				return
			}
			msgs <- m
		}
	})

	c := NewCodec(ws)
	sent := []birpc.Message{
		{ID: 1, Func: "Arith.Add", Args: testmsg.Args{A: 1, B: 2, S: "x"}},
		{ID: 2, Result: &testmsg.Result{C: 3}},
		{ID: 3, Error: &birpc.Error{Msg: "boom"}},
	}
	for i := range sent {
		if err := c.WriteMessage(&sent[i]); err != nil {
			t.Fatal(err)
		}
	}

	for i := range sent {
		select {
		case got := <-msgs:
			if got.ID != sent[i].ID || got.Func != sent[i].Func {
				t.Errorf("message %d: got %+v want %+v", i, got, sent[i])
			}
			if (got.Error == nil) != (sent[i].Error == nil) {
				t.Errorf("message %d: error mismatch %v vs %v", i, got.Error, sent[i].Error)
			}
		case <-time.After(timeout):
			t.Fatalf("message %d never arrived", i)
		}
	}
}

// A failed marshal must not leave a half written frame behind
func TestWriteMessageKeepsStreamIntactOnError(t *testing.T) {
	msgs := make(chan birpc.Message, 1)
	ws := serve(t, func(ws *websocket.Conn) {
		c := NewCodec(ws)
		for {
			var m birpc.Message
			if err := c.ReadMessage(&m); err != nil {
				return
			}
			msgs <- m
		}
	})

	c := NewCodec(ws)
	if err := c.WriteMessage(&birpc.Message{ID: 1, Func: "X.Y", Args: testmsg.Plain{}}); err == nil {
		t.Fatal("WriteMessage accepted an unserializable payload")
	}
	if err := c.WriteMessage(&birpc.Message{ID: 2, Func: "Arith.Add", Args: testmsg.Args{A: 7}}); err != nil {
		t.Fatal(err)
	}

	select {
	case got := <-msgs:
		if got.ID != 2 {
			t.Errorf("got id %d, want the message written after the failure", got.ID)
		}
	case <-time.After(timeout):
		t.Fatal("nothing arrived, the connection is wedged")
	}
}

func TestWriteMessageReportsClosedConnection(t *testing.T) {
	ws := serve(t, func(ws *websocket.Conn) {
		ws.Close()
	})

	c := NewCodec(ws)
	// the first write may still succeed into the local buffer, the second one
	// cannot
	var err error
	for i := 0; i < 10 && err == nil; i++ {
		err = c.WriteMessage(&birpc.Message{ID: uint64(i), Func: "Arith.Add", Args: testmsg.Args{A: 1}})
	}
	if err == nil {
		t.Error("writing to a closed connection never failed")
	}
}

func TestCloseClosesTheConnection(t *testing.T) {
	ws := serve(t, func(ws *websocket.Conn) {
		ws.ReadMessage()
	})

	if err := NewCodec(ws).Close(); err != nil {
		t.Fatal(err)
	}
	if err := ws.WriteMessage(websocket.BinaryMessage, []byte{0x00}); err == nil {
		t.Error("connection still writable after Close")
	}
}

func TestUnmarshalArgsAndResult(t *testing.T) {
	c := NewCodec(nil)

	raw, err := testmsg.Args{A: 1, B: 2, S: "s"}.MarshalMsg(nil)
	if err != nil {
		t.Fatal(err)
	}

	var args testmsg.Args
	if err := c.UnmarshalArgs(&birpc.Message{Args: msgp.Raw(raw)}, &args); err != nil {
		t.Fatal(err)
	}
	if args.A != 1 || args.B != 2 || args.S != "s" {
		t.Errorf("got %+v", args)
	}

	rawres, err := testmsg.Result{C: 9}.MarshalMsg(nil)
	if err != nil {
		t.Fatal(err)
	}
	var res testmsg.Result
	if err := c.UnmarshalResult(&birpc.Message{Result: msgp.Raw(rawres)}, &res); err != nil {
		t.Fatal(err)
	}
	if res.C != 9 {
		t.Errorf("got %+v", res)
	}
}

func TestFillArgsLeavesUnknownTypesAlone(t *testing.T) {
	ws := serve(t, func(ws *websocket.Conn) {
		ws.ReadMessage()
	})

	c := NewCodec(ws)
	args := []reflect.Value{
		reflect.Zero(reflect.TypeOf((*websocket.Conn)(nil))),
		reflect.Zero(reflect.TypeOf(0)),
	}
	if err := c.FillArgs(args); err != nil {
		t.Fatal(err)
	}
	if args[0].IsNil() {
		t.Error("the connection was not filled in")
	}
	if args[1].Interface() != 0 {
		t.Error("an unknown argument type was touched")
	}
}
