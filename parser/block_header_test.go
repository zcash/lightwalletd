// Copyright (c) 2019-2020 The Zcash developers
// Distributed under the MIT software license, see the accompanying
// file COPYING or https://www.opensource.org/licenses/mit-license.php .
package parser

import (
	"bufio"
	"bytes"
	"encoding/hex"
	"math/big"
	"os"
	"testing"
)

// https://bitcoin.org/en/developer-reference#target-nbits
var nbitsTests = []struct {
	bytes  []byte
	target string
}{
	{
		[]byte{0x18, 0x1b, 0xc3, 0x30},
		"1bc330000000000000000000000000000000000000000000",
	},
	{
		[]byte{0x01, 0x00, 0x34, 0x56},
		"00",
	},
	{
		[]byte{0x01, 0x12, 0x34, 0x56},
		"12",
	},
	{
		[]byte{0x02, 0x00, 0x80, 00},
		"80",
	},
	{
		[]byte{0x05, 0x00, 0x92, 0x34},
		"92340000",
	},
	{
		[]byte{0x04, 0x92, 0x34, 0x56},
		"-12345600",
	},
	{
		[]byte{0x04, 0x12, 0x34, 0x56},
		"12345600",
	},
}

func TestParseNBits(t *testing.T) {
	for i, tt := range nbitsTests {
		target := parseNBits(tt.bytes)
		expected, _ := new(big.Int).SetString(tt.target, 16)
		if target.Cmp(expected) != 0 {
			t.Errorf("NBits parsing failed case %d:\nwant: %x\nhave: %x", i, expected, target)
		}
	}
}

func TestBlockHeader(t *testing.T) {
	testBlocks, err := os.Open("../testdata/blocks")
	if err != nil {
		t.Fatal(err)
	}
	defer testBlocks.Close()

	lastBlockTime := uint32(0)

	scan := bufio.NewScanner(testBlocks)
	var prevHash []byte
	for scan.Scan() {
		blockDataHex := scan.Text()
		blockData, err := hex.DecodeString(blockDataHex)
		if err != nil {
			t.Error(err)
			continue
		}

		blockHeader := NewBlockHeader()
		_, err = blockHeader.ParseFromSlice(blockData)
		if err != nil {
			t.Error(err)
			continue
		}

		// Some basic sanity checks
		if blockHeader.Version != 4 {
			t.Error("Read wrong version in a test block.")
			break
		}

		if blockHeader.Time < lastBlockTime {
			t.Error("Block times not increasing.")
			break
		}
		lastBlockTime = blockHeader.Time

		if len(blockHeader.Solution) != equihashSizeMainnet {
			t.Error("Got wrong Equihash solution size.")
			break
		}

		// Re-serialize and check for consistency
		serializedHeader, err := blockHeader.MarshalBinary()
		if err != nil {
			t.Errorf("Error serializing header: %v", err)
			break
		}

		if !bytes.Equal(serializedHeader, blockData[:serBlockHeaderMinusEquihashSize+3+equihashSizeMainnet]) {
			offset := 0
			length := 0
			for i := 0; i < len(serializedHeader); i++ {
				if serializedHeader[i] != blockData[i] {
					if offset == 0 {
						offset = i
					}
					length++
				}
			}
			t.Errorf(
				"Block header failed round-trip:\ngot\n%x\nwant\n%x\nfirst diff at %d",
				serializedHeader[offset:offset+length],
				blockData[offset:offset+length],
				offset,
			)
			break
		}

		hash := blockHeader.GetDisplayHash()
		// test caching
		if !bytes.Equal(hash, blockHeader.GetDisplayHash()) {
			t.Error("caching is broken")
		}

		// This is not necessarily true for anything but our current test cases.
		for _, b := range hash[:4] {
			if b != 0 {
				t.Errorf("Hash lacked leading zeros: %x", hash)
			}
		}
		if prevHash != nil && !bytes.Equal(blockHeader.GetDisplayPrevHash(), prevHash) {
			t.Errorf("Previous hash mismatch")
		}
		prevHash = hash
	}
}

