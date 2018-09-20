// Package bytestring provides a cryptobyte-inspired API specialized to the
// needs of parsing Zcash transactions.
package bytestring

import (
	"errors"
	"io"
)

const MAX_COMPACT_SIZE uint64 = 0x02000000

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
	if !s.Skip(n) {
		return 0, errors.New("unexpected end of bytestring read")
	}
	return n, nil
}

// Empty reports whether or not the string is empty.
func (s *String) Empty() bool {
	return len(*s) == 0
}

// Skip advances the string by n bytes and reports whether it was successful.
func (s *String) Skip(n int) bool {
	return s.read(n) != nil
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
func (s *String) ReadCompactSize(size *uint64) bool {
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
		lenLen = 8
		minSize = 0x100000000
	}

	if lenLen > 0 {
		// expect little endian uint of varying size
		lenBytes := s.read(lenLen)
		for i := lenLen - 1; i >= 0; i-- {
			length <<= 8
			length = length | uint64(lenBytes[i])
		}
	}

	if length > MAX_COMPACT_SIZE || length < minSize {
		return false
	}

	*size = length
	return true
}

// ReadCompactLengthPrefixed reads data prefixed by a CompactSize-encoded
// length field into out. It reports whether the read was successful.
func (s *String) ReadCompactLengthPrefixed(out *String) bool {
	var length uint64
	if ok := s.ReadCompactSize(&length); !ok {
		return false
	}

	v := s.read(int(length))
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
	if ok := s.ReadUint32(&tmp); !ok {
		return false
	}

	*out = int32(tmp)
	return true
}

// ReadInt64 decodes a little-endian 64-bit value into out, treating it as
// signed, and advances over it. It reports whether the read was successful.
func (s *String) ReadInt64(out *int64) bool {
	var tmp uint64
	if ok := s.ReadUint64(&tmp); !ok {
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
	*out = uint16(v[0]) | uint16(v[1])<<8
	return true
}

// ReadUint32 decodes a little-endian, 32-bit value into out and advances over
// it. It reports whether the read was successful.
func (s *String) ReadUint32(out *uint32) bool {
	v := s.read(4)
	if v == nil {
		return false
	}
	*out = uint32(v[0]) | uint32(v[1])<<8 | uint32(v[2])<<16 | uint32(v[3])<<24
	return true
}

// ReadUint64 decodes a little-endian, 64-bit value into out and advances over
// it. It reports whether the read was successful.
func (s *String) ReadUint64(out *uint64) bool {
	v := s.read(8)
	if v == nil {
		return false
	}
	*out = uint64(v[0]) | uint64(v[1])<<8 | uint64(v[2])<<16 | uint64(v[3])<<24 |
		uint64(v[4])<<32 | uint64(v[5])<<40 | uint64(v[6])<<48 | uint64(v[7])<<56
	return true
}
