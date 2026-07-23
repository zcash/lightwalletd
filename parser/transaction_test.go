// Copyright (c) 2019-2020 The Zcash developers
// Distributed under the MIT software license, see the accompanying
// file COPYING or https://www.opensource.org/licenses/mit-license.php .
package parser

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"os"
	"strings"
	"testing"
)

// Some of these values may be "null" (which translates to nil in Go) in
// the test data, so we have *_set variables to indicate if the corresponding
// variable is non-null. (There is an "optional" package we could use for
// these but it doesn't seem worth pulling it in.)
type TxTestData struct {
	Tx                 string
	Txid               string
	Version            int
	NVersionGroupId    int
	NConsensusBranchId int
	Tx_in_count        int
	Tx_out_count       int
	NSpendsSapling     int
	NoutputsSapling    int
	NActionsOrchard    int
}

// https://jhall.io/posts/go-json-tricks-array-as-structs/
func (r *TxTestData) UnmarshalJSON(p []byte) error {
	var t []any
	if err := json.Unmarshal(p, &t); err != nil {
		return err
	}
	r.Tx = t[0].(string)
	r.Txid = t[1].(string)
	r.Version = int(t[2].(float64))
	r.NVersionGroupId = int(t[3].(float64))
	r.NConsensusBranchId = int(t[4].(float64))
	r.Tx_in_count = int(t[7].(float64))
	r.Tx_out_count = int(t[8].(float64))
	r.NSpendsSapling = int(t[9].(float64))
	r.NoutputsSapling = int(t[10].(float64))
	r.NActionsOrchard = int(t[14].(float64))
	return nil
}

func TestV5TransactionParser(t *testing.T) {
	// The raw data are stored in a separate file because they're large enough
	// to make the test table difficult to scroll through. They are in the same
	// order as the test table above. If you update the test table without
	// adding a line to the raw file, this test will panic due to index
	// misalignment.
	s, err := os.ReadFile("../testdata/tx_v5.json")
	if err != nil {
		t.Fatal(err)
	}

	var testdata []json.RawMessage
	err = json.Unmarshal(s, &testdata)
	if err != nil {
		t.Fatal(err)
	}
	if len(testdata) < 3 {
		t.Fatal("tx_vt.json has too few lines")
	}
	testdata = testdata[2:]
	for _, onetx := range testdata {
		var txtestdata TxTestData

		err = json.Unmarshal(onetx, &txtestdata)
		if err != nil {
			t.Fatal(err)
		}
		t.Logf("txid %s", txtestdata.Txid)
		rawTxData, _ := hex.DecodeString(txtestdata.Tx)

		tx := NewTransaction()
		rest, err := tx.ParseFromSlice(rawTxData)
		if err != nil {
			t.Fatalf("%v", err)
		}
		if len(rest) != 0 {
			t.Fatalf("Test did not consume entire buffer, %d remaining", len(rest))
		}
		// Currently, we can't check the txid because we get that from
		// zcashd (getblock rpc) rather than computing it ourselves.
		// https://github.com/zcash/lightwalletd/issues/392
		if tx.version != uint32(txtestdata.Version) {
			t.Fatal("version miscompare")
		}
		if tx.nVersionGroupID != uint32(txtestdata.NVersionGroupId) {
			t.Fatal("nVersionGroupId miscompare")
		}
		if tx.consensusBranchID != uint32(txtestdata.NConsensusBranchId) {
			t.Fatal("consensusBranchID miscompare")
		}
		if len(tx.transparentInputs) != int(txtestdata.Tx_in_count) {
			t.Fatal("tx_in_count miscompare")
		}
		if len(tx.transparentOutputs) != int(txtestdata.Tx_out_count) {
			t.Fatal("tx_out_count miscompare")
		}
		if len(tx.shieldedSpends) != int(txtestdata.NSpendsSapling) {
			t.Fatal("NSpendsSapling miscompare")
		}
		if len(tx.shieldedOutputs) != int(txtestdata.NoutputsSapling) {
			t.Fatal("NOutputsSapling miscompare")
		}
		if len(tx.orchardActions) != int(txtestdata.NActionsOrchard) {
			t.Fatal("NActionsOrchard miscompare")
		}
	}
}