func TestBadBlockHeader(t *testing.T) {
	testBlocks, err := os.Open("../testdata/badblocks")
	if err != nil {
		t.Fatal(err)
	}
	defer testBlocks.Close()

	scan := bufio.NewScanner(testBlocks)

	// the first "block" contains an illegal hex character
	{
		scan.Scan()
		blockDataHex := scan.Text()
		_, err := hex.DecodeString(blockDataHex)
		if err == nil {
			t.Error("unexpected success parsing illegal hex bad block")
		}
	}
	// these bad blocks are short in various ways
	for i := 1; scan.Scan(); i++ {
		blockDataHex := scan.Text()
		blockData, err := hex.DecodeString(blockDataHex)
		if err != nil {
			t.Error(err)
			continue
		}

		blockHeader := NewBlockHeader()
		_, err = blockHeader.ParseFromSlice(blockData)
		if err == nil {
			t.Errorf("unexpected success parsing bad block %d", i)
		}
	}
}

var compactLengthPrefixedLenTests = []struct {
	length       int
	returnLength int
}{
	/* 00 */ {0, 1},
	/* 01 */ {1, 1 + 1},
	/* 02 */ {2, 1 + 2},
	/* 03 */ {252, 1 + 252},
	/* 04 */ {253, 1 + 2 + 253},
	/* 05 */ {0xffff, 1 + 2 + 0xffff},
	/* 06 */ {0x10000, 1 + 4 + 0x10000},
	/* 07 */ {0x10001, 1 + 4 + 0x10001},
	/* 08 */ {0xffffffff, 1 + 4 + 0xffffffff},
	/* 09 */ {0x100000000, 1 + 8 + 0x100000000},
	/* 10 */ {0x100000001, 1 + 8 + 0x100000001},
}

func TestCompactLengthPrefixedLen(t *testing.T) {
	for i, tt := range compactLengthPrefixedLenTests {
		returnLength := CompactLengthPrefixedLen(tt.length)
		if returnLength != tt.returnLength {
			t.Errorf("TestCompactLengthPrefixedLen case %d: want: %v have %v",
				i, tt.returnLength, returnLength)
		}
	}
}

var writeCompactLengthPrefixedTests = []struct {
	argLen       int
	returnLength int
	header       []byte
}{
	/* 00 */ {0, 1, []byte{0}},
	/* 01 */ {1, 1, []byte{1}},
	/* 02 */ {2, 1, []byte{2}},
	/* 03 */ {252, 1, []byte{252}},
	/* 04 */ {253, 1 + 2, []byte{253, 253, 0}},
	/* 05 */ {254, 1 + 2, []byte{253, 254, 0}},
	/* 06 */ {0xffff, 1 + 2, []byte{253, 0xff, 0xff}},
	/* 07 */ {0x10000, 1 + 4, []byte{254, 0x00, 0x00, 0x01, 0x00}},
	/* 08 */ {0x10003, 1 + 4, []byte{254, 0x03, 0x00, 0x01, 0x00}},
	/* 09 */ {0xffffffff, 1 + 4, []byte{254, 0xff, 0xff, 0xff, 0xff}},
	/* 10 */ {0x100000000, 1 + 8, []byte{255, 0x00, 0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00}},
	/* 11 */ {0x100000007, 1 + 8, []byte{255, 0x07, 0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00}},
}

func TestWriteCompactLengthPrefixedLen(t *testing.T) {
	for i, tt := range writeCompactLengthPrefixedTests {
		var b bytes.Buffer
		WriteCompactLengthPrefixedLen(&b, tt.argLen)
		if b.Len() != tt.returnLength {
			t.Fatalf("TestWriteCompactLengthPrefixed case %d: unexpected length", i)
		}
		// check the header (tag and length)
		r := make([]byte, len(tt.header))
		b.Read(r)
		if !bytes.Equal(r, tt.header) {
			t.Fatalf("TestWriteCompactLengthPrefixed case %d: incorrect header", i)
		}
		if b.Len() > 0 {
			t.Fatalf("TestWriteCompactLengthPrefixed case %d: unexpected data remaining", i)
		}
	}
}

func TestWriteCompactLengthPrefixed(t *testing.T) {
	var b bytes.Buffer
	val := []byte{22, 33, 44}
	WriteCompactLengthPrefixed(&b, val)
	r := make([]byte, 4)
	b.Read(r)
	expected := []byte{3, 22, 33, 44}
	if !bytes.Equal(r, expected) {
		t.Fatal("TestWriteCompactLengthPrefixed incorrect result")
	}
}
