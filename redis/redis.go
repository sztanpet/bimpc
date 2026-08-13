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

// codec reads and writes on separate channels on purpose: redis delivers a
// published message to every subscriber of that channel, the publisher
// included, so an endpoint listening on the channel it publishes to would
// serve its own requests and choke on its own replies.
type codec struct {
	db  *redis.Database
	in  string
	out string

	rmu sync.Mutex
	r   *msgp.Reader
	sub *redis.Subscription

	wmu sync.Mutex
	w   *msgp.Writer
	buf []byte
}

// setupSubscription is called with rmu held
func (c *codec) setupSubscription() error {
	if c.sub == nil {
		sub, err := c.db.Subscription()
		if err != nil {
			return err
		}
		c.sub = sub
	}

	if err := c.sub.Subscribe(c.in); err != nil {
		c.sub.Close()
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
			msg.Error = &birpc.Error{Msg: m.Error.Msg}
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

	b, err := m.MarshalMsg(c.buf[:0])
	if err != nil {
		return err
	}
	c.buf = b

	conn, err := c.db.Connection()
	if err != nil {
		return err
	}
	defer conn.Return()

	// DoInt and not Do: the client hands an error reply back as an ordinary
	// value with a nil error, and PUBLISH answers with the number of
	// subscribers it reached, so a reply that is not an integer is a refusal
	_, err = conn.DoInt("PUBLISH", c.out, b)
	return err
}

// Close stops the redis subscription
//
// It deliberately does not take rmu: a reader blocked in Pop would hold it
// until a message arrives. The redis client offers no way to interrupt that,
// so tearing down an endpoint whose ReadMessage is blocked means closing the
// database out from under it.
func (c *codec) Close() error {
	if c.sub == nil {
		return nil
	}

	// Subscription.Close only sends punsubscribe, which leaves a plain
	// channel subscription in place, and the connection goes back into the
	// pool still in subscriber mode
	err := c.sub.Unsubscribe(c.in)
	if cerr := c.sub.Close(); err == nil {
		err = cerr
	}
	c.sub = nil

	return err
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

// NewCodec returns a codec listening on the subscribe channel and publishing
// to the publish channel. The two have to differ, see codec.
func NewCodec(db *redis.Database, subscribe, publish string) *codec {
	c := &codec{
		db:  db,
		in:  subscribe,
		out: publish,
		r:   msgp.NewReader(nil),
		w:   msgp.NewWriter(nil),
	}
	return c
}

// NewEndpoint returns a birpc endpoint serving registry over redis pub-sub,
// listening on the subscribe channel and publishing to the publish channel
func NewEndpoint(registry *birpc.Registry, db *redis.Database, subscribe, publish string) *birpc.Endpoint {
	c := NewCodec(db, subscribe, publish)
	e := birpc.NewEndpoint(c, registry)
	return e
}
