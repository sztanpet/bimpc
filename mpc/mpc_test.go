package mpc

import (
	"bytes"
	"strings"
	"testing"

	"github.com/sztanpet/bimpc/internal/testmsg"
	"github.com/tinylib/msgp/msgp"
	"github.com/tv42/birpc"
)

// The javascript client in examples/chat reads the message as a five element
// array and the error as a one element array. Encoding these as maps instead
// silently breaks every non-Go peer, so pin the bytes down.
func TestWireFormatIsATuple(t *testing.T) {
	tests := []struct {
		name string
		msg  Message
		want []byte
	}{
		{
			name: "empty message",
			msg:  Message{},
			want: []byte{
				0x95,             // fixarray, 5 elements
				0x00,             // id: 0
				0xa0,             // fn: ""
				0xc0, 0xc0, 0xc0, // args, result, error: nil
			},
		},
		{
			name: "request",
			msg:  Message{ID: 300, Func: "Arith.Add", Args: msgp.Raw{0x01}},
			want: []byte{
				0x95,
				0xcd, 0x01, 0x2c, // id: uint16 300
				0xa9, 'A', 'r', 'i', 't', 'h', '.', 'A', 'd', 'd',
				0x01,       // args: raw, verbatim
				0xc0, 0xc0, // result, error: nil
			},
		},
		{
			name: "error response",
			msg:  Message{ID: 1, Error: &Error{Msg: "boom"}},
			want: []byte{
				0x95,
				0x01,
				0xa0,
				0xc0, 0xc0,
				0x91,                     // error: fixarray, 1 element
				0xa4, 'b', 'o', 'o', 'm', // msg
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.msg.MarshalMsg(nil)
			if err != nil {
				t.Fatalf("MarshalMsg: %v", err)
			}
			if !bytes.Equal(got, tt.want) {
				t.Errorf("MarshalMsg\n got % x\nwant % x", got, tt.want)
			}

			// the streaming encoder has to agree with the byte one
			var buf bytes.Buffer
			w := msgp.NewWriter(&buf)
			if err := tt.msg.EncodeMsg(w); err != nil {
				t.Fatalf("EncodeMsg: %v", err)
			}
			if err := w.Flush(); err != nil {
				t.Fatalf("Flush: %v", err)
			}
			if !bytes.Equal(buf.Bytes(), tt.want) {
				t.Errorf("EncodeMsg\n got % x\nwant % x", buf.Bytes(), tt.want)
			}
		})
	}
}

func TestMessageRoundTrip(t *testing.T) {
	args, err := testmsg.Args{A: 3, B: 4, S: "hello"}.MarshalMsg(nil)
	if err != nil {
		t.Fatal(err)
	}

	orig := Message{
		ID:     42,
		Func:   "Arith.Add",
		Args:   msgp.Raw(args),
		Result: msgp.Raw{0xc3}, // true
		Error:  &Error{Msg: "still here"},
	}

	t.Run("bytes", func(t *testing.T) {
		b, err := orig.MarshalMsg(nil)
		if err != nil {
			t.Fatal(err)
		}

		var got Message
		left, err := got.UnmarshalMsg(b)
		if err != nil {
			t.Fatal(err)
		}
		if len(left) != 0 {
			t.Errorf("%d bytes left over", len(left))
		}
		assertMessageEqual(t, &got, &orig)
	})

	t.Run("stream", func(t *testing.T) {
		var buf bytes.Buffer
		w := msgp.NewWriter(&buf)
		if err := orig.EncodeMsg(w); err != nil {
			t.Fatal(err)
		}
		if err := w.Flush(); err != nil {
			t.Fatal(err)
		}

		var got Message
		if err := got.DecodeMsg(msgp.NewReader(&buf)); err != nil {
			t.Fatal(err)
		}
		assertMessageEqual(t, &got, &orig)
	})
}

