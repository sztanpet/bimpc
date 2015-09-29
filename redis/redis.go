// Package bimpcrds is the redis pub-sub codec for bi-rpc using MessagePack for
// serialization
package bimpcrds

import (
	"reflect"
	"sync"

	"github.com/sztanpet/bimpc/mpc"
	"github.com/tideland/golib/redis"
	"github.com/tinylib/msgp/msgp"
	"github.com/tv42/birpc"
)

type codec struct {
	db *redis.Database
	ch string

	rmu sync.Mutex
	r   *msgp.Reader
	sub *redis.Subscription

	wmu sync.Mutex
	w   *msgp.Writer
	buf []byte
}

func (c *codec) setupSubscription() error {
	if c.sub == nil {
		sub, err := c.db.Subscription()
		if err != nil {
			return err
		}
		c.sub = sub
	}

	err := c.sub.Subscribe(c.ch)
	if err != nil {
		c.sub = nil
		return err
	}
	return nil
}

// ReadMessage listens on the redis pub-sub channel and unmarshals messages
// into a birpc.Message
func (c *codec) ReadMessage(msg *birpc.Message) error {
	c.rmu.Lock()
	defer c.rmu.Unlock()

	for {
		if c.sub == nil {
			err := c.setupSubscription()
			if err != nil {
				return err
			}
		}

		result, err := c.sub.Pop()
		if err != nil {
			c.sub = nil
			return err
		}

		if result.Value.IsNil() {
			continue
		}

		b := result.Value.Bytes()
		m := &mpc.Message{}
		_, err = m.UnmarshalMsg(b)
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
}

// WriteMessage marshals the birpc.Message into messagepack and publishes it
// to the redis pub-sub channel
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

	conn, err := c.db.Connection()
	if err != nil {
		return err
	}

	b, err := m.MarshalMsg(c.buf)
	if err != nil {
		return err
	}
	c.buf = b[:0]

	_, err = conn.Do("PUBLISH", c.ch, b)
	if err != nil {
		return err
	}

	err = conn.Return()
	return err
}

// Close stops the redis subscription
func (c *codec) Close() error {
	if c.sub != nil {
		return c.sub.Close()
	}

	return nil
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
		case *redis.Database:
			arglist[i] = reflect.ValueOf(c.db)
		}
	}
	return nil
}

func NewCodec(db *redis.Database, channel string) *codec {
	c := &codec{
		db: db,
		ch: channel,
		r:  msgp.NewReader(nil),
		w:  msgp.NewWriter(nil),
	}
	return c
}

func NewEndpoint(registry *birpc.Registry, db *redis.Database, channel string) *birpc.Endpoint {
	c := NewCodec(db, channel)
	e := birpc.NewEndpoint(c, registry)
	return e
}
