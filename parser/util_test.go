// Copyright (c) 2019-2020 The Zcash developers
// Distributed under the MIT software license, see the accompanying
// file COPYING or https://www.opensource.org/licenses/mit-license.php .

package parser

import (
	"testing"
)

func TestReverse(t *testing.T) {
	s := make([]byte, 32, 32)
	for i := 0; i < 32; i++ {
		s[i] = byte(i)
	}
	r := Reverse(s)
	for i := 0; i < 32; i++ {
		if r[i] != byte(32-1-i) {
			t.Fatal("mismatch")
		}
	}
}

// Currently, Reverse() isn't called for odd-length slices, but
// it should work.
func TestReverseOdd(t *testing.T) {
	s := make([]byte, 5, 5)
	for i := 0; i < 5; i++ {
		s[i] = byte(i)
	}
	r := Reverse(s)
	for i := 0; i < 5; i++ {
		if r[i] != byte(5-1-i) {
			t.Fatal("mismatch")
		}
	}
}