func TestZip229V6TransactionParser(t *testing.T) {
	// header: fOverwintered | version 6
	// nVersionGroupId: 0xD884B698 (NU6.3)
	// nConsensusBranchId: 0x37A5165B (NU6.3)
	// lock_time, nExpiryHeight: 0
	// tx_in_count, tx_out_count: 0
	// nShieldedSpend, nShieldedOutput: 0
	// nActionsOrchard: 0
	// nActionsIronwood: 0
	rawTxData, err := hex.DecodeString("0600008098b684d85b16a5370000000000000000000000000000")
	if err != nil {
		t.Fatal(err)
	}

	tx := NewTransaction()
	rest, err := tx.ParseFromSlice(rawTxData)
	if err != nil {
		t.Fatalf("%v", err)
	}
	if len(rest) != 0 {
		t.Fatalf("Test did not consume entire buffer, %d remaining", len(rest))
	}
	if tx.version != ZIP229_TX_VERSION {
		t.Fatal("version miscompare")
	}
	if tx.nVersionGroupID != NU6_3_VERSION_GROUP_ID {
		t.Fatal("nVersionGroupId miscompare")
	}
	if tx.consensusBranchID != NU6_3_CONSENSUS_BRANCH_ID {
		t.Fatal("consensusBranchID miscompare")
	}
	if len(tx.transparentInputs) != 0 {
		t.Fatal("tx_in_count miscompare")
	}
	if len(tx.transparentOutputs) != 0 {
		t.Fatal("tx_out_count miscompare")
	}
	if len(tx.shieldedSpends) != 0 {
		t.Fatal("NSpendsSapling miscompare")
	}
	if len(tx.shieldedOutputs) != 0 {
		t.Fatal("NOutputsSapling miscompare")
	}
	if len(tx.orchardActions) != 0 {
		t.Fatal("NActionsOrchard miscompare")
	}
	if len(tx.ironwoodActions) != 0 {
		t.Fatal("NActionsIronwood miscompare")
	}
}

func TestZip229V6TransactionParserAcceptsOtherConsensusBranchID(t *testing.T) {
	// Same as above but with the consensus branch ID of some future
	// network upgrade (0x4C89A1F3). nConsensusBranchId identifies the
	// consensus epoch the transaction is mined in, not the epoch that
	// introduced the v6 format, so the parser must accept v6
	// transactions carrying branch IDs of upgrades after NU6.3.
	rawTxData, err := hex.DecodeString("0600008098b684d8f3a1894c0000000000000000000000000000")
	if err != nil {
		t.Fatal(err)
	}

	tx := NewTransaction()
	rest, err := tx.ParseFromSlice(rawTxData)
	if err != nil {
		t.Fatalf("%v", err)
	}
	if len(rest) != 0 {
		t.Fatalf("Test did not consume entire buffer, %d remaining", len(rest))
	}
	if tx.consensusBranchID != 0x4C89A1F3 {
		t.Fatal("consensusBranchID miscompare")
	}
}

