package bytestring

import (
	"bytes"
	"testing"
)

func TestString_read(t *testing.T) {
	s := String{}
	if !(s).Empty() {
		t.Fatal("initial string not empty")
	}
	s = String{22, 33, 44}
	if s.Empty() {
		t.Fatal("string unexpectedly empty")
	}
	r := s.read(2)
	if len(r) != 2 {
		t.Fatal("unexpected string length after read()")
	}
	if !bytes.Equal(r, []byte{22, 33}) {
		t.Fatal("miscompare mismatch after read()")
	}
	r = s.read(0)
	if !bytes.Equal(r, []byte{}) {
		t.Fatal("miscompare mismatch after read()")
	}
	if s.read(2) != nil {
		t.Fatal("unexpected successful too-large read()")
	}
	r = s.read(1)
	if !bytes.Equal(r, []byte{44}) {
		t.Fatal("miscompare after read()")
	}
	r = s.read(0)
	if !bytes.Equal(r, []byte{}) {
		t.Fatal("miscompare after read()")
	}
	if s.read(1) != nil {
		t.Fatal("unexpected successful too-large read()")
	}
}

func TestString_Read(t *testing.T) {
	s := String{22, 33, 44}
	b := make([]byte, 10)
	n, err := s.Read(b)
	if err != nil {
		t.Fatal("Read() failed")
	}
	if n != 3 {
		t.Fatal("Read() returned incorrect length")
	}
	if !bytes.Equal(b[:3], []byte{22, 33, 44}) {
		t.Fatal("miscompare after Read()")
	}

	// s should now be empty
	n, err = s.Read(b)
	if err == nil {
		t.Fatal("Read() unexpectedly succeeded")
	}
	if n != 0 {
		t.Fatal("Read() failed as expected but returned incorrect length")
	}
	// s empty, the passed-in slice has zero length is not an error
	n, err = s.Read([]byte{})
	if err != nil {
		t.Fatal("Read() failed")
	}
	if n != 0 {
		t.Fatal("Read() returned non-zero length")
	}

	// make sure we can advance through string s (this time buffer smaller than s)
	s = String{55, 66, 77}
	b = make([]byte, 2)
	n, err = s.Read(b)
	if err != nil {
		t.Fatal("Read() failed")
	}
	if n != 2 {
		t.Fatal("Read() returned incorrect length")
	}
	if !bytes.Equal(b[:2], []byte{55, 66}) {
		t.Fatal("miscompare after Read()")
	}

	// keep reading s, one byte remaining
	n, err = s.Read(b)
	if err != nil {
		t.Fatal("Read() failed")
	}
	if n != 1 {
		t.Fatal("Read() returned incorrect length")
	}
	if !bytes.Equal(b[:1], []byte{77}) {
		t.Fatal("miscompare after Read()")
	}

	// If the buffer to read into is zero-length...
	s = String{88}
	n, err = s.Read([]byte{})
	if err != nil {
		t.Fatal("Read() into zero-length buffer failed")
	}
	if n != 0 {
		t.Fatal("Read() failed as expected but returned incorrect length")
	}
}

func TestString_Skip(t *testing.T) {
	s := String{22, 33, 44}
	b := make([]byte, 10)
	if !s.Skip(1) {
		t.Fatal("Skip() failed")
	}
	n, err := s.Read(b)
	if err != nil {
		t.Fatal("Read() failed")
	}
	if n != 2 {
		t.Fatal("Read() returned incorrect length")
	}
	if !bytes.Equal(b[:2], []byte{33, 44}) {
		t.Fatal("miscompare after Read()")
	}

	// we're at the end of the string
	if s.Skip(1) {
		t.Fatal("Skip() unexpectedly succeeded")
	}
	if !s.Skip(0) {
		t.Fatal("Skip(0) failed")
	}
}

