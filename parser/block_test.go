// Copyright (c) 2019-2020 The Zcash developers
// Distributed under the MIT software license, see the accompanying
// file COPYING or https://www.opensource.org/licenses/mit-license.php .
package parser

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"

	protobuf "github.com/golang/protobuf/proto"
)

func TestCompactBlocks(t *testing.T) {
	type compactTest struct {
		BlockHeight int    `json:"block"`
		BlockHash   string `json:"hash"`
		PrevHash    string `json:"prev"`
		Full        string `json:"full"`
		Compact     string `json:"compact"`
	}
	var compactTests []compactTest

	blockJSON, err := os.ReadFile("../testdata/compact_blocks.json")
	if err != nil {
		t.Fatal(err)
	}

	err = json.Unmarshal(blockJSON, &compactTests)
	if err != nil {
		t.Fatal(err)
	}

	for _, test := range compactTests {
		blockData, _ := hex.DecodeString(test.Full)
		block := NewBlock()
		blockData, err = block.ParseFromSlice(blockData)
		if err != nil {
			t.Error(fmt.Errorf("error parsing testnet block %d: %w", test.BlockHeight, err))
			continue
		}
		if len(blockData) > 0 {
			t.Error("Extra data remaining")
		}
		if block.GetHeight() != test.BlockHeight {
			t.Errorf("incorrect block height in testnet block %d", test.BlockHeight)
			continue
		}
		if block.GetDisplayHashString() != test.BlockHash {
			t.Errorf("incorrect block hash in testnet block %x", test.BlockHash)
			continue
		}
		if block.GetDisplayPrevHashString() != test.PrevHash {
			t.Errorf("incorrect block prevhash in testnet block %x", test.BlockHash)
			continue
		}
		if block.GetPrevHash() != block.hdr.HashPrevBlock {
			t.Error("block and block header prevhash don't match")
		}

		compact := block.ToCompact()
		marshaled, err := protobuf.Marshal(compact)
		if err != nil {
			t.Errorf("could not marshal compact testnet block %d", test.BlockHeight)
			continue
		}
		encodedCompact := hex.EncodeToString(marshaled)
		if encodedCompact != test.Compact {
			t.Errorf("wrong data for compact testnet block %d\nhave: %s\nwant: %s\n", test.BlockHeight, encodedCompact, test.Compact)
		}
	}

}

func TestParseBlockRejectsTransactionCountThatCannotFit(t *testing.T) {
	// 141-byte block header with version=4, zero-valued header fields, and an
	// empty CompactSize-prefixed Equihash solution, followed by tx_count=1 and
	// no transaction bytes.
	blockData := make([]byte, 141)
	blockData[0] = 0x04
	blockData = append(blockData, 0x01)

	block := NewBlock()
	_, err := block.ParseFromSlice(blockData)
	if err == nil {
		t.Fatal("expected error")
	}
	wantErr := "tx_count 1 requires at least 10 bytes, but only 0 remain"
	if !strings.Contains(err.Error(), wantErr) {
		t.Fatalf("error mismatch:\nhave: %v\nwant substring: %s", err, wantErr)
	}
}

// minimalBlockHeader is a 141-byte version-4 header with zero-valued fields
// and an empty Equihash solution.
func minimalBlockHeader() []byte {
	hdr := make([]byte, 141)
	hdr[0] = 0x04
	return hdr
}

func TestParseBlockRejectsNoTransactions(t *testing.T) {
	blockData := append(minimalBlockHeader(), 0x00)
	_, err := NewBlock().ParseFromSlice(blockData)
	if err == nil || !strings.Contains(err.Error(), "no transactions") {
		t.Fatalf("got %v, want a no-transactions error", err)
	}
}

func TestParseBlockRejectsInputlessCoinbase(t *testing.T) {
	// A version-1 transaction with no inputs, no outputs and lock time 0.
	coinbase := []byte{0x01, 0, 0, 0, 0x00, 0x00, 0, 0, 0, 0}
	blockData := append(append(minimalBlockHeader(), 0x01), coinbase...)
	_, err := NewBlock().ParseFromSlice(blockData)
	if err == nil || !strings.Contains(err.Error(), "no transparent input") {
		t.Fatalf("got %v, want a no-transparent-input error", err)
	}
}

func TestGetHeightWithoutCoinbaseInput(t *testing.T) {
	if h := NewBlock().GetHeight(); h != -1 {
		t.Fatalf("block with no transactions: height %d, want -1", h)
	}
	b := NewBlock()
	b.vtx = []*Transaction{NewTransaction()}
	if h := b.GetHeight(); h != -1 {
		t.Fatalf("coinbase with no inputs: height %d, want -1", h)
	}
}
