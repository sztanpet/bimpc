// Package mpc contains the types sent through the wire, and the conversion
// to and from birpc's own message type.
//
// Both the entire message and error are transmitted as a tuple for speed,
// interoperating services beware.
package mpc

import (
	"fmt"

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

// ToWire fills m from msg, marshaling the arguments and the result into their
// raw MessagePack representation.
//
// Args and Result MUST implement the msgp.Marshaler interface when they are
// set; a value that does not is an error rather than a silently dropped field,
// as the peer would otherwise see a well-formed message with no payload.
func ToWire(m *Message, msg *birpc.Message) error {
	m.ID = msg.ID
	m.Func = msg.Func
	m.Args = nil
	m.Result = nil
	m.Error = nil

	var err error
	if m.Args, err = marshal(msg.Args); err != nil {
		return fmt.Errorf("marshaling args: %v", err)
	}
	if m.Result, err = marshal(msg.Result); err != nil {
		return fmt.Errorf("marshaling result: %v", err)
	}

	if msg.Error != nil {
		m.Error = &Error{Msg: msg.Error.Msg}
	}

	return nil
}

func marshal(v interface{}) (msgp.Raw, error) {
	if v == nil {
		return nil, nil
	}

	t, ok := v.(msgp.Marshaler)
	if !ok {
		return nil, fmt.Errorf("%T does not implement the msgp.Marshaler interface", v)
	}

	b, err := t.MarshalMsg(nil)
	if err != nil {
		return nil, err
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
func Unmarshal(i interface{}, ret interface{}) error {
	t, ok := ret.(msgp.Unmarshaler)
	if !ok {
		return fmt.Errorf("%T does not implement the msgp.Unmarshaler interface", ret)
	}

	_, err := t.UnmarshalMsg([]byte(i.(msgp.Raw)))
	return err
}
