// Package mpc contains the types sent through the wire, and the conversion
// to and from birpc's own message type.
//
// Both the entire message and error are transmitted as a tuple for speed,
// interoperating services beware.
package mpc

import (
	"fmt"
	"sync"

	"github.com/tinylib/msgp/msgp"
	"github.com/tv42/birpc"
)

//go:generate msgp -unexported
//msgp:tuple Message Error

// Error is equivalent to birpc.Error,
// recreated here for MessagePack en/decoding
type Error struct {
	Msg string `msg:"msg"`
}

// Message is the equivalent of birpc.Message
// recreated here for MessagePack en/decoding
type Message struct {
	ID     uint64   `msg:"id"`
	Func   string   `msg:"fn"`
	Args   msgp.Raw `msg:"args"`
	Result msgp.Raw `msg:"result"`
	Error  *Error   `msg:"error"`
}

// messages hands out wire messages that keep their payload buffers between
// uses, so writing one does not allocate
var messages = sync.Pool{
	New: func() interface{} { return &Message{} },
}

// a message that had to grow a large buffer once should not hold on to it for
// the lifetime of the process
const maxPooledBuffer = 64 << 10

// GetMessage returns a wire message to be filled in by ToWire. Hand it back
// with PutMessage once it has been written out.
func GetMessage() *Message {
	return messages.Get().(*Message)
}

// PutMessage returns a wire message for reuse. The caller must be done with
// it: whatever wrote it out has to have copied the payloads by now.
func PutMessage(m *Message) {
	if cap(m.Args) > maxPooledBuffer {
		m.Args = nil
	}
	if cap(m.Result) > maxPooledBuffer {
		m.Result = nil
	}
	m.Error = nil

	messages.Put(m)
}

// ToWire fills m from msg, marshaling the arguments and the result into their
// raw MessagePack representation.
//
// Args and Result MUST implement the msgp.Marshaler interface when they are
// set; a value that does not is an error rather than a silently dropped field,
// as the peer would otherwise see a well-formed message with no payload.
//
// The payloads are marshaled into whatever m already has, so a message that
// comes back around is free of allocations. An absent payload is left as an
// empty slice rather than a nil one, which is the same nil on the wire.
func ToWire(m *Message, msg *birpc.Message) error {
	m.ID = msg.ID
	m.Func = msg.Func
	m.Error = nil

	var err error
	if m.Args, err = marshal(m.Args, msg.Args); err != nil {
		return fmt.Errorf("marshaling args: %v", err)
	}
	if m.Result, err = marshal(m.Result, msg.Result); err != nil {
		return fmt.Errorf("marshaling result: %v", err)
	}

	if msg.Error != nil {
		m.Error = &Error{Msg: msg.Error.Msg}
	}

	return nil
}

// marshal appends the MessagePack form of v to buf, reusing its storage
func marshal(buf msgp.Raw, v interface{}) (msgp.Raw, error) {
	buf = buf[:0]
	if v == nil {
		return buf, nil
	}

	t, ok := v.(msgp.Marshaler)
	if !ok {
		return buf, fmt.Errorf("%T does not implement the msgp.Marshaler interface", v)
	}

	b, err := t.MarshalMsg(buf)
	if err != nil {
		return buf, err
	}
	return msgp.Raw(b), nil
}

// FromWire fills msg from a decoded wire message. The raw payloads are handed
// over as-is, to be decoded later by UnmarshalArgs/UnmarshalResult once the
// concrete types are known.
func FromWire(msg *birpc.Message, m *Message) {
	msg.ID = m.ID
	msg.Func = m.Func
	msg.Args = m.Args
	msg.Result = m.Result
	msg.Error = nil
	if m.Error != nil {
		msg.Error = &birpc.Error{Msg: m.Error.Msg}
	}
}

// Unmarshal is a helper function used in all the other packages, it
// unmarshals msgp.Raw messages into types, the type (argument ret) MUST
// implement the msgp.Unmarshaler interface
//
// An empty payload means the peer sent nil, ret is then left untouched.
func Unmarshal(i interface{}, ret interface{}) error {
	t, ok := ret.(msgp.Unmarshaler)
	if !ok {
		return fmt.Errorf("%T does not implement the msgp.Unmarshaler interface", ret)
	}

	if i == nil {
		return nil
	}

	raw, ok := i.(msgp.Raw)
	if !ok {
		return fmt.Errorf("%T is not a raw MessagePack payload", i)
	}
	if len(raw) == 0 {
		return nil
	}

	_, err := t.UnmarshalMsg([]byte(raw))
	return err
}