func TestString_ReadByte(t *testing.T) {
	s := String{22, 33}
	var b byte
	if !s.ReadByte(&b) {
		t.Fatal("ReadByte() failed")
	}
	if b != 22 {
		t.Fatal("ReadByte() unexpected value")
	}
	if !s.ReadByte(&b) {
		t.Fatal("ReadByte() failed")
	}
	if b != 33 {
		t.Fatal("ReadByte() unexpected value")
	}

	// we're at the end of the string
	if s.ReadByte(&b) {
		t.Fatal("ReadByte() unexpectedly succeeded")
	}
}

func TestString_ReadBytes(t *testing.T) {
	s := String{22, 33, 44}
	var b []byte
	if !s.ReadBytes(&b, 2) {
		t.Fatal("ReadBytes() failed")
	}
	if !bytes.Equal(b, []byte{22, 33}) {
		t.Fatal("miscompare after ReadBytes()")
	}

	// s is now [44]
	if len(s) != 1 {
		t.Fatal("unexpected updated s following ReadBytes()")
	}
	if s.ReadBytes(&b, 2) {
		t.Fatal("ReadBytes() unexpected success")
	}
	if !s.ReadBytes(&b, 1) {
		t.Fatal("ReadBytes() failed")
	}
	if !bytes.Equal(b, []byte{44}) {
		t.Fatal("miscompare after ReadBytes()")
	}
}

var readCompactSizeTests = []struct {
	s        String
	ok       bool
	expected int
}{
	/* 00 */ {String{}, false, 0},
	/* 01 */ {String{43}, true, 43},
	/* 02 */ {String{252}, true, 252},
	/* 03 */ {String{253, 1, 0}, false, 0}, // 1 < minSize (253)
	/* 04 */ {String{253, 252, 0}, false, 0}, // 252 < minSize (253)
	/* 05 */ {String{253, 253, 0}, true, 253},
	/* 06 */ {String{253, 255, 255}, true, 0xffff},
	/* 07 */ {String{254, 0xff, 0xff, 0, 0}, false, 0}, // 0xffff < minSize
	/* 08 */ {String{254, 0, 0, 1, 0}, true, 0x00010000},
	/* 09 */ {String{254, 7, 0, 1, 0}, true, 0x00010007},
	/* 10 */ {String{254, 0, 0, 0, 2}, true, 0x02000000},
	/* 11 */ {String{254, 1, 0, 0, 2}, false, 0}, // > maxCompactSize
	/* 12 */ {String{255, 0, 0, 0, 2, 0, 0, 0, 0}, false, 0},
}

func TestString_ReadCompactSize(t *testing.T) {
	for i, tt := range readCompactSizeTests {
		var expected int
		ok := tt.s.ReadCompactSize(&expected)
		if ok != tt.ok {
			t.Fatalf("ReadCompactSize case %d: want: %v, have: %v", i, tt.ok, ok)
		}
		if expected != tt.expected {
			t.Fatalf("ReadCompactSize case %d: want: %v, have: %v", i, tt.expected, expected)
		}
	}
}

func TestString_ReadCompactLengthPrefixed(t *testing.T) {
	// a stream of 3 bytes followed by 2 bytes into the value variable, v
	s := String{3, 55, 66, 77, 2, 88, 99}
	v := String{}

	// read the 3 and thus the following 3 bytes
	if !s.ReadCompactLengthPrefixed(&v) {
		t.Fatalf("ReadCompactLengthPrefix failed")
	}
	if len(v) != 3 {
		t.Fatalf("ReadCompactLengthPrefix incorrect length")
	}
	if !bytes.Equal(v, String{55, 66, 77}) {
		t.Fatalf("ReadCompactLengthPrefix unexpected return")
	}

	// read the 2 and then two bytes
	if !s.ReadCompactLengthPrefixed(&v) {
		t.Fatalf("ReadCompactLengthPrefix failed")
	}
	if len(v) != 2 {
		t.Fatalf("ReadCompactLengthPrefix incorrect length")
	}
	if !bytes.Equal(v, String{88, 99}) {
		t.Fatalf("ReadCompactLengthPrefix unexpected return")
	}

	// at the end of the String, another read should return false
	if s.ReadCompactLengthPrefixed(&v) {
		t.Fatalf("ReadCompactLengthPrefix unexpected success")
	}

	// this string is too short (less than 2 bytes of data)
	s = String{3, 55, 66}
	if s.ReadCompactLengthPrefixed(&v) {
		t.Fatalf("ReadCompactLengthPrefix unexpected success")
	}
}

