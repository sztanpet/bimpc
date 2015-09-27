// Package msgpcodec is a codec for the github.com/tv42/birpc project providing
// messagepack en/decoding
// Users of the package should make sure that
package msgpcodec

//go:generate msgp

import (
	"reflect"

	"github.com/gorilla/websocket"
	"github.com/tinylib/msgp/msgp"
	"github.com/tv42/birpc"
)

//msgp:ignore codec
type codec struct {
	WS *websocket.Conn
}

// Error is equivalent to birpc.Error,
// recreated here for messagepack en/decoding
type Error struct {
	Msg string `msg:"msg"`
}

// MsgPackMessage is the equivalent of birp.Message
// recreated here for messagepack en/decoding
type MsgPackMessage struct {
	ID     uint64   `msg:"id,string,omitempty"`
	Func   string   `msg:"fn,omitempty"`
	Args   msgp.Raw `msg:"args,omitempty"`
	Result msgp.Raw `msg:"result,omitempty"`
	Error  *Error   `msg:"error"`
}

func (c *codec) ReadMessage(msg *birpc.Message) error {
	_, r, err := c.WS.NextReader() // ignoring message type
	if err != nil {
		return err
	}

	m := &MsgPackMessage{}
	err = m.DecodeMsg(msgp.NewReader(r))
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

func (c *codec) WriteMessage(msg *birpc.Message) error {
	m := &MsgPackMessage{}
	m.ID = msg.ID
	m.Func = msg.Func
	m.Args = msg.Args.(msgp.Raw)
	m.Result = msg.Result.(msgp.Raw)
	if msg.Error != nil {
		m.Error = &Error{msg.Error.Msg}
	}

	w, err := c.WS.NextWriter(websocket.BinaryMessage)
	if err != nil {
		return err
	}
	defer w.Close()

	mr := msgp.NewWriter(w)
	return m.EncodeMsg(mr)
}

func (c *codec) Close() error {
	return c.WS.Close()
}

func (c *codec) UnmarshalArgs(msg *birpc.Message, args interface{}) error {
	return nil
}

func (c *codec) UnmarshalResult(msg *birpc.Message, result interface{}) error {
	return nil
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
	}
	return c
}

func NewEndpoint(registry *birpc.Registry, ws *websocket.Conn) *birpc.Endpoint {
	c := NewCodec(ws)
	e := birpc.NewEndpoint(c, registry)
	return e
}