func TestZip229V6TransactionParserKeepsIronwoodBundle(t *testing.T) {
	var raw bytes.Buffer
	raw.Write([]byte{
		0x06, 0x00, 0x00, 0x80, // fOverwintered | version 6
		0x98, 0xb6, 0x84, 0xd8, // version group ID (NU6.3)
		0x5b, 0x16, 0xa5, 0x37, // consensus branch ID (NU6.3)
		0x00, 0x00, 0x00, 0x00, // lock time
		0x00, 0x00, 0x00, 0x00, // expiry height
		0x00, // tx_in_count
		0x00, // tx_out_count
		0x00, // nShieldedSpend
		0x00, // nShieldedOutput
	})
	appendOrchardLikeBundle(&raw, 1) // Orchard bundle
	appendOrchardLikeBundle(&raw, 1) // Ironwood bundle
	raw.Write([]byte{0xaa, 0xbb})    // trailing bytes

	tx := NewTransaction()
	rest, err := tx.ParseFromSlice(raw.Bytes())
	if err != nil {
		t.Fatalf("%v", err)
	}
	if len(rest) != 2 {
		t.Fatalf("expected two trailing bytes, got %d", len(rest))
	}
	if !bytes.Equal(rest, []byte{0xaa, 0xbb}) {
		t.Fatal("trailing bytes miscompare")
	}
	if len(tx.orchardActions) != 1 {
		t.Fatal("NActionsOrchard miscompare")
	}
	if len(tx.ironwoodActions) != 1 {
		t.Fatal("NActionsIronwood miscompare")
	}
	compact := tx.ToCompact(0)
	if len(compact.Actions) != 1 {
		t.Fatal("compact orchard action count miscompare")
	}
	if len(compact.IronwoodActions) != 1 {
		t.Fatal("compact ironwood action count miscompare")
	}
	if len(tx.rawBytes) != raw.Len()-len(rest) {
		t.Fatal("raw transaction length miscompare")
	}
}

func appendOrchardLikeBundle(raw *bytes.Buffer, actionsCount int) {
	raw.WriteByte(byte(actionsCount))
	for i := 0; i < actionsCount; i++ {
		raw.Write(bytes.Repeat([]byte{byte(i + 1)}, 820))
	}
	if actionsCount > 0 {
		raw.WriteByte(0x00)                      // flags
		raw.Write(make([]byte, 8))               // value balance
		raw.Write(make([]byte, 32))              // anchor
		raw.WriteByte(0x00)                      // proofs length
		raw.Write(make([]byte, 64*actionsCount)) // spend auth signatures
		raw.Write(make([]byte, 64))              // binding signature
	}
}

func TestParseTransparentRejectsCountsThatCannotFit(t *testing.T) {
	tests := []struct {
		name    string
		data    []byte
		wantErr string
	}{
		{
			name:    "transparent inputs",
			data:    []byte{0x01},
			wantErr: "tx_in_count 1 requires at least 41 bytes, but only 0 remain",
		},
		{
			name:    "transparent outputs",
			data:    []byte{0x00, 0x01},
			wantErr: "tx_out_count 1 requires at least 9 bytes, but only 0 remain",
		},
		{
			// A count far larger than one, with a non-trivial amount of input
			// left, to exercise the division rather than the count==1 edge.
			name:    "many transparent outputs",
			data:    append([]byte{0x00, 0xfd, 0xe8, 0x03}, make([]byte, 100)...),
			wantErr: "tx_out_count 1000 requires at least 9000 bytes, but only 100 remain",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tx := NewTransaction()
			_, err := tx.ParseTransparent(tt.data)
			if err == nil {
				t.Fatal("expected error")
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("error mismatch:\nhave: %v\nwant substring: %s", err, tt.wantErr)
			}
		})
	}
}

// The bounds checks must never reject input that would otherwise have parsed.
// Feed each check a structure holding exactly its minimum-size elements, so a
// bound that was tightened by even one byte would fail here.
func TestBoundsChecksAcceptMinimallySizedElements(t *testing.T) {
	var raw bytes.Buffer
	raw.WriteByte(0x01)         // tx_in_count
	raw.Write(make([]byte, 32)) // prevout hash
	raw.Write(make([]byte, 4))  // prevout index
	raw.WriteByte(0x00)         // script length (empty script)
	raw.Write(make([]byte, 4))  // sequence
	raw.WriteByte(0x01)         // tx_out_count
	raw.Write(make([]byte, 8))  // value
	raw.WriteByte(0x00)         // script length (empty script)

	tx := NewTransaction()
	rest, err := tx.ParseTransparent(raw.Bytes())
	if err != nil {
		t.Fatalf("minimally sized transparent elements rejected: %v", err)
	}
	if len(rest) != 0 {
		t.Fatalf("did not consume entire buffer, %d remaining", len(rest))
	}
	if len(tx.transparentInputs) != 1 {
		t.Fatal("tx_in_count miscompare")
	}
	if len(tx.transparentOutputs) != 1 {
		t.Fatal("tx_out_count miscompare")
	}
}