var readInt32Tests = []struct {
	s        String
	expected int32
}{
	// Little-endian (least-significant byte first)
	/* 00 */ {String{0, 0, 0, 0}, 0},
	/* 01 */ {String{17, 0, 0, 0}, 17},
	/* 02 */ {String{0xde, 0x8a, 0x7b, 0x72}, 0x727b8ade},
	/* 03 */ {String{0xde, 0x8a, 0x7b, 0x92}, -1837397282}, // signed overflow
	/* 04 */ {String{0xff, 0xff, 0xff, 0xff}, -1},
}

var readInt32FailTests = []struct {
	s String
}{
	/* 00 */ {String{}},
	/* 01 */ {String{1, 2, 3}}, // too few bytes (must be >= 4)
}

func TestString_ReadInt32(t *testing.T) {
	// create one large string to ensure a sequences of values can be read
	var s String
	for _, tt := range readInt32Tests {
		s = append(s, tt.s...)
	}
	for i, tt := range readInt32Tests {
		var v int32
		if !s.ReadInt32(&v) {
			t.Fatalf("ReadInt32 case %d: failed", i)
		}
		if v != tt.expected {
			t.Fatalf("ReadInt32 case %d: want: %v, have: %v", i, tt.expected, v)
		}
	}
	if len(s) > 0 {
		t.Fatalf("ReadInt32 bytes remaining: %d", len(s))
	}
	for i, tt := range readInt32FailTests {
		var v int32
		prevlen := len(tt.s)
		if tt.s.ReadInt32(&v) {
			t.Fatalf("ReadInt32 fail case %d: unexpected success", i)
		}
		if v != 0 {
			t.Fatalf("ReadInt32 fail case %d: value should be zero", i)
		}
		if len(tt.s) != prevlen {
			t.Fatalf("ReadInt32 fail case %d: some bytes consumed", i)
		}
	}
}

var readInt64Tests = []struct {
	s        String
	expected int64
}{
	// Little-endian (least-significant byte first)
	/* 00 */ {String{0, 0, 0, 0, 0, 0, 0, 0}, 0},
	/* 01 */ {String{17, 0, 0, 0, 0, 0, 0, 0}, 17},
	/* 02 */ {String{0xde, 0x8a, 0x7b, 0x72, 0x27, 0xa3, 0x94, 0x55}, 0x5594a327727b8ade},
	/* 03 */ {String{0xde, 0x8a, 0x7b, 0x72, 0x27, 0xa3, 0x94, 0x85}, -8821246380292207906}, // signed overflow
	/* 04 */ {String{0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff}, -1},
}

var readInt64FailTests = []struct {
	s String
}{
	/* 00 */ {String{}},
	/* 01 */ {String{1, 2, 3, 4, 5, 6, 7}}, // too few bytes (must be >= 8)
}

func TestString_ReadInt64(t *testing.T) {
	// create one large string to ensure a sequences of values can be read
	var s String
	for _, tt := range readInt64Tests {
		s = append(s, tt.s...)
	}
	for i, tt := range readInt64Tests {
		var v int64
		if !s.ReadInt64(&v) {
			t.Fatalf("ReadInt64 case %d: failed", i)
		}
		if v != tt.expected {
			t.Fatalf("ReadInt64 case %d: want: %v, have: %v", i, tt.expected, v)
		}
	}
	if len(s) > 0 {
		t.Fatalf("ReadInt64 bytes remaining: %d", len(s))
	}
	for i, tt := range readInt64FailTests {
		var v int64
		prevlen := len(tt.s)
		if tt.s.ReadInt64(&v) {
			t.Fatalf("ReadInt64 fail case %d: unexpected success", i)
		}
		if v != 0 {
			t.Fatalf("ReadInt32 fail case %d: value should be zero", i)
		}
		if len(tt.s) != prevlen {
			t.Fatalf("ReadInt64 fail case %d: some bytes consumed", i)
		}
	}
}

