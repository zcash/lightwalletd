// Copyright (c) 2019-2020 The Zcash developers
// Distributed under the MIT software license, see the accompanying
// file COPYING or https://www.opensource.org/licenses/mit-license.php .

package parser

// Reverse the given byte slice, returning a slice pointing to new data;
// the input slice is unchanged.
func Reverse(a []byte) []byte {
	r := make([]byte, len(a), len(a))
	for left, right := 0, len(a)-1; left <= right; left, right = left+1, right-1 {
		r[left], r[right] = a[right], a[left]
	}
	return r
}