// Several messages have to be decodable back to back from one stream, that is
// what every connection oriented codec relies on.
func TestMessageStreamsBackToBack(t *testing.T) {
	var buf bytes.Buffer
	w := msgp.NewWriter(&buf)
	for i := uint64(1); i <= 10; i++ {
		m := Message{ID: i, Func: "Arith.Add", Args: msgp.Raw{0x01}}
		if err := m.EncodeMsg(w); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Flush(); err != nil {
		t.Fatal(err)
	}

	r := msgp.NewReader(&buf)
	for i := uint64(1); i <= 10; i++ {
		var m Message
		if err := m.DecodeMsg(r); err != nil {
			t.Fatalf("message %d: %v", i, err)
		}
		if m.ID != i {
			t.Errorf("message %d: got id %d", i, m.ID)
		}
	}
}

func TestMessageDecodeRejectsGarbage(t *testing.T) {
	tests := []struct {
		name string
		in   []byte
	}{
		{"empty", nil},
		{"truncated array", []byte{0x95, 0x01}},
		{"not an array", []byte{0x81, 0xa2, 0x69, 0x64, 0x01}}, // a map, the old wire format
		{"wrong arity", []byte{0x93, 0x01, 0xa0, 0xc0}},
		{"id is a string", []byte{0x95, 0xa1, 0x78, 0xa0, 0xc0, 0xc0, 0xc0}},
		{"error is not an array", []byte{0x95, 0x01, 0xa0, 0xc0, 0xc0, 0xa1, 0x78}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var m Message
			if _, err := m.UnmarshalMsg(tt.in); err == nil {
				t.Error("UnmarshalMsg accepted it")
			}
			var m2 Message
			if err := m2.DecodeMsg(msgp.NewReader(bytes.NewReader(tt.in))); err == nil {
				t.Error("DecodeMsg accepted it")
			}
		})
	}
}

func TestToWire(t *testing.T) {
	args := testmsg.Args{A: 1, B: 2}
	rawArgs, err := args.MarshalMsg(nil)
	if err != nil {
		t.Fatal(err)
	}

	t.Run("request", func(t *testing.T) {
		var m Message
		if err := ToWire(&m, &birpc.Message{ID: 7, Func: "Arith.Add", Args: args}); err != nil {
			t.Fatal(err)
		}
		if m.ID != 7 || m.Func != "Arith.Add" {
			t.Errorf("header not copied: %+v", m)
		}
		if !bytes.Equal(m.Args, rawArgs) {
			t.Errorf("args\n got % x\nwant % x", m.Args, rawArgs)
		}
		if m.Result != nil || m.Error != nil {
			t.Errorf("result/error should be unset: %+v", m)
		}
	})

	t.Run("result", func(t *testing.T) {
		var m Message
		if err := ToWire(&m, &birpc.Message{ID: 7, Result: &testmsg.Result{C: 3}}); err != nil {
			t.Fatal(err)
		}
		if len(m.Result) == 0 {
			t.Error("result not marshaled")
		}
		if m.Args != nil {
			t.Error("args should be unset")
		}
	})

	t.Run("error", func(t *testing.T) {
		var m Message
		if err := ToWire(&m, &birpc.Message{ID: 7, Error: &birpc.Error{Msg: "nope"}}); err != nil {
			t.Fatal(err)
		}
		if m.Error == nil || m.Error.Msg != "nope" {
			t.Errorf("error not copied: %+v", m.Error)
		}
	})

	// a message reused across calls must not leak the previous payload
	t.Run("reuse clears the message", func(t *testing.T) {
		m := Message{
			ID:     1,
			Func:   "Old.Func",
			Args:   msgp.Raw{0x01},
			Result: msgp.Raw{0x02},
			Error:  &Error{Msg: "old"},
		}
		if err := ToWire(&m, &birpc.Message{ID: 2}); err != nil {
			t.Fatal(err)
		}
		if m.ID != 2 || m.Func != "" || m.Args != nil || m.Result != nil || m.Error != nil {
			t.Errorf("stale fields left behind: %+v", m)
		}
	})

	// silently dropping a payload the peer will never see is worse than failing
	t.Run("unserializable payload", func(t *testing.T) {
		for _, tt := range []struct {
			name string
			msg  birpc.Message
		}{
			{"args", birpc.Message{Args: testmsg.Plain{}}},
			{"result", birpc.Message{Result: &testmsg.Plain{}}},
			{"string args", birpc.Message{Args: "not messagepack"}},
		} {
			t.Run(tt.name, func(t *testing.T) {
				var m Message
				err := ToWire(&m, &tt.msg)
				if err == nil {
					t.Fatal("ToWire accepted a payload it cannot serialize")
				}
				if !strings.Contains(err.Error(), "msgp.Marshaler") {
					t.Errorf("unhelpful error: %v", err)
				}
			})
		}
	})
}