func requireErrorContains(t *testing.T, err error, want string) {
	t.Helper()
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), want) {
		t.Fatalf("error mismatch:\nhave: %v\nwant substring: %s", err, want)
	}
}

func saplingV4Prefix() []byte {
	return []byte{
		0x00,                   // tx_in_count
		0x00,                   // tx_out_count
		0x00, 0x00, 0x00, 0x00, // nLockTime
		0x00, 0x00, 0x00, 0x00, // nExpiryHeight
		0x00, 0x00, 0x00, 0x00, // valueBalanceSapling (int64)
		0x00, 0x00, 0x00, 0x00,
	}
}

func TestParsePreV5RejectsCountsThatCannotFit(t *testing.T) {
	tests := []struct {
		name    string
		data    []byte
		wantErr string
	}{
		{
			name:    "sapling spends",
			data:    append(saplingV4Prefix(), 0x01),
			wantErr: "nShieldedSpend 1 requires at least 384 bytes, but only 0 remain",
		},
		{
			name:    "sapling outputs",
			data:    append(saplingV4Prefix(), 0x00, 0x01),
			wantErr: "nShieldedOutput 1 requires at least 948 bytes, but only 0 remain",
		},
		{
			name:    "join splits",
			data:    append(saplingV4Prefix(), 0x00, 0x00, 0x01),
			wantErr: "nJoinSplit 1 requires at least 1698 bytes, but only 0 remain",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tx := NewTransaction()
			tx.fOverwintered = true
			tx.version = SAPLING_TX_VERSION
			tx.nVersionGroupID = SAPLING_VERSION_GROUP_ID

			_, err := tx.parsePreV5(tt.data)
			requireErrorContains(t, err, tt.wantErr)
		})
	}
}

func zip225V5Prefix() []byte {
	return []byte{
		0x00, 0x00, 0x00, 0x00, // nConsensusBranchId
		0x00, 0x00, 0x00, 0x00, // nLockTime
		0x00, 0x00, 0x00, 0x00, // nExpiryHeight
		0x00, // tx_in_count
		0x00, // tx_out_count
	}
}

func TestParseV5RejectsCountsThatCannotFit(t *testing.T) {
	tests := []struct {
		name    string
		data    []byte
		wantErr string
	}{
		{
			name:    "sapling spends",
			data:    append(zip225V5Prefix(), 0x01),
			wantErr: "nShieldedSpend 1 requires at least 96 bytes, but only 0 remain",
		},
		{
			name:    "sapling outputs",
			data:    append(zip225V5Prefix(), 0x00, 0x01),
			wantErr: "nShieldedOutput 1 requires at least 756 bytes, but only 0 remain",
		},
		{
			name:    "orchard actions",
			data:    append(zip225V5Prefix(), 0x00, 0x00, 0x01),
			wantErr: "nActionsOrchard 1 requires at least 820 bytes, but only 0 remain",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tx := NewTransaction()
			tx.fOverwintered = true
			tx.version = ZIP225_TX_VERSION
			tx.nVersionGroupID = ZIP225_VERSION_GROUP_ID

			_, err := tx.parseV5(tt.data)
			requireErrorContains(t, err, tt.wantErr)
		})
	}
}

// The action bundle parser is shared between Orchard and Ironwood, so the
// bound must name whichever pool it was called for.
func TestParseOrchardActionShapeBundleNamesItsPool(t *testing.T) {
	for _, pool := range []string{"Orchard", "Ironwood"} {
		t.Run(pool, func(t *testing.T) {
			_, _, err := parseOrchardActionShapeBundle([]byte{0x01}, pool)
			requireErrorContains(t, err,
				"nActions"+pool+" 1 requires at least 820 bytes, but only 0 remain")
		})
	}
}
