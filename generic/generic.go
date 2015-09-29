// Package bimpcgen is the generic codec for bi-rpc using MessagePack for
// serialization
package bimpcgen

import (
	"io"
	"sync"

	"github.com/sztanpet/bimpc/mpc"
	"github.com/tinylib/msgp/msgp"
	"github.com/tv42/birpc"
)

type codec struct {
	conn io.ReadWriteCloser
	r    *msgp.Reader

	wmu sync.Mutex
	w   *msgp.Writer
}

// ReadMessage reads from the connection and unmarshals the message
// into a birpc.Message
func (c *codec) ReadMessage(msg *birpc.Message) error {
	m := &mpc.Message{}
	err := m.DecodeMsg(c.r)
	if err != nil {
		return err
	}

	msg.ID = m.ID
	msg.Func = m.Func
	msg.Args = m.Args
	msg.Result = m.Result
	if m.Error != nil {
		*msg.Error = birpc.Error{Msg: m.Error.Msg}
	}

	return nil
}

// WriteMessage marshals the birpc.Message into MessagePack and writes it out
func (c *codec) WriteMessage(msg *birpc.Message) error {
	c.wmu.Lock()
	defer c.wmu.Unlock()

	m := &mpc.Message{}
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
		m.Error = &mpc.Error{Msg: msg.Error.Msg}
	}

	if err := m.EncodeMsg(c.w); err != nil {
		return err
	}
	if err := c.w.Flush(); err != nil {
		return err
	}

	return nil
}

// Close stops the redis subscription
func (c *codec) Close() error {
	return c.conn.Close()
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

func NewCodec(conn io.ReadWriteCloser) *codec {
	c := &codec{
		conn: conn,
		r:    msgp.NewReader(conn),
		w:    msgp.NewWriter(conn),
	}
	return c
}

func NewEndpoint(registry *birpc.Registry, conn io.ReadWriteCloser) *birpc.Endpoint {
	c := NewCodec(conn)
	e := birpc.NewEndpoint(c, registry)
	return e
}
