package msgpcodec

// NOTE: THIS FILE WAS PRODUCED BY THE
// MSGP CODE GENERATION TOOL (github.com/tinylib/msgp)
// DO NOT EDIT

import (
	"github.com/tinylib/msgp/msgp"
)

// DecodeMsg implements msgp.Decodable
func (z *msgPackError) DecodeMsg(dc *msgp.Reader) (err error) {
	{
		var ssz uint32
		ssz, err = dc.ReadArrayHeader()
		if err != nil {
			return
		}
		if ssz != 1 {
			err = msgp.ArrayError{Wanted: 1, Got: ssz}
			return
		}
	}
	z.Msg, err = dc.ReadString()
	if err != nil {
		return
	}
	return
}

// EncodeMsg implements msgp.Encodable
func (z msgPackError) EncodeMsg(en *msgp.Writer) (err error) {
	// array header, size 1
	err = en.Append(0x91)
	if err != nil {
		return err
	}
	err = en.WriteString(z.Msg)
	if err != nil {
		return
	}
	return
}

// MarshalMsg implements msgp.Marshaler
func (z msgPackError) MarshalMsg(b []byte) (o []byte, err error) {
	o = msgp.Require(b, z.Msgsize())
	// array header, size 1
	o = append(o, 0x91)
	o = msgp.AppendString(o, z.Msg)
	return
}

// UnmarshalMsg implements msgp.Unmarshaler
func (z *msgPackError) UnmarshalMsg(bts []byte) (o []byte, err error) {
	{
		var ssz uint32
		ssz, bts, err = msgp.ReadArrayHeaderBytes(bts)
		if err != nil {
			return
		}
		if ssz != 1 {
			err = msgp.ArrayError{Wanted: 1, Got: ssz}
			return
		}
	}
	z.Msg, bts, err = msgp.ReadStringBytes(bts)
	if err != nil {
		return
	}
	o = bts
	return
}

func (z msgPackError) Msgsize() (s int) {
	s = 1 + msgp.StringPrefixSize + len(z.Msg)
	return
}

// DecodeMsg implements msgp.Decodable
func (z *msgPackMessage) DecodeMsg(dc *msgp.Reader) (err error) {
	{
		var ssz uint32
		ssz, err = dc.ReadArrayHeader()
		if err != nil {
			return
		}
		if ssz != 5 {
			err = msgp.ArrayError{Wanted: 5, Got: ssz}
			return
		}
	}
	z.ID, err = dc.ReadUint64()
	if err != nil {
		return
	}
	z.Func, err = dc.ReadString()
	if err != nil {
		return
	}
	err = z.Args.DecodeMsg(dc)
	if err != nil {
		return
	}
	err = z.Result.DecodeMsg(dc)
	if err != nil {
		return
	}
	if dc.IsNil() {
		err = dc.ReadNil()
		if err != nil {
			return
		}
		z.Error = nil
	} else {
		if z.Error == nil {
			z.Error = new(msgPackError)
		}
		{
			var ssz uint32
			ssz, err = dc.ReadArrayHeader()
			if err != nil {
				return
			}
			if ssz != 1 {
				err = msgp.ArrayError{Wanted: 1, Got: ssz}
				return
			}
		}
		z.Error.Msg, err = dc.ReadString()
		if err != nil {
			return
		}
	}
	return
}

// EncodeMsg implements msgp.Encodable
func (z *msgPackMessage) EncodeMsg(en *msgp.Writer) (err error) {
	// array header, size 5
	err = en.Append(0x95)
	if err != nil {
		return err
	}
	err = en.WriteUint64(z.ID)
	if err != nil {
		return
	}
	err = en.WriteString(z.Func)
	if err != nil {
		return
	}
	err = z.Args.EncodeMsg(en)
	if err != nil {
		return
	}
	err = z.Result.EncodeMsg(en)
	if err != nil {
		return
	}
	if z.Error == nil {
		err = en.WriteNil()
		if err != nil {
			return
		}
	} else {
		// array header, size 1
		err = en.Append(0x91)
		if err != nil {
			return err
		}
		err = en.WriteString(z.Error.Msg)
		if err != nil {
			return
		}
	}
	return
}

// MarshalMsg implements msgp.Marshaler
func (z *msgPackMessage) MarshalMsg(b []byte) (o []byte, err error) {
	o = msgp.Require(b, z.Msgsize())
	// array header, size 5
	o = append(o, 0x95)
	o = msgp.AppendUint64(o, z.ID)
	o = msgp.AppendString(o, z.Func)
	o, err = z.Args.MarshalMsg(o)
	if err != nil {
		return
	}
	o, err = z.Result.MarshalMsg(o)
	if err != nil {
		return
	}
	if z.Error == nil {
		o = msgp.AppendNil(o)
	} else {
		// array header, size 1
		o = append(o, 0x91)
		o = msgp.AppendString(o, z.Error.Msg)
	}
	return
}

// UnmarshalMsg implements msgp.Unmarshaler
func (z *msgPackMessage) UnmarshalMsg(bts []byte) (o []byte, err error) {
	{
		var ssz uint32
		ssz, bts, err = msgp.ReadArrayHeaderBytes(bts)
		if err != nil {
			return
		}
		if ssz != 5 {
			err = msgp.ArrayError{Wanted: 5, Got: ssz}
			return
		}
	}
	z.ID, bts, err = msgp.ReadUint64Bytes(bts)
	if err != nil {
		return
	}
	z.Func, bts, err = msgp.ReadStringBytes(bts)
	if err != nil {
		return
	}
	bts, err = z.Args.UnmarshalMsg(bts)
	if err != nil {
		return
	}
	bts, err = z.Result.UnmarshalMsg(bts)
	if err != nil {
		return
	}
	if msgp.IsNil(bts) {
		bts, err = msgp.ReadNilBytes(bts)
		if err != nil {
			return
		}
		z.Error = nil
	} else {
		if z.Error == nil {
			z.Error = new(msgPackError)
		}
		{
			var ssz uint32
			ssz, bts, err = msgp.ReadArrayHeaderBytes(bts)
			if err != nil {
				return
			}
			if ssz != 1 {
				err = msgp.ArrayError{Wanted: 1, Got: ssz}
				return
			}
		}
		z.Error.Msg, bts, err = msgp.ReadStringBytes(bts)
		if err != nil {
			return
		}
	}
	o = bts
	return
}

func (z *msgPackMessage) Msgsize() (s int) {
	s = 1 + msgp.Uint64Size + msgp.StringPrefixSize + len(z.Func) + z.Args.Msgsize() + z.Result.Msgsize()
	if z.Error == nil {
		s += msgp.NilSize
	} else {
		s += 1 + msgp.StringPrefixSize + len(z.Error.Msg)
	}
	return
}
