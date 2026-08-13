package main

import (
	"flag"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"os"
	"path"
	"time"

	"github.com/gorilla/websocket"
	bimpcws "github.com/sztanpet/bimpc/websocket"
	"github.com/tv42/birpc"
	"github.com/tv42/topic"
)

// -unexported so the empty `nothing` reply gets a marshaler too
//go:generate msgp -unexported

var (
	host = flag.String("host", "", "IP address to bind to")
	port = flag.Int("port", 8000, "TCP port to listen on")
)

var html = template.New("main")

func init() {
	template.Must(html.New("chat.html").Parse(chatHTML))
	template.Must(html.New("chat.css").Parse(chatCSS))
	template.Must(html.New("chat.js").Parse(chatJS))
}

func Usage() {
	fmt.Fprintf(os.Stderr, "Usage of %s:\n", os.Args[0])
	flag.PrintDefaults()
}

type Incoming struct {
	From    string `msg:"from"`
	Message string `msg:"message"`
}

type Outgoing struct {
	Time    uint32 `msg:"time"`
	From    string `msg:"from"`
	Message string `msg:"message"`
}

func index(w http.ResponseWriter, req *http.Request) {
	w.Header().Set("Content-Type", "text/html")
	w.WriteHeader(http.StatusOK)
	err := html.ExecuteTemplate(w, "chat.html", nil)
	if err != nil {
		log.Printf("Template error: %v", err)
	}
}

//msgp:ignore Chat
type Chat struct {
	broadcast *topic.Topic
	registry  *birpc.Registry
	upgrader  websocket.Upgrader
}

type nothing struct{}

// newChat registers the chat service with itself, so both ends of a
// connection can call Chat.Message on each other
func newChat() *Chat {
	c := &Chat{
		broadcast: topic.New(),
		registry:  birpc.NewRegistry(),
	}
	c.registry.RegisterService(c)
	return c
}

func (c *Chat) Message(msg *Incoming, _ *nothing, ws *websocket.Conn) error {
	log.Printf("recv from %v:%#v\n", ws.RemoteAddr(), msg)

	c.broadcast.Broadcast <- Outgoing{
		Time:    uint32(time.Now().Unix()),
		From:    msg.From,
		Message: msg.Message,
	}
	return nil
}

// serve upgrades the connection and pumps broadcasts at it until either side
// gives up
func (c *Chat) serve(w http.ResponseWriter, req *http.Request) {
	ws, err := c.upgrader.Upgrade(w, req, nil)
	if err != nil {
		log.Println(err)
		return
	}

	endpoint := bimpcws.NewEndpoint(c.registry, ws)
	messages := make(chan interface{}, 10)
	c.broadcast.Register(messages)

	// unregistering closes the channel, which is what stops the pump below,
	// otherwise every disconnect leaves a goroutine broadcasting into a dead
	// websocket
	defer c.broadcast.Unregister(messages)

	_ = endpoint.Go("Chat.Message", Outgoing{
		Time:    uint32(time.Now().Unix()),
		From:    "Server",
		Message: "HELLO FROM SERVER",
	}, nil, nil)

	go func() {
		defer c.broadcast.Unregister(messages)
		for i := range messages {
			msg := i.(Outgoing)
			// Fire-and-forget.
			// TODO use .Notify when it exists
			_ = endpoint.Go("Chat.Message", msg, nil, nil)
		}
		// broadcast topic kicked us out for being too slow;
		// probably a hung TCP connection. let client
		// re-establish.
		log.Printf("Kicking slow client: %v", ws.RemoteAddr())
		ws.Close()
	}()

	if err := endpoint.Serve(); err != nil {
		log.Printf("websocket error from %v: %v", ws.RemoteAddr(), err)
	}
}

func main() {
	prog := path.Base(os.Args[0])
	log.SetFlags(0)
	log.SetPrefix(prog + ": ")

	flag.Usage = Usage
	flag.Parse()

	if flag.NArg() > 0 {
		Usage()
		os.Exit(1)
	}

	log.Printf("Serving at http://%s:%d/", *host, *port)

	chat := newChat()
	defer close(chat.broadcast.Broadcast)

	http.HandleFunc("/sock", chat.serve)
	http.Handle("/", http.HandlerFunc(index))
	addr := fmt.Sprintf("%s:%d", *host, *port)
	err := http.ListenAndServe(addr, nil)
	if err != nil {
		log.Fatal(err)
	}
}
