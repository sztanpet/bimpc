package msgpcodec

// NOTE: THIS FILE WAS PRODUCED BY THE
// MSGP CODE GENERATION TOOL (github.com/tinylib/msgp)
// DO NOT EDIT

import (
	"github.com/tinylib/msgp/msgp"
)

// DecodeMsg implements msgp.Decodable
func (z *Error) DecodeMsg(dc *msgp.Reader) (err error) {
	var field []byte
	_ = field
	var isz uint32
	isz, err = dc.ReadMapHeader()
	if err != nil {
		return
	}
	for isz > 0 {
		isz--
		field, err = dc.ReadMapKeyPtr()
		if err != nil {
			return
		}
		switch msgp.UnsafeString(field) {
		case "msg":
			z.Msg, err = dc.ReadString()
			if err != nil {
				return
			}
		default:
			err = dc.Skip()
			if err != nil {
				return
			}
		}
	}
	return
}

// EncodeMsg implements msgp.Encodable
func (z Error) EncodeMsg(en *msgp.Writer) (err error) {
	// map header, size 1
	// write "msg"
	err = en.Append(0x81, 0xa3, 0x6d, 0x73, 0x67)
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
func (z Error) MarshalMsg(b []byte) (o []byte, err error) {
	o = msgp.Require(b, z.Msgsize())
	// map header, size 1
	// string "msg"
	o = append(o, 0x81, 0xa3, 0x6d, 0x73, 0x67)
	o = msgp.AppendString(o, z.Msg)
	return
}

// UnmarshalMsg implements msgp.Unmarshaler
func (z *Error) UnmarshalMsg(bts []byte) (o []byte, err error) {
	var field []byte
	_ = field
	var isz uint32
	isz, bts, err = msgp.ReadMapHeaderBytes(bts)
	if err != nil {
		return
	}
	for isz > 0 {
		isz--
		field, bts, err = msgp.ReadMapKeyZC(bts)
		if err != nil {
			return
		}
		switch msgp.UnsafeString(field) {
		case "msg":
			z.Msg, bts, err = msgp.ReadStringBytes(bts)
			if err != nil {
				return
			}
		default:
			bts, err = msgp.Skip(bts)
			if err != nil {
				return
			}
		}
	}
	o = bts
	return
}

func (z Error) Msgsize() (s int) {
	s = 1 + 4 + msgp.StringPrefixSize + len(z.Msg)
	return
}

// DecodeMsg implements msgp.Decodable
func (z *MsgPackMessage) DecodeMsg(dc *msgp.Reader) (err error) {
	var field []byte
	_ = field
	var isz uint32
	isz, err = dc.ReadMapHeader()
	if err != nil {
		return
	}
	for isz > 0 {
		isz--
		field, err = dc.ReadMapKeyPtr()
		if err != nil {
			return
		}
		switch msgp.UnsafeString(field) {
		case "id":
			z.ID, err = dc.ReadUint64()
			if err != nil {
				return
			}
		case "fn":
			z.Func, err = dc.ReadString()
			if err != nil {
				return
			}
		case "args":
			err = z.Args.DecodeMsg(dc)
			if err != nil {
				return
			}
		case "result":
			err = z.Result.DecodeMsg(dc)
			if err != nil {
				return
			}
		case "error":
			if dc.IsNil() {
				err = dc.ReadNil()
				if err != nil {
					return
				}
				z.Error = nil
			} else {
				if z.Error == nil {
					z.Error = new(Error)
				}
				var isz uint32
				isz, err = dc.ReadMapHeader()
				if err != nil {
					return
				}
				for isz > 0 {
					isz--
					field, err = dc.ReadMapKeyPtr()
					if err != nil {
						return
					}
					switch msgp.UnsafeString(field) {
					case "msg":
						z.Error.Msg, err = dc.ReadString()
						if err != nil {
							return
						}
					default:
						err = dc.Skip()
						if err != nil {
							return
						}
					}
				}
			}
		default:
			err = dc.Skip()
			if err != nil {
				return
			}
		}
	}
	return
}

// EncodeMsg implements msgp.Encodable
func (z *MsgPackMessage) EncodeMsg(en *msgp.Writer) (err error) {
	// map header, size 5
	// write "id"
	err = en.Append(0x85, 0xa2, 0x69, 0x64)
	if err != nil {
		return err
	}
	err = en.WriteUint64(z.ID)
	if err != nil {
		return
	}
	// write "fn"
	err = en.Append(0xa2, 0x66, 0x6e)
	if err != nil {
		return err
	}
	err = en.WriteString(z.Func)
	if err != nil {
		return
	}
	// write "args"
	err = en.Append(0xa4, 0x61, 0x72, 0x67, 0x73)
	if err != nil {
		return err
	}
	err = z.Args.EncodeMsg(en)
	if err != nil {
		return
	}
	// write "result"
	err = en.Append(0xa6, 0x72, 0x65, 0x73, 0x75, 0x6c, 0x74)
	if err != nil {
		return err
	}
	err = z.Result.EncodeMsg(en)
	if err != nil {
		return
	}
	// write "error"
	err = en.Append(0xa5, 0x65, 0x72, 0x72, 0x6f, 0x72)
	if err != nil {
		return err
	}
	if z.Error == nil {
		err = en.WriteNil()
		if err != nil {
			return
		}
	} else {
		// map header, size 1
		// write "msg"
		err = en.Append(0x81, 0xa3, 0x6d, 0x73, 0x67)
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
func (z *MsgPackMessage) MarshalMsg(b []byte) (o []byte, err error) {
	o = msgp.Require(b, z.Msgsize())
	// map header, size 5
	// string "id"
	o = append(o, 0x85, 0xa2, 0x69, 0x64)
	o = msgp.AppendUint64(o, z.ID)
	// string "fn"
	o = append(o, 0xa2, 0x66, 0x6e)
	o = msgp.AppendString(o, z.Func)
	// string "args"
	o = append(o, 0xa4, 0x61, 0x72, 0x67, 0x73)
	o, err = z.Args.MarshalMsg(o)
	if err != nil {
		return
	}
	// string "result"
	o = append(o, 0xa6, 0x72, 0x65, 0x73, 0x75, 0x6c, 0x74)
	o, err = z.Result.MarshalMsg(o)
	if err != nil {
		return
	}
	// string "error"
	o = append(o, 0xa5, 0x65, 0x72, 0x72, 0x6f, 0x72)
	if z.Error == nil {
		o = msgp.AppendNil(o)
	} else {
		// map header, size 1
		// string "msg"
		o = append(o, 0x81, 0xa3, 0x6d, 0x73, 0x67)
		o = msgp.AppendString(o, z.Error.Msg)
	}
	return
}

// UnmarshalMsg implements msgp.Unmarshaler
func (z *MsgPackMessage) UnmarshalMsg(bts []byte) (o []byte, err error) {
	var field []byte
	_ = field
	var isz uint32
	isz, bts, err = msgp.ReadMapHeaderBytes(bts)
	if err != nil {
		return
	}
	for isz > 0 {
		isz--
		field, bts, err = msgp.ReadMapKeyZC(bts)
		if err != nil {
			return
		}
		switch msgp.UnsafeString(field) {
		case "id":
			z.ID, bts, err = msgp.ReadUint64Bytes(bts)
			if err != nil {
				return
			}
		case "fn":
			z.Func, bts, err = msgp.ReadStringBytes(bts)
			if err != nil {
				return
			}
		case "args":
			bts, err = z.Args.UnmarshalMsg(bts)
			if err != nil {
				return
			}
		case "result":
			bts, err = z.Result.UnmarshalMsg(bts)
			if err != nil {
				return
			}
		case "error":
			if msgp.IsNil(bts) {
				bts, err = msgp.ReadNilBytes(bts)
				if err != nil {
					return
				}
				z.Error = nil
			} else {
				if z.Error == nil {
					z.Error = new(Error)
				}
				var isz uint32
				isz, bts, err = msgp.ReadMapHeaderBytes(bts)
				if err != nil {
					return
				}
				for isz > 0 {
					isz--
					field, bts, err = msgp.ReadMapKeyZC(bts)
					if err != nil {
						return
					}
					switch msgp.UnsafeString(field) {
					case "msg":
						z.Error.Msg, bts, err = msgp.ReadStringBytes(bts)
						if err != nil {
							return
						}
					default:
						bts, err = msgp.Skip(bts)
						if err != nil {
							return
						}
					}
				}
			}
		default:
			bts, err = msgp.Skip(bts)
			if err != nil {
				return
			}
		}
	}
	o = bts
	return
}

func (z *MsgPackMessage) Msgsize() (s int) {
	s = 1 + 3 + msgp.Uint64Size + 3 + msgp.StringPrefixSize + len(z.Func) + 5 + z.Args.Msgsize() + 7 + z.Result.Msgsize() + 6
	if z.Error == nil {
		s += msgp.NilSize
	} else {
		s += 1 + 4 + msgp.StringPrefixSize + len(z.Error.Msg)
	}
	return
}
