// Package bytestring provides a cryptobyte-inspired API specialized to the
// needs of parsing Zcash transactions.
package bytestring

import (
	"io"
)

const maxCompactSize uint64 = 0x02000000

const (
	op0       uint8 = 0x00
	op1Negate uint8 = 0x4f
	op1       uint8 = 0x51
	op16      uint8 = 0x60
)

// String represents a string of bytes and provides methods for parsing values
// from it.
type String []byte

// read advances the string by n bytes and returns them. If fewer than n bytes
// remain, it returns nil.
func (s *String) read(n int) []byte {
	if len(*s) < n {
		return nil
	}

	out := (*s)[:n]
	(*s) = (*s)[n:]
	return out
}

// Read reads the next len(p) bytes from the string, or the remainder of the
// string if len(*s) < len(p). It returns the number of bytes read as n. If the
// string is empty it returns an io.EOF error, or a nil error if len(p) == 0.
// Read satisfies io.Reader.
func (s *String) Read(p []byte) (n int, err error) {
	if s.Empty() {
		if len(p) == 0 {
			return 0, nil
		}
		return 0, io.EOF
	}

	n = copy(p, *s)
	s.Skip(n)
	return n, nil
}

// Empty reports whether or not the string is empty.
func (s *String) Empty() bool {
	return len(*s) == 0
}

// Skip advances the string by n bytes and reports whether it was successful.
func (s *String) Skip(n int) bool {
	if len(*s) < n {
		return false
	}
	(*s) = (*s)[n:]
	return true
}

// ReadByte reads a single byte into out and advances over it. It reports if
// the read was successful.
func (s *String) ReadByte(out *byte) bool {
	v := s.read(1)
	if v == nil {
		return false
	}
	*out = v[0]
	return true
}

// ReadBytes reads n bytes into out and advances over them. It reports if the
// read was successful.
func (s *String) ReadBytes(out *[]byte, n int) bool {
	v := s.read(n)
	if v == nil {
		return false
	}
	*out = v
	return true
}

// ReadCompactSize reads and interprets a Bitcoin-custom compact integer
// encoding used for length-prefixing and count values. If the values fall
// outside the expected canonical ranges, it returns false.
func (s *String) ReadCompactSize(size *int) bool {
	*size = 0
	lenBytes := s.read(1)
	if lenBytes == nil {
		return false
	}
	lenByte := lenBytes[0]

	var lenLen int
	var length, minSize uint64

	switch {
	case lenByte < 253:
		length = uint64(lenByte)
	case lenByte == 253:
		lenLen = 2
		minSize = 253
	case lenByte == 254:
		lenLen = 4
		minSize = 0x10000
	case lenByte == 255:
		// this case is not currently usable, beyond maxCompactSize;
		// also, this is not possible if sizeof(int) is 4 bytes
		//     lenLen = 8; minSize = 0x100000000
		return false
	}

	if lenLen > 0 {
		// expect little endian uint of varying size
		lenBytes := s.read(lenLen)
		if len(lenBytes) < lenLen {
			return false
		}
		for i := lenLen - 1; i >= 0; i-- {
			length <<= 8
			length = length | uint64(lenBytes[i])
		}
	}

	if length > maxCompactSize || length < minSize {
		return false
	}
	*size = int(length)
	return true
}

// ReadCompactLengthPrefixed reads data prefixed by a CompactSize-encoded
// length field into out. It reports whether the read was successful.
func (s *String) ReadCompactLengthPrefixed(out *String) bool {
	var length int
	if !s.ReadCompactSize(&length) {
		return false
	}

	v := s.read(length)
	if v == nil {
		return false
	}

	*out = v
	return true
}

// ReadInt32 decodes a little-endian 32-bit value into out, treating it as
// signed, and advances over it. It reports whether the read was successful.
func (s *String) ReadInt32(out *int32) bool {
	var tmp uint32
	if !s.ReadUint32(&tmp) {
		return false
	}

	*out = int32(tmp)
	return true
}

// ReadInt64 decodes a little-endian 64-bit value into out, treating it as
// signed, and advances over it. It reports whether the read was successful.
func (s *String) ReadInt64(out *int64) bool {
	var tmp uint64
	if !s.ReadUint64(&tmp) {
		return false
	}

	*out = int64(tmp)
	return true
}

// ReadUint16 decodes a little-endian, 16-bit value into out and advances over
// it. It reports whether the read was successful.
func (s *String) ReadUint16(out *uint16) bool {
	v := s.read(2)
	if v == nil {
		return false
	}
	*out = 0
	for i := 1; i >= 0; i-- {
		*out <<= 8
		*out |= uint16(v[i])
	}
	return true
}

// ReadUint32 decodes a little-endian, 32-bit value into out and advances over
// it. It reports whether the read was successful.
func (s *String) ReadUint32(out *uint32) bool {
	v := s.read(4)
	if v == nil {
		return false
	}
	*out = 0
	for i := 3; i >= 0; i-- {
		*out <<= 8
		*out |= uint32(v[i])
	}
	return true
}

// ReadUint64 decodes a little-endian, 64-bit value into out and advances over
// it. It reports whether the read was successful.
func (s *String) ReadUint64(out *uint64) bool {
	v := s.read(8)
	if v == nil {
		return false
	}
	*out = 0
	for i := 7; i >= 0; i-- {
		*out <<= 8
		*out |= uint64(v[i])
	}
	return true
}

// ReadScriptInt64 reads and interprets a Bitcoin-custom compact integer
// encoding used for int64 numbers in scripts.
//
// Serializer in zcashd:
// https://github.com/zcash/zcash/blob/4df60f4b334dd9aee5df3a481aee63f40b52654b/src/script/script.h#L363-L378
//
// Partial parser in zcashd:
// https://github.com/zcash/zcash/blob/4df60f4b334dd9aee5df3a481aee63f40b52654b/src/script/interpreter.cpp#L308-L335
func (s *String) ReadScriptInt64(num *int64) bool {
	// First byte is either an integer opcode, or the number of bytes in the
	// number.
	*num = 0
	firstBytes := s.read(1)
	if firstBytes == nil {
		return false
	}
	firstByte := firstBytes[0]

	var number uint64

	if firstByte == op1Negate {
		*num = -1
		return true
	} else if firstByte == op0 {
		number = 0
	} else if firstByte >= op1 && firstByte <= op16 {
		number = uint64(firstByte) - uint64(op1-1)
	} else {
		numLen := int(firstByte)
		// expect little endian int of varying size
		numBytes := s.read(numLen)
		if numBytes == nil {
			return false
		}
		for i := numLen - 1; i >= 0; i-- {
			number <<= 8
			number = number | uint64(numBytes[i])
		}
	}

	*num = int64(number)
	return true
}
