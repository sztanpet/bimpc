package main

import (
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	bimpcws "github.com/sztanpet/bimpc/websocket"
	"github.com/tinylib/msgp/msgp"
	"github.com/tv42/birpc"
)

const timeout = 5 * time.Second

func init() {
	// the handlers log every message, which is noise in a test run
	log.SetOutput(io.Discard)
}

// newServer starts the chat over http and returns its websocket url
func newServer(t *testing.T) string {
	t.Helper()

	chat := newChat()
	mux := http.NewServeMux()
	mux.HandleFunc("/sock", chat.serve)
	mux.HandleFunc("/", index)

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	return "ws" + strings.TrimPrefix(srv.URL, "http")
}

// client is a chat participant collecting everything broadcast at it
type client struct {
	ws       *websocket.Conn
	endpoint *birpc.Endpoint
	out      chan Outgoing
}

// dial connects a client running the very same chat service, so incoming
// Chat.Message calls land on its own topic
func dial(t *testing.T, url string) *client {
	t.Helper()

	ws := raw(t, url)

	chat := newChat()
	received := make(chan interface{}, 10)
	chat.broadcast.Register(received)

	c := &client{
		ws:       ws,
		endpoint: bimpcws.NewEndpoint(chat.registry, ws),
		out:      make(chan Outgoing, 10),
	}
	go c.endpoint.Serve()
	go func() {
		defer close(c.out)
		for i := range received {
			c.out <- i.(Outgoing)
		}
	}()

	return c
}

// raw dials without speaking birpc on top
func raw(t *testing.T, url string) *websocket.Conn {
	t.Helper()

	ws, _, err := websocket.DefaultDialer.Dial(url+"/sock", nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { ws.Close() })
	return ws
}

func (c *client) recv(t *testing.T) Outgoing {
	t.Helper()

	select {
	case msg, ok := <-c.out:
		if !ok {
			t.Fatal("the client stopped receiving")
		}
		return msg
	case <-time.After(timeout):
		t.Fatal("no message arrived")
		return Outgoing{}
	}
}

func TestIndexServesTheChatPage(t *testing.T) {
	url := newServer(t)

	res, err := http.Get("http" + strings.TrimPrefix(url, "ws") + "/")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		t.Errorf("got status %d", res.StatusCode)
	}
	body, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	// both other assets are inlined into the page by the template
	for _, want := range []string{"<html", "msgform", "new WebSocket", "#msg {"} {
		if !strings.Contains(string(body), want) {
			t.Errorf("the page is missing %q", want)
		}
	}
}

func TestServerGreetsEveryClient(t *testing.T) {
	c := dial(t, newServer(t))

	msg := c.recv(t)
	if msg.From != "Server" || msg.Message != "HELLO FROM SERVER" {
		t.Errorf("got %+v", msg)
	}
}

// What the example is for: a message from one browser shows up in the other.
func TestMessagesReachEveryClient(t *testing.T) {
	url := newServer(t)

	alice := dial(t, url)
	bob := dial(t, url)

	// both are greeted first
	alice.recv(t)
	bob.recv(t)

	if err := alice.endpoint.Call("Chat.Message", Incoming{From: "alice", Message: "hello"}, nil); err != nil {
		t.Fatal(err)
	}

	for name, c := range map[string]*client{"sender": alice, "other": bob} {
		msg := c.recv(t)
		if msg.From != "alice" || msg.Message != "hello" {
			t.Errorf("%s got %+v", name, msg)
		}
		if msg.Time == 0 {
			t.Errorf("%s got no timestamp", name)
		}
	}
}

