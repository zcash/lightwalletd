package parser

import "encoding"

type Serializable interface {
	encoding.BinaryMarshaler
	encoding.BinaryUnmarshaler
}

type Decoder interface {
	Decode(v Serializable) error
}