func TestFromWire(t *testing.T) {
	// birpc hands ReadMessage a zero valued Message, so a nil Error pointer is
	// the normal case. Writing through it used to panic on every error reply.
	t.Run("error response does not dereference nil", func(t *testing.T) {
		var msg birpc.Message
		FromWire(&msg, &Message{ID: 3, Error: &Error{Msg: "remote blew up"}})

		if msg.Error == nil {
			t.Fatal("error not copied")
		}
		if msg.Error.Msg != "remote blew up" {
			t.Errorf("got %q", msg.Error.Msg)
		}
	})

	t.Run("payloads are handed over raw", func(t *testing.T) {
		var msg birpc.Message
		FromWire(&msg, &Message{ID: 3, Func: "Arith.Add", Args: msgp.Raw{0x01}, Result: msgp.Raw{0x02}})

		if msg.ID != 3 || msg.Func != "Arith.Add" {
			t.Errorf("header not copied: %+v", msg)
		}
		if raw, ok := msg.Args.(msgp.Raw); !ok || !bytes.Equal(raw, []byte{0x01}) {
			t.Errorf("args: %#v", msg.Args)
		}
		if raw, ok := msg.Result.(msgp.Raw); !ok || !bytes.Equal(raw, []byte{0x02}) {
			t.Errorf("result: %#v", msg.Result)
		}
		if msg.Error != nil {
			t.Errorf("error should be nil: %v", msg.Error)
		}
	})

	// reusing a birpc.Message must not resurrect the previous error
	t.Run("reuse clears the error", func(t *testing.T) {
		msg := birpc.Message{Error: &birpc.Error{Msg: "stale"}}
		FromWire(&msg, &Message{ID: 4})

		if msg.Error != nil {
			t.Errorf("stale error left behind: %v", msg.Error)
		}
	})
}

func TestToWireFromWireRoundTrip(t *testing.T) {
	orig := birpc.Message{
		ID:    9,
		Func:  "Arith.Add",
		Args:  testmsg.Args{A: 10, B: 20, S: "x"},
		Error: &birpc.Error{Msg: "and an error"},
	}

	var m Message
	if err := ToWire(&m, &orig); err != nil {
		t.Fatal(err)
	}
	b, err := m.MarshalMsg(nil)
	if err != nil {
		t.Fatal(err)
	}

	var decoded Message
	if _, err := decoded.UnmarshalMsg(b); err != nil {
		t.Fatal(err)
	}
	var got birpc.Message
	FromWire(&got, &decoded)

	if got.ID != orig.ID || got.Func != orig.Func {
		t.Errorf("header: %+v", got)
	}
	if got.Error == nil || got.Error.Msg != orig.Error.Msg {
		t.Errorf("error: %+v", got.Error)
	}

	var args testmsg.Args
	if err := Unmarshal(got.Args, &args); err != nil {
		t.Fatal(err)
	}
	if args != orig.Args {
		t.Errorf("args: got %+v want %+v", args, orig.Args)
	}
}

