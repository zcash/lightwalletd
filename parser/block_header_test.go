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
	testBlocks, err := os.Open("testdata/blocks")
	if err != nil {
		t.Fatal(err)
	}
	defer testBlocks.Close()

	lastBlockTime := uint32(0)

	scan := bufio.NewScanner(testBlocks)
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

		if len(blockHeader.Solution) != EQUIHASH_SIZE {
			t.Error("Got wrong Equihash solution size.")
			break
		}

		// Re-serialize and check for consistency
		serializedHeader, err := blockHeader.MarshalBinary()
		if err != nil {
			t.Errorf("Error serializing header: %v", err)
			break
		}

		if !bytes.Equal(serializedHeader, blockData[:SER_BLOCK_HEADER_SIZE]) {
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

		// This is not necessarily true for anything but our current test cases.
		for _, b := range hash[:4] {
			if b != 0 {
				t.Errorf("Hash lacked leading zeros: %x", hash)
			}
		}
	}
}
