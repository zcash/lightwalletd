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
	// These (valid on testnet) correspond to the transactions in testdata/blocks;
	// for each block, the hashes for the tx within that block.
	var txhashes = [][]string{
		{
			"81096ff101a4f01d25ffd34a446bee4368bd46c233a59ac0faf101e1861c6b22",
		}, {
			"921dc41bef3a0d887c615abac60a29979efc8b4bbd3d887caeb6bb93501bde8e",
		}, {
			"d8e4c336ffa69dacaa4e0b4eaf8e3ae46897f1930a573c10b53837a03318c980",
			"4d5ccbfc6984680c481ff5ce145b8a93d59dfea90c150dfa45c938ab076ee5b2",
		}, {
			"df2b03619d441ce3d347e9278d87618e975079d0e235dfb3b3d8271510f707aa",
			"8d2593edfc328fa637b4ac91c7d569ee922bb9a6fda7cea230e92deb3ae4b634",
		},
	}
	testBlocks, err := os.Open("../testdata/blocks")
	if err != nil {
		t.Fatal(err)
	}
	defer testBlocks.Close()

	scan := bufio.NewScanner(testBlocks)
	for blockindex := 0; scan.Scan(); blockindex++ {
		blockDataHex := scan.Text()
		blockData, err := hex.DecodeString(blockDataHex)
		if err != nil {
			t.Error(err)
			continue
		}

		// This is just a sanity check of the test:
		if int(blockData[1487]) != len(txhashes[blockindex]) {
			t.Error("wrong number of transactions, test broken?")
		}

		// Make a copy of just the transactions alone, which,
		// for these blocks, start just beyond the header and
		// the one-byte nTx value, which is offset 1488.
		transactions := make([]byte, len(blockData[1488:]))
		copy(transactions, blockData[1488:])

		// Each iteration of this loop appends the block's original
		// transactions, so we build an ever-larger block. The loop
		// limit is arbitrary, but make sure we get into double-digit
		// transaction counts (compact integer).
		for i := 0; i < 264; i++ {
			b := blockData
			block := NewBlock()
			b, err = block.ParseFromSlice(b)
			if err != nil {
				t.Error(errors.Wrap(err, fmt.Sprintf("parsing block %d", i)))
				continue
			}
			if len(b) > 0 {
				t.Error("Extra data remaining")
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
			if block.GetTxCount() != len(txhashes[blockindex])*(i+1) {
				t.Error("Unexpected number of transactions")
			}
			if block.HasSaplingTransactions() {
				t.Error("Unexpected Sapling tx")
				break
			}
			for txindex, tx := range block.Transactions() {
				if tx.HasSaplingElements() {
					t.Error("Unexpected Sapling tx")
					break
				}
				expectedHash := txhashes[blockindex][txindex%len(txhashes[blockindex])]
				if hex.EncodeToString(tx.GetDisplayHash()) != expectedHash {
					t.Error("incorrect tx hash")
				}
			}
			// Keep appending the original transactions, which is unrealistic
			// because the coinbase is being replicated, but it works; first do
			// some surgery to the transaction count (see DarksideApplyStaged()).
			for j := 0; j < len(txhashes[blockindex]); j++ {
				nTxFirstByte := blockData[1487]
				switch {
				case nTxFirstByte < 252:
					blockData[1487]++
				case nTxFirstByte == 252:
					// incrementing to 253, requires "253" followed by 2-byte length,
					// extend the block by two bytes, shift existing transaction bytes
					blockData = append(blockData, 0, 0)
					copy(blockData[1490:], blockData[1488:len(blockData)-2])
					blockData[1487] = 253
					blockData[1488] = 253
					blockData[1489] = 0
				case nTxFirstByte == 253:
					blockData[1488]++
					if blockData[1488] == 0 {
						// wrapped around
						blockData[1489]++
					}
				}
			}
			blockData = append(blockData, transactions...)
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
		if len(blockData) > 0 {
			t.Error("Extra data remaining")
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
		if len(blockData) > 0 {
			t.Error("Extra data remaining")
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
