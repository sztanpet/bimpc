// Package mpc contains the type sent thrugh the write
//
// Both the entire message and error are transmitted as a tupple for speed
// interoperating services beware
package mpc

import (
	"fmt"

	"github.com/tinylib/msgp/msgp"
)

//go:generate msgp -unexported
//msgp:tuple MessageError

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