var readUint16Tests = []struct {
	s        String
	expected uint16
}{
	// Little-endian (least-significant byte first)
	/* 00 */ {String{0, 0}, 0},
	/* 01 */ {String{23, 0}, 23},
	/* 02 */ {String{0xde, 0x8a}, 0x8ade},
	/* 03 */ {String{0xff, 0xff}, 0xffff},
}

var readUint16FailTests = []struct {
	s String
}{
	/* 00 */ {String{}},
	/* 01 */ {String{1}}, // too few bytes (must be >= 2)
}

func TestString_ReadUint16(t *testing.T) {
	// create one large string to ensure a sequences of values can be read
	var s String
	for _, tt := range readUint16Tests {
		s = append(s, tt.s...)
	}
	for i, tt := range readUint16Tests {
		var v uint16
		if !s.ReadUint16(&v) {
			t.Fatalf("ReadUint16 case %d: failed", i)
		}
		if v != tt.expected {
			t.Fatalf("ReadUint16 case %d: want: %v, have: %v", i, tt.expected, v)
		}
	}
	if len(s) > 0 {
		t.Fatalf("ReadUint16 bytes remaining: %d", len(s))
	}
	for i, tt := range readUint16FailTests {
		var v uint16
		prevlen := len(tt.s)
		if tt.s.ReadUint16(&v) {
			t.Fatalf("ReadUint16 fail case %d: unexpected success", i)
		}
		if v != 0 {
			t.Fatalf("ReadInt32 fail case %d: value should be zero", i)
		}
		if len(tt.s) != prevlen {
			t.Fatalf("ReadUint16 fail case %d: some bytes consumed", i)
		}
	}
}

var readUint32Tests = []struct {
	s        String
	expected uint32
}{
	// Little-endian (least-significant byte first)
	/* 00 */ {String{0, 0, 0, 0}, 0},
	/* 01 */ {String{23, 0, 0, 0}, 23},
	/* 02 */ {String{0xde, 0x8a, 0x7b, 0x92}, 0x927b8ade},
	/* 03 */ {String{0xff, 0xff, 0xff, 0xff}, 0xffffffff},
}

var readUint32FailTests = []struct {
	s String
}{
	/* 00 */ {String{}},
	/* 01 */ {String{1, 2, 3}}, // too few bytes (must be >= 4)
}

func TestString_ReadUint32(t *testing.T) {
	// create one large string to ensure a sequences of values can be read
	var s String
	for _, tt := range readUint32Tests {
		s = append(s, tt.s...)
	}
	for i, tt := range readUint32Tests {
		var v uint32
		if !s.ReadUint32(&v) {
			t.Fatalf("ReadUint32 case %d: failed", i)
		}
		if v != tt.expected {
			t.Fatalf("ReadUint32 case %d: want: %v, have: %v", i, tt.expected, v)
		}
	}
	if len(s) > 0 {
		t.Fatalf("ReadUint32 bytes remaining: %d", len(s))
	}
	for i, tt := range readUint32FailTests {
		var v uint32
		prevlen := len(tt.s)
		if tt.s.ReadUint32(&v) {
			t.Fatalf("ReadUint32 fail case %d: unexpected success", i)
		}
		if v != 0 {
			t.Fatalf("ReadInt32 fail case %d: value should be zero", i)
		}
		if len(tt.s) != prevlen {
			t.Fatalf("ReadUint32 fail case %d: some bytes consumed", i)
		}
	}
}

