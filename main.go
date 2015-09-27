// Package msgpcodec is a codec for the github.com/tv42/birpc project providing
// messagepack en/decoding
//
// Both the entire message and error are transmitted as a tupple for speed
// interoperating services beware
package msgpcodec

//go:generate msgp -unexported
//msgp:tuple msgPackMessage msgPackError

import (
	"errors"
	"fmt"
	"reflect"
	"sync"

	"github.com/gorilla/websocket"
	"github.com/tinylib/msgp/msgp"
	"github.com/tv42/birpc"
)

var (
	InvalidMessage = errors.New("Websocket message was not a binary message")
)

//msgp:ignore codec
type codec struct {
	WS *websocket.Conn

	rmu sync.Mutex
	r   *msgp.Reader

	wmu sync.Mutex
	w   *msgp.Writer
}

// msgPackError is equivalent to birpc.Error,
// recreated here for messagepack en/decoding
type msgPackError struct {
	Msg string `msg:"msg"`
}

// msgPackMessage is the equivalent of birpc.Message
// recreated here for messagepack en/decoding
type msgPackMessage struct {
	ID     uint64        `msg:"id"`
	Func   string        `msg:"fn"`
	Args   msgp.Raw      `msg:"args"`
	Result msgp.Raw      `msg:"result"`
	Error  *msgPackError `msg:"error"`
}

// ReadMessage reads from the websocket and unmarshals it into a birpc.Message
func (c *codec) ReadMessage(msg *birpc.Message) error {
	c.rmu.Lock()
	defer c.rmu.Unlock()

	mt, r, err := c.WS.NextReader() // ignoring message type
	if mt != websocket.BinaryMessage {
		return InvalidMessage
	}

	if err != nil {
		return err
	}
	c.r.Reset(r)

	m := &msgPackMessage{}
	err = m.DecodeMsg(c.r)
	if err != nil {
		return err
	}

	msg.ID = m.ID
	msg.Func = m.Func
	msg.Args = m.Args
	msg.Result = m.Result
	if m.Error != nil {
		*msg.Error = birpc.Error{m.Error.Msg}
	}

	return nil
}

// WriteMessage marshals the birpc.Message into messagepack and writes it out
// to the websocket connection
func (c *codec) WriteMessage(msg *birpc.Message) error {
	c.wmu.Lock()
	defer c.wmu.Unlock()

	m := &msgPackMessage{}
	m.ID = msg.ID
	m.Func = msg.Func

	if t, ok := msg.Args.(msgp.Marshaler); ok {
		b, err := t.MarshalMsg(nil)
		if err != nil {
			return err
		}
		m.Args = msgp.Raw(b)
	}

	if t, ok := msg.Result.(msgp.Marshaler); ok {
		b, err := t.MarshalMsg(nil)
		if err != nil {
			return err
		}
		m.Result = msgp.Raw(b)
	}

	if msg.Error != nil {
		m.Error = &msgPackError{msg.Error.Msg}
	}

	w, err := c.WS.NextWriter(websocket.BinaryMessage)
	if err != nil {
		return err
	}

	// replace the writer, encode the message, flush the buffer to the writer
	// buffer, close the writer thus flushing its buffer to the wire finally
	defer w.Close()
	c.w.Reset(w)
	if err = m.EncodeMsg(c.w); err != nil {
		return err
	}
	if err = c.w.Flush(); err != nil {
		return err
	}

	return nil
}

// Close closes the websocket connection
func (c *codec) Close() error {
	return c.WS.Close()
}

// UnmarshalArgs unmarshals the arguments into the type as registered by
// birpc.Register, the type MUST implement the msgp.Unmarshaler interface
func (c *codec) UnmarshalArgs(msg *birpc.Message, args interface{}) error {
	return unmarshal(msg.Args, args)
}

// UnmarshalResult unmarshals the result into the type as registered by
// birpc.Register, the type MUST implement the msgp.Unmarshaler interface
func (c *codec) UnmarshalResult(msg *birpc.Message, result interface{}) error {
	return unmarshal(msg.Result, result)
}

func unmarshal(i interface{}, ret interface{}) error {
	t, ok := ret.(msgp.Unmarshaler)
	if !ok {
		return fmt.Errorf("%T does not implement the msgp.Unmarshaler interface")
	}

	_, err := t.UnmarshalMsg([]byte(i.(msgp.Raw)))
	return err
}

func (c *codec) FillArgs(arglist []reflect.Value) error {
	for i := 0; i < len(arglist); i++ {
		switch arglist[i].Interface().(type) {
		case *websocket.Conn:
			arglist[i] = reflect.ValueOf(c.WS)
		}
	}
	return nil
}

func NewCodec(ws *websocket.Conn) *codec {
	c := &codec{
		WS: ws,
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
