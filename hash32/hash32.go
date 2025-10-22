// Copyright (c) 2025 The Zcash developers
// Distributed under the MIT software license, see the accompanying
// file COPYING or https://www.opensource.org/licenses/mit-license.php .

package hash32

import (
	"encoding/hex"
	"errors"
)

// This type is for any kind of 32-byte hash, such as block ID,
// txid, or merkle root. Variables of this type are passed
// around and returned by value (treat like an integer).
type T [32]byte

// It is considered impossible for a hash value to be all zeros,
// so we use that to represent an unset or undefined hash value.
var Nil = [32]byte{}

// FromSlice converts a slice to a hash32. If the slice is too long,
// the return is only the first 32 bytes; if the slice is too short,
// the remaining bytes in the return value are zeros. This should
// not happen in practice.
func FromSlice(arg []byte) T {
	return T(arg)
}

// ToSlice converts a hash32 to a byte slice.
func ToSlice(arg T) []byte {
	return arg[:]
}

// Reverse the given hash, returning a slice pointing to new data;
// the input slice is unchanged.
func Reverse(arg T) T {
	r := T{}
	for i := range 32 {
		r[i] = arg[32-1-i]
	}
	return r
}

func ReverseSlice(arg []byte) []byte {
	return ToSlice(Reverse(T(arg)))
}

func Decode(s string) (T, error) {
	r := T{}
	hash, err := hex.DecodeString(s)
	if err != nil {
		return r, err
	}
	if len(hash) != 32 {
		return r, errors.New("DecodeHexHash: length is not 32 bytes")
	}
	return T(hash), nil
}

func Encode(arg T) string {
	return hex.EncodeToString(ToSlice(arg))
}
