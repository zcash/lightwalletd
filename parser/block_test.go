package parser

import (
	"bufio"
	"bytes"
	"encoding/hex"
	"os"
	"testing"
)

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
		decodedBlockData, err := hex.DecodeString(blockDataHex)
		if err != nil {
			t.Error(err)
			continue
		}

		// Try to read the header
		blockHeader := &BlockHeader{}
		err = ReadBlockHeader(blockHeader, decodedBlockData)
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

		if blockHeader.SolutionSize.Size != 1344 {
			t.Error("Got wrong Equihash solution size.")
			break
		}

		// Re-serialize and check for consistency
		serializedHeader, err := blockHeader.MarshalBinary()
		if err != nil {
			t.Errorf("Error serializing header: %v", err)
			break
		}

		if !bytes.Equal(serializedHeader, decodedBlockData[:SER_BLOCK_HEADER_SIZE]) {
			offset := 0
			length := 0
			for i := 0; i < SER_BLOCK_HEADER_SIZE; i++ {
				if serializedHeader[i] != decodedBlockData[i] {
					if offset == 0 {
						offset = i
					}
					length++
				}
			}
			t.Errorf("Block header failed round-trip serialization:\nwant\n%x\ngot\n%x\nat %d", serializedHeader[offset:offset+length], decodedBlockData[offset:offset+length], offset)
			break
		}

		hash := blockHeader.GetBlockHash()

		// This is not necessarily true for anything but our current test cases.
		for _, b := range hash[28:] {
			if b != 0 {
				t.Errorf("Hash lacked trailing zeros")
			}
		}
	}
}