// chat.js reads the message as a five element array and the args as a map of
// time/from/message. Decode a raw frame the way the browser would.
func TestFramesMatchWhatTheJavascriptClientExpects(t *testing.T) {
	ws := raw(t, newServer(t))

	mt, frame, err := ws.ReadMessage()
	if err != nil {
		t.Fatal(err)
	}
	if mt != websocket.BinaryMessage {
		t.Fatalf("the greeting is not a binary frame but type %d", mt)
	}

	// [id, fn, args, result, error]
	n, rest, err := msgp.ReadArrayHeaderBytes(frame)
	if err != nil {
		t.Fatalf("the frame is not an array: %v", err)
	}
	if n != 5 {
		t.Fatalf("the frame has %d elements, chat.js reads 5", n)
	}

	if _, rest, err = msgp.ReadUint64Bytes(rest); err != nil {
		t.Fatalf("id: %v", err)
	}
	fn, rest, err := msgp.ReadStringBytes(rest)
	if err != nil {
		t.Fatalf("fn: %v", err)
	}
	if fn != "Chat.Message" {
		t.Errorf("fn is %q", fn)
	}

	args, rest, err := msgp.ReadMapStrIntfBytes(rest, nil)
	if err != nil {
		t.Fatalf("args: %v", err)
	}
	for _, key := range []string{"time", "from", "message"} {
		if _, ok := args[key]; !ok {
			t.Errorf("args has no %q, chat.js reads rpc.args.%s", key, key)
		}
	}
	if args["from"] != "Server" {
		t.Errorf("args.from is %v", args["from"])
	}

	// result and error are both empty on a notification
	if !msgp.IsNil(rest) {
		t.Error("result is set on a notification")
	}
	if rest, err = msgp.ReadNilBytes(rest); err != nil {
		t.Fatal(err)
	}
	if !msgp.IsNil(rest) {
		t.Error("error is set on a notification")
	}
}

// The other direction: exactly the bytes chat.js sends have to reach the
// service as an Incoming
func TestServerAcceptsAHandWrittenFrame(t *testing.T) {
	url := newServer(t)
	listener := dial(t, url)
	listener.recv(t) // the greeting

	ws := raw(t, url)

	// [id, fn, {from, message}, result, error]
	var frame []byte
	frame = msgp.AppendArrayHeader(frame, 5)
	frame = msgp.AppendUint64(frame, 0)
	frame = msgp.AppendString(frame, "Chat.Message")
	frame = msgp.AppendMapHeader(frame, 2)
	frame = msgp.AppendString(frame, "from")
	frame = msgp.AppendString(frame, "bob")
	frame = msgp.AppendString(frame, "message")
	frame = msgp.AppendString(frame, "typed by hand")
	frame = msgp.AppendString(frame, "")
	frame = msgp.AppendNil(frame)

	if err := ws.WriteMessage(websocket.BinaryMessage, frame); err != nil {
		t.Fatal(err)
	}

	msg := listener.recv(t)
	if msg.From != "bob" || msg.Message != "typed by hand" {
		t.Errorf("got %+v", msg)
	}
}

// A disconnect has to take the broadcast pump with it, a leaked one keeps
// writing into a dead socket for the lifetime of the process
func TestDisconnectingStopsTheBroadcastPump(t *testing.T) {
	const clients = 10

	url := newServer(t)

	settle()
	before := runtime.NumGoroutine()

	for i := 0; i < clients; i++ {
		ws := raw(t, url)
		if _, _, err := ws.ReadMessage(); err != nil { // the greeting
			t.Fatal(err)
		}
		ws.Close()
	}

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if runtime.NumGoroutine() <= before+clients/2 {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Errorf("goroutines went from %d to %d for %d closed connections",
		before, runtime.NumGoroutine(), clients)
}

func settle() {
	for i := 0; i < 10; i++ {
		runtime.Gosched()
		time.Sleep(10 * time.Millisecond)
	}
}

func TestUpgradeFailsForAPlainRequest(t *testing.T) {
	url := newServer(t)

	res, err := http.Get("http" + strings.TrimPrefix(url, "ws") + "/sock")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()

	if res.StatusCode == http.StatusSwitchingProtocols {
		dump, _ := httputil.DumpResponse(res, false)
		t.Errorf("a plain GET was upgraded:\n%s", dump)
	}
}
