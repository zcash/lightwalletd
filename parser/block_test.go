package parser

import (
	"bufio"
	"encoding/hex"
	"fmt"
	"os"
	"testing"

	"github.com/pkg/errors"
)

func TestBlockParser(t *testing.T) {
	testBlocks, err := os.Open("testdata/blocks")
	if err != nil {
		t.Fatal(err)
	}
	defer testBlocks.Close()

	scan := bufio.NewScanner(testBlocks)
	for i := 0; scan.Scan(); i++ {
		blockDataHex := scan.Text()
		blockData, err := hex.DecodeString(blockDataHex)
		if err != nil {
			t.Error(err)
			continue
		}

		block := NewBlock()
		blockData, err = block.ParseFromSlice(blockData)
		if err != nil {
			t.Error(errors.Wrap(err, fmt.Sprintf("parsing block %d", i)))
			continue
		}

		// Some basic sanity checks
		if block.hdr.Version != 4 {
			t.Error("Read wrong version in a test block.")
			break
		}
	}
}
