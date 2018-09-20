package parser

import (
	"encoding/binary"
	"encoding/hex"
	"testing"
)

// https://github.com/zcash/zips/blob/master/zip-0143.rst
var zip143tests = []struct {
	raw, header, nVersionGroupId, nLockTime, nExpiryHeight string
	vin, vout, vJoinSplits                                 [][]string
}{
	{
		raw:             "030000807082c40300028f739811893e0000095200ac6551ac636565b1a45a0805750200025151481cdd86b3cc431800",
		header:          "03000080",
		nVersionGroupId: "7082c403",
		nLockTime:       "481cdd86",
		nExpiryHeight:   "b3cc4318",
		vin:             nil,
		vout: [][]string{
			{"8f739811893e0000", "095200ac6551ac636565"},
			{"b1a45a0805750200", "025151"},
		},
		vJoinSplits: nil,
	},
}

func TestSproutTransactionParser(t *testing.T) {
	for i, tt := range zip143tests {
		txBytes, err := hex.DecodeString(tt.raw)
		if err != nil {
			t.Errorf("Couldn't decode test case %d", i)
			continue
		}

		tx := newTransaction()
		rest, err := tx.ParseFromSlice(txBytes)
		if err != nil {
			t.Errorf("Test %d: %v", i, err)
			continue
		}

		if len(rest) != 0 {
			t.Errorf("Test %d: did not consume entire buffer", i)
			continue
		}

		le := binary.LittleEndian

		headerBytes, _ := hex.DecodeString(tt.header)
		header := le.Uint32(headerBytes)
		if (header >> 31) == 1 != tx.fOverwintered {
			t.Errorf("Test %d: unexpected fOverwintered", i)
		}
		if (header & 0x7FFFFFFF) != tx.version {
			t.Errorf("Test %d: unexpected tx version", i)
			continue
		}

		versionGroupBytes, _ := hex.DecodeString(tt.nVersionGroupId)
		versionGroup := le.Uint32(versionGroupBytes)
		if versionGroup != tx.nVersionGroupId {
			t.Errorf("Test %d: unexpected versionGroupId", i)
			continue
		}

		lockTimeBytes, _ := hex.DecodeString(tt.nLockTime)
		lockTime := le.Uint32(lockTimeBytes)
		if lockTime != tx.nLockTime {
			t.Errorf("Test %d: unexpected nLockTime", i)
			continue
		}

		expiryHeightBytes, _ := hex.DecodeString(tt.nExpiryHeight)
		expiryHeight := le.Uint32(expiryHeightBytes)
		if expiryHeight != tx.nExpiryHeight {
			t.Errorf("Test %d: unexpected nExpiryHeight", i)
			continue
		}

		if tt.vin == nil && tx.transparentInputs != nil {
			t.Errorf("Test %d: non-zero vin when expected zero", i)
			continue
		}

		if len(tt.vin) != len(tx.transparentInputs) {
			t.Errorf("Test %d: vins have mismatched lengths", i)
			continue
		}

		// 4201cfb1cd8dbf69b8250c18ef41294ca97993db546c1fe01f7e9c8e36d6a5e2 9d4e30a7 03ac6a00 98421c69
		for idx, ti := range tt.vin {
			prevTxHash, _ := hex.DecodeString(ti[0])
			prevTxIndexBytes, _ := hex.DecodeString(ti[1])
			prevTxIndex := le.Uint32(prevTxIndexBytes)
			scriptSig, _ := hex.DecodeString(ti[2])
			seqNumBytes, _ := hex.DecodeString(ti[3])
			seqNum := le.Uint32(seqNumBytes)

			if !bytes.Equal(prevTxHash, tx.transparentInputs[idx][0]) {

			}
		}
	}
}
