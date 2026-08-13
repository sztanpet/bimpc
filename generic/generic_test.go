package bimpcgen

import (
	"errors"
	"io"
	"net"
	"net/rpc"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sztanpet/bimpc/internal/testmsg"
	"github.com/sztanpet/bimpc/mpc"
	"github.com/tinylib/msgp/msgp"
	"github.com/tv42/birpc"
)

const timeout = 5 * time.Second

// pair wires two endpoints together over a net.Pipe and serves both
type pair struct {
	client, server *birpc.Endpoint
	errs           chan error
}

func newPair(t *testing.T, clientSvc, serverSvc interface{}) *pair {
	t.Helper()

	c, s := net.Pipe()
	p := &pair{errs: make(chan error, 2)}
	p.client = NewEndpoint(registry(clientSvc), c)
	p.server = NewEndpoint(registry(serverSvc), s)

	var wg sync.WaitGroup
	for _, e := range []*birpc.Endpoint{p.client, p.server} {
		wg.Add(1)
		go func(e *birpc.Endpoint) {
			wg.Done()
			p.errs <- e.Serve()
		}(e)
	}
	wg.Wait()

	t.Cleanup(func() {
		c.Close()
		s.Close()
	})
	return p
}

func registry(svc interface{}) *birpc.Registry {
	if svc == nil {
		return nil
	}
	r := birpc.NewRegistry()
	r.RegisterService(svc)
	return r
}

// call is birpc's Call with a deadline, so a hang fails the test instead of
// wedging the whole run
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
	p := newPair(t, nil, svc)

	var reply testmsg.Result
	if err := call(t, p.client, "Arith.Add", testmsg.Args{A: 2, B: 40, S: "hi"}, &reply); err != nil {
		t.Fatal(err)
	}
	if reply.C != 42 || reply.S != "hi" {
		t.Errorf("got %+v", reply)
	}
	if n := svc.Calls.Get(); n != 1 {
		t.Errorf("service called %d times", n)
	}
}

func TestCallPointerArgs(t *testing.T) {
	p := newPair(t, nil, &testmsg.Arith{})

	var reply testmsg.Result
	if err := call(t, p.client, "Arith.Add", &testmsg.Args{A: 1, B: 1}, &reply); err != nil {
		t.Fatal(err)
	}
	if reply.C != 2 {
		t.Errorf("got %+v", reply)
	}
}

// An error reply used to panic the receiving side: birpc hands ReadMessage a
// zero valued Message, and the codec wrote through its nil Error pointer.
func TestCallReturningError(t *testing.T) {
	p := newPair(t, nil, &testmsg.Arith{})

	var reply testmsg.Result
	err := call(t, p.client, "Arith.Explode", testmsg.Args{}, &reply)
	if err == nil {
		t.Fatal("expected an error")
	}
	if _, ok := err.(rpc.ServerError); !ok {
		t.Errorf("expected an rpc.ServerError, got %T: %v", err, err)
	}
	if err.Error() != testmsg.ErrExploded.Error() {
		t.Errorf("got %q", err)
	}

	// the connection has to survive it
	var reply2 testmsg.Result
	if err := call(t, p.client, "Arith.Add", testmsg.Args{A: 1, B: 2}, &reply2); err != nil {
		t.Fatal(err)
	}
	if reply2.C != 3 {
		t.Errorf("got %+v", reply2)
	}
}

func TestCallUnknownFunction(t *testing.T) {
	p := newPair(t, nil, &testmsg.Arith{})

	var reply testmsg.Result
	err := call(t, p.client, "Arith.Nope", testmsg.Args{}, &reply)
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "No such function") {
		t.Errorf("got %q", err)
	}
}

// The point of birpc: the server calls the client just as well.
func TestBidirectional(t *testing.T) {
	clientSvc := &testmsg.Arith{}
	serverSvc := &testmsg.Arith{}
	p := newPair(t, clientSvc, serverSvc)

	var reply testmsg.Result
	if err := call(t, p.server, "Arith.Add", testmsg.Args{A: 20, B: 22}, &reply); err != nil {
		t.Fatal(err)
	}
	if reply.C != 42 {
		t.Errorf("got %+v", reply)
	}
	if clientSvc.Calls.Get() != 1 || serverSvc.Calls.Get() != 0 {
		t.Errorf("wrong side served the call: client=%d server=%d",
			clientSvc.Calls.Get(), serverSvc.Calls.Get())
	}
}

