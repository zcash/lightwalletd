// Copyright (c) 2019-2020 The Zcash developers
// Distributed under the MIT software license, see the accompanying
// file COPYING or https://www.opensource.org/licenses/mit-license.php .
package parser

import (
	"bufio"
	"bytes"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"testing"

	"github.com/pkg/errors"

	protobuf "github.com/golang/protobuf/proto"
)

func TestBlockParser(t *testing.T) {
	// These (valid on testnet) correspond to the transactions in testdata/blocks
	var txhashes = []string{
		"81096ff101a4f01d25ffd34a446bee4368bd46c233a59ac0faf101e1861c6b22",
		"921dc41bef3a0d887c615abac60a29979efc8b4bbd3d887caeb6bb93501bde8e",
		"d8e4c336ffa69dacaa4e0b4eaf8e3ae46897f1930a573c10b53837a03318c980",
		"4d5ccbfc6984680c481ff5ce145b8a93d59dfea90c150dfa45c938ab076ee5b2",
		"df2b03619d441ce3d347e9278d87618e975079d0e235dfb3b3d8271510f707aa",
		"8d2593edfc328fa637b4ac91c7d569ee922bb9a6fda7cea230e92deb3ae4b634",
	}
	txindex := 0
	testBlocks, err := os.Open("../testdata/blocks")
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
		if block.GetVersion() != 4 {
			t.Error("Read wrong version in a test block.")
			break
		}
		if block.GetTxCount() < 1 {
			t.Error("No transactions in block")
			break
		}
		if len(block.Transactions()) != block.GetTxCount() {
			t.Error("Number of transactions mismatch")
			break
		}
		if block.HasSaplingTransactions() {
			t.Error("Unexpected Saping tx")
			break
		}
		for _, tx := range block.Transactions() {
			if tx.HasSaplingElements() {
				t.Error("Unexpected Saping tx")
				break
			}
			if hex.EncodeToString(tx.GetDisplayHash()) != txhashes[txindex] {
				t.Error("incorrect tx hash")
			}
			txindex++
		}
	}
}

func TestBlockParserFail(t *testing.T) {
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
	for i := 0; scan.Scan(); i++ {
		blockDataHex := scan.Text()
		blockData, err := hex.DecodeString(blockDataHex)
		if err != nil {
			t.Error(err)
			continue
		}

		block := NewBlock()
		blockData, err = block.ParseFromSlice(blockData)
		if err == nil {
			t.Error("unexpected success parsing bad block")
		}
	}
}

// Checks on the first 20 blocks from mainnet genesis.
func TestGenesisBlockParser(t *testing.T) {
	blockFile, err := os.Open("../testdata/mainnet_genesis")
	if err != nil {
		t.Fatal(err)
	}
	defer blockFile.Close()

	scan := bufio.NewScanner(blockFile)
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
			t.Error(err)
			continue
		}

		// Some basic sanity checks
		if block.hdr.Version != 4 {
			t.Error("Read wrong version in genesis block.")
			break
		}

		if block.GetHeight() != i {
			t.Errorf("Got wrong height for block %d: %d", i, block.GetHeight())
		}
	}
}

func TestCompactBlocks(t *testing.T) {
	type compactTest struct {
		BlockHeight int    `json:"block"`
		BlockHash   string `json:"hash"`
		PrevHash    string `json:"prev"`
		Full        string `json:"full"`
		Compact     string `json:"compact"`
	}
	var compactTests []compactTest

	blockJSON, err := ioutil.ReadFile("../testdata/compact_blocks.json")
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
			t.Error(errors.Wrap(err, fmt.Sprintf("parsing testnet block %d", test.BlockHeight)))
			continue
		}
		if block.GetHeight() != test.BlockHeight {
			t.Errorf("incorrect block height in testnet block %d", test.BlockHeight)
			continue
		}
		if hex.EncodeToString(block.GetDisplayHash()) != test.BlockHash {
			t.Errorf("incorrect block hash in testnet block %x", test.BlockHash)
			continue
		}
		if hex.EncodeToString(block.GetDisplayPrevHash()) != test.PrevHash {
			t.Errorf("incorrect block prevhash in testnet block %x", test.BlockHash)
			continue
		}
		if !bytes.Equal(block.GetPrevHash(), block.hdr.HashPrevBlock) {
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
			break
		}
	}

}
