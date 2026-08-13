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

// Codec implements birpc.Codec on top of any io.ReadWriteCloser
type Codec struct {
	conn io.ReadWriteCloser

	rmu  sync.Mutex
	r    *msgp.Reader
	wire mpc.Message

	wmu sync.Mutex
	w   *msgp.Writer
}

// ReadMessage reads from the connection and unmarshals the message
// into a birpc.Message
func (c *Codec) ReadMessage(msg *birpc.Message) error {
	c.rmu.Lock()
	defer c.rmu.Unlock()

	if err := c.wire.DecodeMsg(c.r); err != nil {
		return err
	}

	mpc.FromWire(msg, &c.wire)
	return nil
}

// WriteMessage marshals the birpc.Message into MessagePack and writes it out
func (c *Codec) WriteMessage(msg *birpc.Message) error {
	m := mpc.GetMessage()
	defer mpc.PutMessage(m)

	if err := mpc.ToWire(m, msg); err != nil {
		return err
	}

	c.wmu.Lock()
	defer c.wmu.Unlock()

	if err := m.EncodeMsg(c.w); err != nil {
		return err
	}

	return c.w.Flush()
}

// Close closes the underlying connection
func (c *Codec) Close() error {
	return c.conn.Close()
}

// UnmarshalArgs unmarshals the arguments into the type as registered by
// birpc.Register, the type MUST implement the msgp.Unmarshaler interface
func (c *Codec) UnmarshalArgs(msg *birpc.Message, args interface{}) error {
	return mpc.Unmarshal(msg.Args, args)
}

// UnmarshalResult unmarshals the result into the type as registered by
// birpc.Register, the type MUST implement the msgp.Unmarshaler interface
func (c *Codec) UnmarshalResult(msg *birpc.Message, result interface{}) error {
	return mpc.Unmarshal(msg.Result, result)
}

// NewCodec returns a codec talking MessagePack over conn
func NewCodec(conn io.ReadWriteCloser) *Codec {
	return &Codec{
		conn: conn,
		r:    msgp.NewReader(conn),
		w:    msgp.NewWriter(conn),
	}
}

// NewEndpoint returns a birpc endpoint serving registry over conn
func NewEndpoint(registry *birpc.Registry, conn io.ReadWriteCloser) *birpc.Endpoint {
	return birpc.NewEndpoint(NewCodec(conn), registry)
}