// WriteMessage is documented as safe for concurrent use, and replies are
// written from one goroutine per call, so the framing has to hold up.
func TestConcurrentCalls(t *testing.T) {
	const workers = 8
	const calls = 25

	svc := &testmsg.Arith{}
	p := newPair(t, nil, svc)

	var wg sync.WaitGroup
	errs := make(chan error, workers*calls)
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for i := 0; i < calls; i++ {
				var reply testmsg.Result
				c := p.client.Go("Arith.Add", testmsg.Args{A: int64(w), B: int64(i)}, &reply, make(chan *rpc.Call, 1))
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
				if want := int64(w + i); reply.C != want {
					errs <- errors.New("wrong reply, framing is off")
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

// Several calls in flight at once, answered out of order.
func TestCallsInFlight(t *testing.T) {
	svc := &testmsg.Arith{Block: make(chan struct{})}
	p := newPair(t, nil, svc)

	const n = 5
	calls := make([]*rpc.Call, n)
	replies := make([]testmsg.Result, n)
	for i := 0; i < n; i++ {
		calls[i] = p.client.Go("Arith.Wait", testmsg.Args{A: int64(i)}, &replies[i], make(chan *rpc.Call, 1))
	}

	close(svc.Block)

	for i, c := range calls {
		select {
		case <-c.Done:
		case <-time.After(timeout):
			t.Fatalf("call %d timed out", i)
		}
		if c.Error != nil {
			t.Errorf("call %d: %v", i, c.Error)
		}
		if replies[i].C != int64(i) {
			t.Errorf("call %d: got %+v, replies got mixed up", i, replies[i])
		}
	}
}

func TestCodecRoundTrip(t *testing.T) {
	a, b := net.Pipe()
	defer a.Close()
	defer b.Close()

	ca, cb := NewCodec(a), NewCodec(b)

	sent := []birpc.Message{
		{ID: 1, Func: "Arith.Add", Args: testmsg.Args{A: 1, B: 2, S: "x"}},
		{ID: 2, Result: &testmsg.Result{C: 3}},
		{ID: 3, Error: &birpc.Error{Msg: "boom"}},
		{},
	}

	done := make(chan error, 1)
	go func() {
		for i := range sent {
			if err := ca.WriteMessage(&sent[i]); err != nil {
				done <- err
				return
			}
		}
		done <- nil
	}()

	for i := range sent {
		var got birpc.Message
		if err := cb.ReadMessage(&got); err != nil {
			t.Fatalf("message %d: %v", i, err)
		}
		if got.ID != sent[i].ID || got.Func != sent[i].Func {
			t.Errorf("message %d: header got %+v want %+v", i, got, sent[i])
		}
		switch {
		case sent[i].Error == nil && got.Error != nil:
			t.Errorf("message %d: unexpected error %v", i, got.Error)
		case sent[i].Error != nil && got.Error == nil:
			t.Errorf("message %d: error went missing", i)
		case sent[i].Error != nil && got.Error.Msg != sent[i].Error.Msg:
			t.Errorf("message %d: got error %q", i, got.Error.Msg)
		}
	}

	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

// A payload the codec cannot serialize must fail before anything hits the
// wire, otherwise the stream desyncs and every later message is garbage.
func TestWriteMessageKeepsStreamIntactOnError(t *testing.T) {
	a, b := net.Pipe()
	defer a.Close()
	defer b.Close()

	ca, cb := NewCodec(a), NewCodec(b)

	if err := ca.WriteMessage(&birpc.Message{ID: 1, Func: "X.Y", Args: testmsg.Plain{}}); err == nil {
		t.Fatal("WriteMessage accepted an unserializable payload")
	}

	done := make(chan error, 1)
	go func() {
		done <- ca.WriteMessage(&birpc.Message{ID: 2, Func: "Arith.Add", Args: testmsg.Args{A: 7}})
	}()

	var got birpc.Message
	if err := cb.ReadMessage(&got); err != nil {
		t.Fatalf("stream is desynced: %v", err)
	}
	if got.ID != 2 {
		t.Errorf("got id %d, want the message after the failed write", got.ID)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestReadMessageRejectsGarbage(t *testing.T) {
	a, b := net.Pipe()
	defer a.Close()
	defer b.Close()

	go func() {
		a.Write([]byte{0x81, 0xa2, 0x69, 0x64, 0x01}) // a map, not our tuple
		a.Close()
	}()

	var msg birpc.Message
	if err := NewCodec(b).ReadMessage(&msg); err == nil {
		t.Error("ReadMessage accepted a map encoded message")
	}
}

func TestReadMessageReportsClosedConnection(t *testing.T) {
	a, b := net.Pipe()
	a.Close()
	defer b.Close()

	var msg birpc.Message
	err := NewCodec(b).ReadMessage(&msg)
	if err == nil {
		t.Fatal("ReadMessage on a closed connection returned no error")
	}
	if !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrClosedPipe) {
		t.Errorf("expected a connection error, got %v", err)
	}
}

func TestCloseClosesTheConnection(t *testing.T) {
	a, b := net.Pipe()
	defer b.Close()

	if err := NewCodec(a).Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Write([]byte{0x00}); err == nil {
		t.Error("connection still writable after Close")
	}
}

func TestUnmarshalArgsAndResult(t *testing.T) {
	c := NewCodec(nopConn{})

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

	if err := c.UnmarshalArgs(&birpc.Message{Args: msgp.Raw(raw)}, &testmsg.Plain{}); err == nil {
		t.Error("UnmarshalArgs accepted a non msgp target")
	}
}

// Sanity check that the codec really speaks the tuple format from mpc, not
// some private dialect.
func TestCodecSpeaksTheTupleFormat(t *testing.T) {
	a, b := net.Pipe()
	defer a.Close()
	defer b.Close()

	go func() {
		NewCodec(a).WriteMessage(&birpc.Message{ID: 1, Func: "Arith.Add", Args: testmsg.Args{A: 1}})
	}()

	r := msgp.NewReader(b)
	var m mpc.Message
	if err := m.DecodeMsg(r); err != nil {
		t.Fatal(err)
	}
	if m.ID != 1 || m.Func != "Arith.Add" {
		t.Errorf("got %+v", m)
	}
}

type nopConn struct{}

func (nopConn) Read([]byte) (int, error)    { return 0, io.EOF }
func (nopConn) Write(b []byte) (int, error) { return len(b), nil }
func (nopConn) Close() error                { return nil }