func TestUnmarshal(t *testing.T) {
	raw, err := testmsg.Args{A: 5, B: 6, S: "s"}.MarshalMsg(nil)
	if err != nil {
		t.Fatal(err)
	}

	t.Run("decodes the payload", func(t *testing.T) {
		var got testmsg.Args
		if err := Unmarshal(msgp.Raw(raw), &got); err != nil {
			t.Fatal(err)
		}
		if want := (testmsg.Args{A: 5, B: 6, S: "s"}); got != want {
			t.Errorf("got %+v want %+v", got, want)
		}
	})

	// a peer that sent nil leaves the target alone instead of erroring out
	t.Run("nil payload", func(t *testing.T) {
		for _, tt := range []struct {
			name string
			in   interface{}
		}{
			{"untyped nil", nil},
			{"empty raw", msgp.Raw{}},
			{"nil raw", msgp.Raw(nil)},
		} {
			t.Run(tt.name, func(t *testing.T) {
				got := testmsg.Args{A: 1}
				if err := Unmarshal(tt.in, &got); err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if got.A != 1 {
					t.Errorf("target was modified: %+v", got)
				}
			})
		}
	})

	t.Run("target must be an unmarshaler", func(t *testing.T) {
		var notMsgp testmsg.Plain
		err := Unmarshal(msgp.Raw(raw), &notMsgp)
		if err == nil {
			t.Fatal("accepted a non msgp target")
		}
		if !strings.Contains(err.Error(), "msgp.Unmarshaler") {
			t.Errorf("unhelpful error: %v", err)
		}
	})

	// used to panic on a failed type assertion
	t.Run("payload must be raw", func(t *testing.T) {
		var got testmsg.Args
		if err := Unmarshal("i am not raw", &got); err == nil {
			t.Error("accepted a non raw payload")
		}
		if err := Unmarshal([]byte(raw), &got); err == nil {
			t.Error("accepted a plain byte slice")
		}
	})

	t.Run("corrupt payload", func(t *testing.T) {
		var got testmsg.Args
		if err := Unmarshal(msgp.Raw(raw[:len(raw)/2]), &got); err == nil {
			t.Error("accepted a truncated payload")
		}
		if err := Unmarshal(msgp.Raw{0xc3}, &got); err == nil {
			t.Error("accepted a boolean where a map was expected")
		}
	})
}

// Decoding whatever the peer sends must never panic, it is attacker controlled.
func FuzzMessageUnmarshal(f *testing.F) {
	f.Add([]byte{0x95, 0x01, 0xa0, 0xc0, 0xc0, 0xc0})
	f.Add([]byte{0x95, 0xcd, 0x01, 0x2c, 0xa9, 'A', 'r', 'i', 't', 'h', '.', 'A', 'd', 'd', 0x01, 0xc0, 0x91, 0xa4, 'b', 'o', 'o', 'm'})
	f.Add([]byte{0x81, 0xa2, 0x69, 0x64, 0x01})
	f.Add([]byte{})

	f.Fuzz(func(t *testing.T, in []byte) {
		var m Message
		if _, err := m.UnmarshalMsg(in); err != nil {
			return
		}

		// whatever came out has to survive a round trip
		b, err := m.MarshalMsg(nil)
		if err != nil {
			t.Fatalf("re-marshaling a decoded message failed: %v", err)
		}
		var again Message
		if _, err := again.UnmarshalMsg(b); err != nil {
			t.Fatalf("re-decoding a re-marshaled message failed: %v", err)
		}
		assertMessageEqual(t, &again, &m)

		var msg birpc.Message
		FromWire(&msg, &m)

		var args testmsg.Args
		_ = Unmarshal(msg.Args, &args)
	})
}

func assertMessageEqual(t *testing.T, got, want *Message) {
	t.Helper()

	if got.ID != want.ID {
		t.Errorf("id: got %d want %d", got.ID, want.ID)
	}
	if got.Func != want.Func {
		t.Errorf("fn: got %q want %q", got.Func, want.Func)
	}
	if !bytes.Equal(got.Args, want.Args) {
		t.Errorf("args:\n got % x\nwant % x", got.Args, want.Args)
	}
	if !bytes.Equal(got.Result, want.Result) {
		t.Errorf("result:\n got % x\nwant % x", got.Result, want.Result)
	}
	switch {
	case got.Error == nil && want.Error == nil:
	case got.Error == nil || want.Error == nil:
		t.Errorf("error: got %v want %v", got.Error, want.Error)
	case got.Error.Msg != want.Error.Msg:
		t.Errorf("error: got %q want %q", got.Error.Msg, want.Error.Msg)
	}
}
