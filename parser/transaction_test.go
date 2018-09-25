package parser

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"testing"

	"github.com/gtank/ctxd/parser/internal/bytestring"
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
			txInput := tx.transparentInputs[idx]

			testPrevTxHash, _ := hex.DecodeString(ti[0])
			if eq := bytes.Equal(testPrevTxHash, txInput.PrevTxHash); !eq {
				t.Errorf("Test %d tin %d: prevhash mismatch %x %x", i, idx, testPrevTxHash, txInput.PrevTxHash)
				continue
			}

			testPrevTxOutIndexBytes, _ := hex.DecodeString(ti[1])
			testPrevTxOutIndex := le.Uint32(testPrevTxOutIndexBytes)
			if testPrevTxOutIndex != txInput.PrevTxOutIndex {
				t.Errorf("Test %d tin %d: prevout index mismatch %d %d", i, idx, testPrevTxOutIndex, txInput.PrevTxOutIndex)
				continue
			}

			// Decode scriptSig and correctly consume own CompactSize field
			testScriptSig, _ := hex.DecodeString(ti[2])
			ok := (*bytestring.String)(&testScriptSig).ReadCompactLengthPrefixed((*bytestring.String)(&testScriptSig))
			if !ok {
				t.Errorf("Test %d, tin %d: couldn't strip size from script", i, idx)
				continue
			}

			if eq := bytes.Equal(testScriptSig, txInput.ScriptSig); !eq {
				t.Errorf("Test %d tin %d: scriptsig mismatch %x %x", i, idx, testScriptSig, txInput.ScriptSig)
				continue
			}

			testSeqNumBytes, _ := hex.DecodeString(ti[3])
			testSeqNum := le.Uint32(testSeqNumBytes)
			if testSeqNum != txInput.SequenceNumber {
				t.Errorf("Test %d tin %d: seq mismatch %d %d", i, idx, testSeqNum, txInput.SequenceNumber)
				continue
			}

		}

		if tt.vout == nil && tx.transparentOutputs != nil {
			t.Errorf("Test %d: non-zero vout when expected zero", i)
			continue
		}

		if len(tt.vout) != len(tx.transparentOutputs) {
			t.Errorf("Test %d: vout have mismatched lengths", i)
			continue
		}

		for idx, testOutput := range tt.vout {
			txOutput := tx.transparentOutputs[idx]

			// Parse tx out value from test
			testValueBytes, _ := hex.DecodeString(testOutput[0])
			testValue := le.Uint64(testValueBytes)

			if testValue != txOutput.Value {
				t.Errorf("Test %d, tout %d: value mismatch %d %d", i, idx, testValue, txOutput.Value)
				continue
			}

			// Parse script from test
			testScript, _ := hex.DecodeString(testOutput[1])
			// Correctly consume own CompactSize field
			ok := (*bytestring.String)(&testScript).ReadCompactLengthPrefixed((*bytestring.String)(&testScript))
			if !ok {
				t.Errorf("Test %d, tout %d: couldn't strip size from script", i, idx)
				continue
			}

			if !bytes.Equal(testScript, txOutput.Script) {
				t.Errorf("Test %d, tout %d: script mismatch %x %x", i, idx, testScript, txOutput.Script)
				continue
			}
		}
	}
}
