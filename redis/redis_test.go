package bimpcrds

import (
	"errors"
	"net/rpc"
	"os"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/alicebob/miniredis/v2/server"
	"github.com/sztanpet/bimpc/internal/testmsg"
	"github.com/sztanpet/bimpc/mpc"
	"github.com/tideland/golib/logger"
	"github.com/tideland/golib/redis"
	"github.com/tinylib/msgp/msgp"
	"github.com/tv42/birpc"
)

const (
	timeout = 5 * time.Second

	// the two directions of one endpoint pair
	toServer = "bimpc.test.c2s"
	toClient = "bimpc.test.s2c"
)

func TestMain(m *testing.M) {
	logger.SetLevel(logger.LevelFatal)
	os.Exit(m.Run())
}

// Db is a service asking for the database it was called over, which the codec
// has to hand it through FillArgs
type Db struct {
	mu   sync.Mutex
	seen *redis.Database
}

// Ping reports whether the codec filled in the database
func (d *Db) Ping(args *testmsg.Args, reply *testmsg.Result) error {
	return nil
}

// Which is the FillArgs variant
func (d *Db) Which(args *testmsg.Args, reply *testmsg.Result, db *redis.Database) error {
	d.mu.Lock()
	d.seen = db
	d.mu.Unlock()

	if db == nil {
		return errors.New("codec did not fill in the database")
	}
	reply.S = db.String()
	return nil
}

func (d *Db) db() *redis.Database {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.seen
}

// newServer starts a redis stand-in and opens a database on it
func newServer(t *testing.T, opts ...redis.Option) (*miniredis.Miniredis, *redis.Database) {
	t.Helper()

	s, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis: %v", err)
	}

	opts = append([]redis.Option{redis.TcpConnection(s.Addr(), timeout)}, opts...)
	db, err := redis.Open(opts...)
	if err != nil {
		s.Close()
		t.Fatalf("redis.Open: %v", err)
	}

	t.Cleanup(func() {
		// closing the server first unblocks anything sitting in Pop, the
		// redis client has no way to interrupt a blocked subscription
		s.Close()
		db.Close()
	})
	return s, db
}

// waitSubscribed blocks until every channel has a subscriber, publishing into
// a channel nobody listens on yet is a silent no-op
func waitSubscribed(t *testing.T, s *miniredis.Miniredis, channels ...string) {
	t.Helper()

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		subs := s.PubSubNumSub(channels...)
		ready := true
		for _, ch := range channels {
			if subs[ch] == 0 {
				ready = false
			}
		}
		if ready {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("channels %v never got a subscriber", channels)
}

func registry(svc interface{}) *birpc.Registry {
	if svc == nil {
		return nil
	}
	r := birpc.NewRegistry()
	r.RegisterService(svc)
	return r
}

// newPair puts two endpoints on mirrored channels and serves both
func newPair(t *testing.T, clientSvc, serverSvc interface{}) (client, server *birpc.Endpoint) {
	t.Helper()

	s, db := newServer(t)
	client = NewEndpoint(registry(clientSvc), db, toClient, toServer)
	server = NewEndpoint(registry(serverSvc), db, toServer, toClient)

	go client.Serve()
	go server.Serve()

	waitSubscribed(t, s, toClient, toServer)
	return client, server
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
	client, _ := newPair(t, nil, svc)

	var reply testmsg.Result
	if err := call(t, client, "Arith.Add", testmsg.Args{A: 2, B: 40, S: "hi"}, &reply); err != nil {
		t.Fatal(err)
	}
	if reply.C != 42 || reply.S != "hi" {
		t.Errorf("got %+v", reply)
	}
	if n := svc.Calls.Get(); n != 1 {
		t.Errorf("service called %d times", n)
	}
}

// Used to panic the client: the error reply was written through a nil pointer.
func TestCallReturningError(t *testing.T) {
	client, _ := newPair(t, nil, &testmsg.Arith{})

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
		t.Fatalf("the endpoint did not survive the error: %v", err)
	}
	if reply2.C != 3 {
		t.Errorf("got %+v", reply2)
	}
}

func TestCallUnknownFunction(t *testing.T) {
	client, _ := newPair(t, nil, &testmsg.Arith{})

	var reply testmsg.Result
	err := call(t, client, "Arith.Nope", testmsg.Args{}, &reply)
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "No such function") {
		t.Errorf("got %q", err)
	}
}

