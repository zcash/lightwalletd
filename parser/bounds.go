// Copyright (c) 2019-present The Zcash developers
// Distributed under the MIT software license, see the accompanying
// file COPYING or https://www.opensource.org/licenses/mit-license.php .

package parser

import "fmt"

// A CompactSize count is only bounded by maxCompactSize (2^25), so a malformed
// or truncated serialization can name far more elements than the input could
// possibly contain. Sizing a slice from such a count allocates gigabytes before
// the first element fails to parse. The constants below give the smallest
// number of bytes each element can occupy on the wire, so a count can be
// rejected against the remaining input before anything is allocated.
//
// Every constant here is the number of bytes consumed by one iteration of the
// loop that parses these elements. Where an element also has per-element bytes
// in a later section of the serialization (v5 Sapling spends carry a 192-byte
// proof and a 64-byte signature in trailing vectors, for example), those are
// deliberately not counted. That makes the bounds looser than the true minimum,
// which is the safe direction: the check must never reject input that would
// have parsed. Do not "tighten" these to the full per-element size.
const (
	// A v1 transaction: 4-byte header, tx_in_count, tx_out_count, nLockTime.
	minTransactionWireBytes = 10

	minTxInWireBytes  = 41 // 32-byte prevout hash + 4-byte index + 1-byte script length + 4-byte sequence
	minTxOutWireBytes = 9  // 8-byte value + 1-byte script length

	minSaplingV4SpendBytes  = 384 // cv + anchor + nullifier + rk + zkproof + spendAuthSig
	minSaplingV4OutputBytes = 948 // cv + cmu + ephemeralKey + encCiphertext + outCiphertext + zkproof

	minSaplingV5SpendBytes  = 96  // cv + nullifier + rk
	minSaplingV5OutputBytes = 756 // cv + cmu + ephemeralKey + encCiphertext + outCiphertext

	minOrchardActionBytes = 820 // cv + nullifier + rk + cmx + ephemeralKey + encCiphertext + outCiphertext

	// vpubOld + vpubNew + anchor + nullifiers + commitments + ephemeralKey +
	// randomSeed + vmacs + proof + encCiphertexts. See section 5.4.10.2 of the
	// Zcash protocol spec; only the proof size differs between the two.
	minJoinSplitGrothBytes = 1698
	minJoinSplitPHGRBytes  = 1802
)

// rejectCountExceedingRemaining reports an error if count elements of at least
// minElementBytes each cannot fit in remaining bytes of input. label names the
// count field for the error message. minElementBytes must be positive.
func rejectCountExceedingRemaining(label string, count int, remaining int, minElementBytes int) error {
	if minElementBytes < 1 {
		// Programming error; a zero would panic on the division below.
		return fmt.Errorf("invalid minimum element size %d for %s", minElementBytes, label)
	}
	// Divide rather than computing count*minElementBytes, which can overflow a
	// 32-bit int (maxCompactSize is 2^25). The truncating division only ever
	// makes the bound looser, never tighter.
	if count > remaining/minElementBytes {
		return fmt.Errorf("%s %d requires at least %d bytes, but only %d remain",
			label, count, int64(count)*int64(minElementBytes), remaining)
	}
	return nil
}

func minJoinSplitWireBytes(isGroth16Proof bool) int {
	if isGroth16Proof {
		return minJoinSplitGrothBytes
	}
	return minJoinSplitPHGRBytes
}