var readUint64Tests = []struct {
	s        String
	expected uint64
}{
	// Little-endian (least-significant byte first)
	/* 00 */ {String{0, 0, 0, 0, 0, 0, 0, 0}, 0},
	/* 01 */ {String{17, 0, 0, 0, 0, 0, 0, 0}, 17},
	/* 03 */ {String{0xde, 0x8a, 0x7b, 0x72, 0x27, 0xa3, 0x94, 0x55}, 0x5594a327727b8ade},
	/* 04 */ {String{0xde, 0x8a, 0x7b, 0x72, 0x27, 0xa3, 0x94, 0x85}, 0x8594a327727b8ade},
	/* 05 */ {String{0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff}, 0xffffffffffffffff},
}

var readUint64FailTests = []struct {
	s String
}{
	/* 00 */ {String{}},
	/* 01 */ {String{1, 2, 3, 4, 5, 6, 7}}, // too few bytes (must be >= 8)
}

func TestString_ReadUint64(t *testing.T) {
	// create one large string to ensure a sequences of values can be read
	var s String
	for _, tt := range readUint64Tests {
		s = append(s, tt.s...)
	}
	for i, tt := range readUint64Tests {
		var v uint64
		if !s.ReadUint64(&v) {
			t.Fatalf("ReadUint64 case %d: failed", i)
		}
		if v != tt.expected {
			t.Fatalf("ReadUint64 case %d: want: %v, have: %v", i, tt.expected, v)
		}
	}
	if len(s) > 0 {
		t.Fatalf("ReadUint64 bytes remaining: %d", len(s))
	}
	for i, tt := range readUint64FailTests {
		var v uint64
		prevlen := len(tt.s)
		if tt.s.ReadUint64(&v) {
			t.Fatalf("ReadUint64 fail case %d: unexpected success", i)
		}
		if v != 0 {
			t.Fatalf("ReadInt64 fail case %d: value should be zero", i)
		}
		if len(tt.s) != prevlen {
			t.Fatalf("ReadUint64 fail case %d: some bytes consumed", i)
		}
	}
}

var readScriptInt64Tests = []struct {
	s        String
	ok       bool
	expected int64
}{
	// Little-endian (least-significant byte first).
	/* 00 */ {String{}, false, 0},
	/* 01 */ {String{0x4f}, true, -1},
	/* 02 */ {String{0x00}, true, 0x00},
	/* 03 */ {String{0x51}, true, 0x01},
	/* 04 */ {String{0x52}, true, 0x02},
	/* 05 */ {String{0x5f}, true, 0x0f},
	/* 06 */ {String{0x60}, true, 0x10},
	/* 07 */ {String{0x01}, false, 0}, // should be one byte following count 0x01
	/* 07 */ {String{0x01, 0xbd}, true, 0xbd},
	/* 07 */ {String{0x02, 0xbd, 0xac}, true, 0xacbd},
	/* 07 */ {String{0x08, 0xbd, 0xac, 0x12, 0x34, 0x56, 0x78, 0x9a, 0x44}, true, 0x449a78563412acbd},
	/* 07 */ {String{0x08, 0xbd, 0xac, 0x12, 0x34, 0x56, 0x78, 0x9a, 0x94}, true, -7738740698046616387},
}

func TestString_ReadScriptInt64(t *testing.T) {
	for i, tt := range readScriptInt64Tests {
		var v int64
		ok := tt.s.ReadScriptInt64(&v)
		if ok != tt.ok {
			t.Fatalf("ReadScriptInt64 case %d: want: %v, have: %v", i, tt.ok, ok)
		}
		if v != tt.expected {
			t.Fatalf("ReadScriptInt64 case %d: want: %v, have: %v", i, tt.expected, v)
		}
		// there should be no bytes remaining
		if ok && len(tt.s) != 0 {
			t.Fatalf("ReadScriptInt64 case %d: stream mispositioned", i)
		}
	}
}
