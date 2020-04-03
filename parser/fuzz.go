// Copyright (c) 2019-2020 The Zcash developers
// Distributed under the MIT software license, see the accompanying
// file COPYING or https://www.opensource.org/licenses/mit-license.php .
// +build gofuzz

package parser

func Fuzz(data []byte) int {
	block := NewBlock()
	_, err := block.ParseFromSlice(data)
	if err != nil {
		return 0
	}
	return 1
}
