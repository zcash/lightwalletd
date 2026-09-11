// Copyright (c) 2025 The Zcash developers
// Distributed under the MIT software license, see the accompanying
// file COPYING or https://www.opensource.org/licenses/mit-license.php .

package parser

import (
	"encoding/hex"
	"encoding/json"
	"os"
	"testing"

	"github.com/zcash/lightwalletd/hash32"
)

func TestComputeV5TxID(t *testing.T) {
	s, err := os.ReadFile("../testdata/tx_v5.json")
	if err != nil {
		t.Fatal(err)
	}

	var testdata []json.RawMessage
	if err := json.Unmarshal(s, &testdata); err != nil {
		t.Fatal(err)
	}
	if len(testdata) < 3 {
		t.Fatal("tx_v5.json has too few lines")
	}
	testdata = testdata[2:]

	for i, onetx := range testdata {
		var td TxTestData
		if err := json.Unmarshal(onetx, &td); err != nil {
			t.Fatal(err)
		}

		rawTxData, err := hex.DecodeString(td.Tx)
		if err != nil {
			t.Fatalf("test %d: bad hex: %v", i, err)
		}

		txid, err := computeV5TxID(rawTxData)
		if err != nil {
			t.Fatalf("test %d (txid %s): computeV5TxID: %v", i, td.Txid, err)
		}

		// Test vector txids are in big-endian display format.
		got := hash32.Encode(hash32.Reverse(txid))
		if got != td.Txid {
			t.Fatalf("test %d txid mismatch:\n  got  %s\n  want %s", i, got, td.Txid)
		}
	}
}

func TestComputeV5TxIDViaParseFromSlice(t *testing.T) {
	s, err := os.ReadFile("../testdata/tx_v5.json")
	if err != nil {
		t.Fatal(err)
	}

	var testdata []json.RawMessage
	if err := json.Unmarshal(s, &testdata); err != nil {
		t.Fatal(err)
	}
	testdata = testdata[2:]

	for i, onetx := range testdata {
		var td TxTestData
		if err := json.Unmarshal(onetx, &td); err != nil {
			t.Fatal(err)
		}

		rawTxData, _ := hex.DecodeString(td.Tx)
		tx := NewTransaction()
		rest, err := tx.ParseFromSlice(rawTxData)
		if err != nil {
			t.Fatalf("test %d: ParseFromSlice: %v", i, err)
		}
		if len(rest) != 0 {
			t.Fatalf("test %d: %d bytes remaining", i, len(rest))
		}

		got := tx.GetDisplayHashString()
		if got != td.Txid {
			t.Fatalf("test %d txid mismatch:\n  got  %s\n  want %s", i, got, td.Txid)
		}
	}
}