func TestBidirectional(t *testing.T) {
	clientSvc := &testmsg.Arith{}
	serverSvc := &testmsg.Arith{}
	_, server := newPair(t, clientSvc, serverSvc)

	var reply testmsg.Result
	if err := call(t, server, "Arith.Add", testmsg.Args{A: 20, B: 22}, &reply); err != nil {
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

func TestFillArgsHandsOverTheDatabase(t *testing.T) {
	svc := &Db{}
	client, _ := newPair(t, nil, svc)

	var reply testmsg.Result
	if err := call(t, client, "Db.Which", testmsg.Args{}, &reply); err != nil {
		t.Fatal(err)
	}
	if reply.S == "" {
		t.Error("no database in the reply")
	}
	if svc.db() == nil {
		t.Error("service was called with a nil database")
	}
}

func TestFillArgsLeavesUnknownTypesAlone(t *testing.T) {
	_, db := newServer(t)
	c := NewCodec(db, toServer, toClient)

	args := []reflect.Value{
		reflect.Zero(reflect.TypeOf((*redis.Database)(nil))),
		reflect.Zero(reflect.TypeOf("")),
	}
	if err := c.FillArgs(args); err != nil {
		t.Fatal(err)
	}
	if args[0].IsNil() {
		t.Error("the database was not filled in")
	}
	if args[1].Interface() != "" {
		t.Error("an unknown argument type was touched")
	}
}

func TestConcurrentCalls(t *testing.T) {
	const workers = 4
	const calls = 10

	svc := &testmsg.Arith{}
	client, _ := newPair(t, nil, svc)

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
					errs <- errors.New("wrong reply, messages got mixed up")
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

// The subscribe confirmation redis sends back carries no payload, handing it
// to the decoder would fail on every fresh subscription.
func TestReadMessageSkipsSubscribeConfirmation(t *testing.T) {
	s, db := newServer(t)
	reader := NewCodec(db, toServer, toClient)
	writer := NewCodec(db, toClient, toServer)

	got := make(chan birpc.Message, 1)
	errs := make(chan error, 1)
	go func() {
		var msg birpc.Message
		if err := reader.ReadMessage(&msg); err != nil {
			errs <- err
			return
		}
		got <- msg
	}()

	waitSubscribed(t, s, toServer)
	if err := writer.WriteMessage(&birpc.Message{ID: 7, Func: "Arith.Add", Args: testmsg.Args{A: 1}}); err != nil {
		t.Fatal(err)
	}

	select {
	case msg := <-got:
		if msg.ID != 7 || msg.Func != "Arith.Add" {
			t.Errorf("got %+v", msg)
		}
	case err := <-errs:
		t.Fatalf("ReadMessage: %v", err)
	case <-time.After(timeout):
		t.Fatal("nothing arrived")
	}
}

func TestCodecRoundTrip(t *testing.T) {
	s, db := newServer(t)
	reader := NewCodec(db, toServer, toClient)
	writer := NewCodec(db, toClient, toServer)

	sent := []birpc.Message{
		{ID: 1, Func: "Arith.Add", Args: testmsg.Args{A: 1, B: 2, S: "x"}},
		{ID: 2, Result: &testmsg.Result{C: 3}},
		{ID: 3, Error: &birpc.Error{Msg: "boom"}},
	}

	got := make(chan birpc.Message, len(sent))
	errs := make(chan error, 1)
	go func() {
		for range sent {
			var msg birpc.Message
			if err := reader.ReadMessage(&msg); err != nil {
				errs <- err
				return
			}
			got <- msg
		}
	}()

	waitSubscribed(t, s, toServer)
	for i := range sent {
		if err := writer.WriteMessage(&sent[i]); err != nil {
			t.Fatal(err)
		}
	}

	for i := range sent {
		select {
		case msg := <-got:
			if msg.ID != sent[i].ID || msg.Func != sent[i].Func {
				t.Errorf("message %d: got %+v want %+v", i, msg, sent[i])
			}
			if (msg.Error == nil) != (sent[i].Error == nil) {
				t.Errorf("message %d: error mismatch %v vs %v", i, msg.Error, sent[i].Error)
			}
		case err := <-errs:
			t.Fatalf("message %d: %v", i, err)
		case <-time.After(timeout):
			t.Fatalf("message %d never arrived", i)
		}
	}
}

func TestReadMessageRejectsGarbage(t *testing.T) {
	s, db := newServer(t)
	c := NewCodec(db, toServer, toClient)

	errs := make(chan error, 1)
	go func() {
		var msg birpc.Message
		errs <- c.ReadMessage(&msg)
	}()

	waitSubscribed(t, s, toServer)
	// a map, which is what the old wire format looked like
	s.Publish(toServer, string([]byte{0x81, 0xa2, 0x69, 0x64, 0x01}))

	select {
	case err := <-errs:
		if err == nil {
			t.Error("ReadMessage accepted a map encoded message")
		}
	case <-time.After(timeout):
		t.Fatal("ReadMessage never returned")
	}
}

func TestWriteMessageRejectsUnserializablePayload(t *testing.T) {
	_, db := newServer(t)
	c := NewCodec(db, toServer, toClient)

	err := c.WriteMessage(&birpc.Message{ID: 1, Func: "X.Y", Args: testmsg.Plain{}})
	if err == nil {
		t.Fatal("WriteMessage accepted an unserializable payload")
	}

	// and the codec still works afterwards
	if err := c.WriteMessage(&birpc.Message{ID: 2, Func: "Arith.Add", Args: testmsg.Args{A: 1}}); err != nil {
		t.Fatalf("codec broken after a failed write: %v", err)
	}
}

// Redis answers a command it refuses with an error reply, which the client
// hands back as an ordinary value, so a plain Do reports success no matter what
// the server said.
func TestWriteMessageReportsARefusedPublish(t *testing.T) {
	s, db := newServer(t)
	c := NewCodec(db, toServer, toClient)

	s.Server().SetPreHook(func(p *server.Peer, cmd string, args ...string) bool {
		if strings.EqualFold(cmd, "publish") {
			p.WriteError("NOPE publishing is broken")
			return true
		}
		return false
	})

	err := c.WriteMessage(&birpc.Message{ID: 1, Func: "Arith.Add", Args: testmsg.Args{A: 1}})
	if err == nil {
		t.Fatal("a refused publish was reported as a successful write")
	}

	s.Server().SetPreHook(nil)
	if err := c.WriteMessage(&birpc.Message{ID: 2, Func: "Arith.Add", Args: testmsg.Args{A: 1}}); err != nil {
		t.Errorf("write after recovery: %v", err)
	}
}

// Every publish borrows a connection and has to give it back, on the failing
// path too, otherwise a flaky redis leaks one connection per attempt.
func TestWriteMessageReturnsConnectionOnFailure(t *testing.T) {
	const writes = 8

	s, db := newServer(t)
	c := NewCodec(db, toServer, toClient)

	s.Server().SetPreHook(func(p *server.Peer, cmd string, args ...string) bool {
		if strings.EqualFold(cmd, "publish") {
			p.WriteError("NOPE publishing is broken")
			return true
		}
		return false
	})

	for i := 0; i < writes; i++ {
		err := c.WriteMessage(&birpc.Message{ID: uint64(i), Func: "Arith.Add", Args: testmsg.Args{A: 1}})
		if err == nil {
			t.Fatalf("write %d: expected the publish to fail", i)
		}
	}

	// one connection, borrowed and returned over and over
	if n := s.Server().TotalConnections(); n > 2 {
		t.Errorf("%d connections opened for %d writes, they are not being returned", n, writes)
	}
}

func TestCloseWithoutSubscription(t *testing.T) {
	_, db := newServer(t)

	if err := NewCodec(db, toServer, toClient).Close(); err != nil {
		t.Errorf("closing a codec that never read: %v", err)
	}
}

func TestCloseUnsubscribes(t *testing.T) {
	s, db := newServer(t)
	c := NewCodec(db, toServer, toClient)

	// drive one message through so the subscription exists
	writer := NewCodec(db, toClient, toServer)
	done := make(chan error, 1)
	go func() {
		var msg birpc.Message
		done <- c.ReadMessage(&msg)
	}()
	waitSubscribed(t, s, toServer)
	if err := writer.WriteMessage(&birpc.Message{ID: 1, Func: "Arith.Add", Args: testmsg.Args{}}); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}

	if err := c.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if n := s.PubSubNumSub(toServer)[toServer]; n != 0 {
		t.Errorf("%d subscribers left on %s", n, toServer)
	}
}

func TestUnmarshalArgsAndResult(t *testing.T) {
	_, db := newServer(t)
	c := NewCodec(db, toServer, toClient)

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

// What goes onto the channel is the same tuple every other codec speaks
func TestCodecSpeaksTheTupleFormat(t *testing.T) {
	s, db := newServer(t)
	c := NewCodec(db, toClient, toServer)

	sub := s.NewSubscriber()
	sub.Subscribe(toServer)
	defer sub.Close()

	// the subscriber channel is unbuffered, publishing blocks until it is read
	published := make(chan miniredis.PubsubMessage, 1)
	go func() {
		published <- <-sub.Messages()
	}()

	if err := c.WriteMessage(&birpc.Message{ID: 1, Func: "Arith.Add", Args: testmsg.Args{A: 1}}); err != nil {
		t.Fatal(err)
	}

	select {
	case pub := <-published:
		var m mpc.Message
		left, err := m.UnmarshalMsg([]byte(pub.Message))
		if err != nil {
			t.Fatalf("published payload is not a wire message: %v", err)
		}
		if len(left) != 0 {
			t.Errorf("%d trailing bytes in the payload", len(left))
		}
		if m.ID != 1 || m.Func != "Arith.Add" {
			t.Errorf("got %+v", m)
		}
	case <-time.After(timeout):
		t.Fatal("nothing was published")
	}
}
