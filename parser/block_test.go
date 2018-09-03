package parser

import (
	"bufio"
	"bytes"
	"encoding/hex"
	"os"
	"testing"
)

func TestReadBlockHeader(t *testing.T) {
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
		reader := bytes.NewReader(decodedBlockData)
		rawHeader, err := ReadBlockHeader(reader)
		if err != nil {
			t.Error(err)
			break
		}

		if rawHeader.Version != 4 {
			t.Error("Read wrong version in a test block.")
			break
		}

		if rawHeader.Time < lastBlockTime {
			t.Error("Block times not increasing.")
			break
		}
		lastBlockTime = rawHeader.Time

		if rawHeader.SolutionSize.Size != 1344 {
			t.Error("Got wrong Equihash solution size.")
			break
		}
	}
}
