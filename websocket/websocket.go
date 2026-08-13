// Package bimpcws is the websocket codec for bi-rpc using MessagePack for
// serialization
package bimpcws

import (
	"errors"
	"reflect"
	"sync"

	"github.com/gorilla/websocket"
	"github.com/sztanpet/bimpc/mpc"
	"github.com/tinylib/msgp/msgp"
	"github.com/tv42/birpc"
)

// ErrInvalidMsg is the error returned when we receive a non-binary message
// from the client, this usually signals that the client either does not
// support binary messages, or simply that the other end is not a birpc endpoint
var ErrInvalidMsg = errors.New("Websocket message was not a binary message")

type codec struct {
	ws *websocket.Conn

	rmu sync.Mutex
	r   *msgp.Reader

	wmu sync.Mutex
	w   *msgp.Writer
}

// ReadMessage reads from the websocket and unmarshals it into a birpc.Message
func (c *codec) ReadMessage(msg *birpc.Message) error {
	c.rmu.Lock()
	defer c.rmu.Unlock()

	mt, r, err := c.ws.NextReader()
	if err != nil {
		return err
	}
	if mt != websocket.BinaryMessage {
		return ErrInvalidMsg
	}

	c.r.Reset(r)

	m := &mpc.Message{}
	if err := m.DecodeMsg(c.r); err != nil {
		return err
	}

	mpc.FromWire(msg, m)
	return nil
}

// WriteMessage marshals the birpc.Message into messagepack and writes it out
// to the websocket connection
func (c *codec) WriteMessage(msg *birpc.Message) error {
	m := &mpc.Message{}
	if err := mpc.ToWire(m, msg); err != nil {
		return err
	}

	c.wmu.Lock()
	defer c.wmu.Unlock()

	w, err := c.ws.NextWriter(websocket.BinaryMessage)
	if err != nil {
		return err
	}

	// replace the writer, encode the message, flush the buffer to the writer
	// buffer, close the writer thus flushing its buffer to the wire finally
	c.w.Reset(w)
	if err = m.EncodeMsg(c.w); err != nil {
		w.Close()
		return err
	}
	if err = c.w.Flush(); err != nil {
		w.Close()
		return err
	}

	// the frame is only complete once the writer is closed, so its error
	// decides whether the message made it out
	return w.Close()
}

// Close closes the websocket connection
func (c *codec) Close() error {
	return c.ws.Close()
}

// UnmarshalArgs unmarshals the arguments into the type as registered by
// birpc.Register, the type MUST implement the msgp.Unmarshaler interface
func (c *codec) UnmarshalArgs(msg *birpc.Message, args interface{}) error {
	return mpc.Unmarshal(msg.Args, args)
}

// UnmarshalResult unmarshals the result into the type as registered by
// birpc.Register, the type MUST implement the msgp.Unmarshaler interface
func (c *codec) UnmarshalResult(msg *birpc.Message, result interface{}) error {
	return mpc.Unmarshal(msg.Result, result)
}

func (c *codec) FillArgs(arglist []reflect.Value) error {
	for i := 0; i < len(arglist); i++ {
		switch arglist[i].Interface().(type) {
		case *websocket.Conn:
			arglist[i] = reflect.ValueOf(c.ws)
		}
	}
	return nil
}

func NewCodec(ws *websocket.Conn) *codec {
	c := &codec{
		ws: ws,
		r:  msgp.NewReader(nil),
		w:  msgp.NewWriter(nil),
	}
	return c
}

func NewEndpoint(registry *birpc.Registry, ws *websocket.Conn) *birpc.Endpoint {
	c := NewCodec(ws)
	e := birpc.NewEndpoint(c, registry)
	return e
}
