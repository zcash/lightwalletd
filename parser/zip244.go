// Copyright (c) 2025 The Zcash developers
// Distributed under the MIT software license, see the accompanying
// file COPYING or https://www.opensource.org/licenses/mit-license.php .

package parser

import (
	"encoding/binary"
	"errors"
	"fmt"
	"hash"

	"github.com/zcash/lightwalletd/hash32"
	"github.com/zcash/lightwalletd/parser/internal/blake2b"
	"github.com/zcash/lightwalletd/parser/internal/bytestring"
)

// personalization converts a 16-byte string to a [16]byte personalization parameter.
func personalization(s string) [16]byte {
	if len(s) != 16 {
		panic(fmt.Sprintf("personalization string must be exactly 16 bytes, got %d", len(s)))
	}
	var p [16]byte
	copy(p[:], s)
	return p
}

func txidPersonalization(consensusBranchID uint32) [16]byte {
	var p [16]byte
	copy(p[:12], "ZcashTxHash_")
	binary.LittleEndian.PutUint32(p[12:], consensusBranchID)
	return p
}

func sumDigest(h hash.Hash) [32]byte {
	var d [32]byte
	copy(d[:], h.Sum(nil))
	return d
}

// writeCompactSize writes a Bitcoin-style CompactSize encoding to a hash.
func writeCompactSize(h hash.Hash, n int) {
	if n < 253 {
		h.Write([]byte{byte(n)})
	} else if n < 0x10000 {
		var buf [3]byte
		buf[0] = 0xfd
		binary.LittleEndian.PutUint16(buf[1:], uint16(n))
		h.Write(buf[:])
	} else {
		var buf [5]byte
		buf[0] = 0xfe
		binary.LittleEndian.PutUint32(buf[1:], uint32(n))
		h.Write(buf[:])
	}
}

// computeV5TxID computes the transaction ID for a v5 transaction per ZIP 244.
// It re-parses rawBytes to extract the fields needed for the hash tree.
//
// ZIP 244 txid_digest tree:
//   txid = H("ZcashTxHash_"||branchID,
//     header_digest || transparent_digest || sapling_digest || orchard_digest)
//
// Proofs and signatures are excluded from the txid per the spec, so the
// remaining bytes after readAndHashOrchard are intentionally not consumed.
func computeV5TxID(rawBytes []byte) (hash32.T, error) {
	s := bytestring.String(rawBytes)

	headerDigest, consensusBranchID, err := readAndHashHeader(&s)
	if err != nil {
		return hash32.T{}, fmt.Errorf("header: %w", err)
	}

	transparentDigest, err := readAndHashTransparent(&s)
	if err != nil {
		return hash32.T{}, fmt.Errorf("transparent: %w", err)
	}

	saplingDigest, spendCount, outputCount, err := readAndHashSapling(&s)
	if err != nil {
		return hash32.T{}, fmt.Errorf("sapling: %w", err)
	}

	if err := skipSaplingProofsAndSigs(&s, spendCount, outputCount); err != nil {
		return hash32.T{}, fmt.Errorf("sapling proofs: %w", err)
	}

	orchardDigest, err := readAndHashOrchard(&s)
	if err != nil {
		return hash32.T{}, fmt.Errorf("orchard: %w", err)
	}

	// Remaining bytes (orchard proofs, auth sigs, binding sig) are
	// intentionally not consumed -- they are excluded from the txid.

	h := blake2b.New256Personalized(txidPersonalization(consensusBranchID))
	h.Write(headerDigest[:])
	h.Write(transparentDigest[:])
	h.Write(saplingDigest[:])
	h.Write(orchardDigest[:])

	return hash32.FromSlice(h.Sum(nil)), nil
}

// readAndHashHeader reads the 20-byte v5 header and returns the header digest
// and consensus branch ID.
func readAndHashHeader(s *bytestring.String) ([32]byte, uint32, error) {
	// header(4) + nVersionGroupId(4) + consensusBranchId(4) + nLockTime(4) + nExpiryHeight(4)
	var headerBytes []byte
	if !s.ReadBytes(&headerBytes, 20) {
		return [32]byte{}, 0, errors.New("could not read header fields")
	}
	consensusBranchID := binary.LittleEndian.Uint32(headerBytes[8:12])
	digest := blake2b.Sum256Personalized(personalization("ZTxIdHeadersHash"), headerBytes)
	return digest, consensusBranchID, nil
}

// readAndHashTransparent parses transparent inputs/outputs and returns
// the transparent digest.
func readAndHashTransparent(s *bytestring.String) ([32]byte, error) {
	var txInCount int
	if !s.ReadCompactSize(&txInCount) {
		return [32]byte{}, errors.New("could not read tx_in_count")
	}

	prevoutsHasher := blake2b.New256Personalized(personalization("ZTxIdPrevoutHash"))
	sequenceHasher := blake2b.New256Personalized(personalization("ZTxIdSequencHash"))

	for i := 0; i < txInCount; i++ {
		// prevout: PrevTxHash(32) + PrevTxOutIndex(4)
		var prevout []byte
		if !s.ReadBytes(&prevout, 36) {
			return [32]byte{}, fmt.Errorf("could not read input %d prevout", i)
		}
		prevoutsHasher.Write(prevout)

		if !s.SkipCompactLengthPrefixed() {
			return [32]byte{}, fmt.Errorf("could not skip input %d scriptSig", i)
		}

		var seq []byte
		if !s.ReadBytes(&seq, 4) {
			return [32]byte{}, fmt.Errorf("could not read input %d sequence", i)
		}
		sequenceHasher.Write(seq)
	}

	var txOutCount int
	if !s.ReadCompactSize(&txOutCount) {
		return [32]byte{}, errors.New("could not read tx_out_count")
	}

	outputsHasher := blake2b.New256Personalized(personalization("ZTxIdOutputsHash"))

	for i := 0; i < txOutCount; i++ {
		var value []byte
		if !s.ReadBytes(&value, 8) {
			return [32]byte{}, fmt.Errorf("could not read output %d value", i)
		}
		outputsHasher.Write(value)

		var scriptLen int
		if !s.ReadCompactSize(&scriptLen) {
			return [32]byte{}, fmt.Errorf("could not read output %d script length", i)
		}
		writeCompactSize(outputsHasher, scriptLen)

		var script []byte
		if !s.ReadBytes(&script, scriptLen) {
			return [32]byte{}, fmt.Errorf("could not read output %d script", i)
		}
		outputsHasher.Write(script)
	}

	if txInCount == 0 && txOutCount == 0 {
		return blake2b.Sum256Personalized(personalization("ZTxIdTranspaHash"), nil), nil
	}

	h := blake2b.New256Personalized(personalization("ZTxIdTranspaHash"))
	prevoutsDigest := sumDigest(prevoutsHasher)
	sequenceDigest := sumDigest(sequenceHasher)
	outputsDigest := sumDigest(outputsHasher)
	h.Write(prevoutsDigest[:])
	h.Write(sequenceDigest[:])
	h.Write(outputsDigest[:])
	return sumDigest(h), nil
}

// readAndHashSapling parses the sapling spend/output descriptions plus
// valueBalance and anchor, and returns the sapling digest along with counts
// needed by skipSaplingProofsAndSigs.
func readAndHashSapling(s *bytestring.String) (digest [32]byte, spendCount, outputCount int, err error) {
	if !s.ReadCompactSize(&spendCount) {
		err = errors.New("could not read spend count")
		return
	}

	// Parse spend descriptions: cv(32) + nullifier(32) + rk(32) per spend.
	// Hash nullifiers to compact digest incrementally.
	// Buffer cv and rk for noncompact digest (need shared anchor later).
	var compactHasher hash.Hash
	var spendCvRk []byte

	if spendCount > 0 {
		compactHasher = blake2b.New256Personalized(personalization("ZTxIdSSpendCHash"))
		spendCvRk = make([]byte, 0, spendCount*64)
	}

	for i := 0; i < spendCount; i++ {
		var cv, nullifier, rk []byte
		if !s.ReadBytes(&cv, 32) {
			err = fmt.Errorf("could not read spend %d cv", i)
			return
		}
		if !s.ReadBytes(&nullifier, 32) {
			err = fmt.Errorf("could not read spend %d nullifier", i)
			return
		}
		if !s.ReadBytes(&rk, 32) {
			err = fmt.Errorf("could not read spend %d rk", i)
			return
		}
		compactHasher.Write(nullifier)
		spendCvRk = append(spendCvRk, cv...)
		spendCvRk = append(spendCvRk, rk...)
	}

	if !s.ReadCompactSize(&outputCount) {
		err = errors.New("could not read output count")
		return
	}

	// Parse output descriptions incrementally:
	// cv(32) + cmu(32) + ephemeralKey(32) + encCiphertext(580) + outCiphertext(80)
	var outCompactHasher, outMemosHasher, outNoncompactHasher hash.Hash

	if outputCount > 0 {
		outCompactHasher = blake2b.New256Personalized(personalization("ZTxIdSOutC__Hash"))
		outMemosHasher = blake2b.New256Personalized(personalization("ZTxIdSOutM__Hash"))
		outNoncompactHasher = blake2b.New256Personalized(personalization("ZTxIdSOutN__Hash"))
	}

	for i := 0; i < outputCount; i++ {
		var cv, cmu, ephemeralKey, encCiphertext, outCiphertext []byte
		if !s.ReadBytes(&cv, 32) {
			err = fmt.Errorf("could not read output %d cv", i)
			return
		}
		if !s.ReadBytes(&cmu, 32) {
			err = fmt.Errorf("could not read output %d cmu", i)
			return
		}
		if !s.ReadBytes(&ephemeralKey, 32) {
			err = fmt.Errorf("could not read output %d ephemeralKey", i)
			return
		}
		if !s.ReadBytes(&encCiphertext, 580) {
			err = fmt.Errorf("could not read output %d encCiphertext", i)
			return
		}
		if !s.ReadBytes(&outCiphertext, 80) {
			err = fmt.Errorf("could not read output %d outCiphertext", i)
			return
		}

		outCompactHasher.Write(cmu)
		outCompactHasher.Write(ephemeralKey)
		outCompactHasher.Write(encCiphertext[:52])

		outMemosHasher.Write(encCiphertext[52:564])

		outNoncompactHasher.Write(cv)
		outNoncompactHasher.Write(encCiphertext[564:])
		outNoncompactHasher.Write(outCiphertext)
	}

	// Read valueBalance and anchor (after all spend/output descriptions).
	var valueBalance, anchor []byte
	if spendCount+outputCount > 0 {
		if !s.ReadBytes(&valueBalance, 8) {
			err = errors.New("could not read valueBalanceSapling")
			return
		}
	}
	if spendCount > 0 {
		if !s.ReadBytes(&anchor, 32) {
			err = errors.New("could not read anchorSapling")
			return
		}
	}

	// Empty sapling section.
	if spendCount+outputCount == 0 {
		digest = blake2b.Sum256Personalized(personalization("ZTxIdSaplingHash"), nil)
		return
	}

	// Compute spends sub-digest.
	var spendsDigest [32]byte
	if spendCount == 0 {
		spendsDigest = blake2b.Sum256Personalized(personalization("ZTxIdSSpendsHash"), nil)
	} else {
		compactDigest := sumDigest(compactHasher)

		noncompactHasher := blake2b.New256Personalized(personalization("ZTxIdSSpendNHash"))
		for i := 0; i < spendCount; i++ {
			off := i * 64
			noncompactHasher.Write(spendCvRk[off : off+32]) // cv
			noncompactHasher.Write(anchor)                    // shared anchor
			noncompactHasher.Write(spendCvRk[off+32 : off+64]) // rk
		}
		noncompactDigest := sumDigest(noncompactHasher)

		h := blake2b.New256Personalized(personalization("ZTxIdSSpendsHash"))
		h.Write(compactDigest[:])
		h.Write(noncompactDigest[:])
		spendsDigest = sumDigest(h)
	}

	// Compute outputs sub-digest.
	var outputsDigest [32]byte
	if outputCount == 0 {
		outputsDigest = blake2b.Sum256Personalized(personalization("ZTxIdSOutputHash"), nil)
	} else {
		compactDigest := sumDigest(outCompactHasher)
		memosDigest := sumDigest(outMemosHasher)
		noncompactDigest := sumDigest(outNoncompactHasher)

		h := blake2b.New256Personalized(personalization("ZTxIdSOutputHash"))
		h.Write(compactDigest[:])
		h.Write(memosDigest[:])
		h.Write(noncompactDigest[:])
		outputsDigest = sumDigest(h)
	}

	// Combine into sapling digest.
	saplingHasher := blake2b.New256Personalized(personalization("ZTxIdSaplingHash"))
	saplingHasher.Write(spendsDigest[:])
	saplingHasher.Write(outputsDigest[:])
	saplingHasher.Write(valueBalance)
	digest = sumDigest(saplingHasher)
	return
}

func skipSaplingProofsAndSigs(s *bytestring.String, spendCount, outputCount int) error {
	if !s.Skip(192 * spendCount) {
		return errors.New("could not skip vSpendProofsSapling")
	}
	if !s.Skip(64 * spendCount) {
		return errors.New("could not skip vSpendAuthSigsSapling")
	}
	if !s.Skip(192 * outputCount) {
		return errors.New("could not skip vOutputProofsSapling")
	}
	if spendCount+outputCount > 0 && !s.Skip(64) {
		return errors.New("could not skip bindingSigSapling")
	}
	return nil
}

// readAndHashOrchard parses orchard actions and metadata, returning the
// orchard digest.
func readAndHashOrchard(s *bytestring.String) ([32]byte, error) {
	var actionsCount int
	if !s.ReadCompactSize(&actionsCount) {
		return [32]byte{}, errors.New("could not read orchard actions count")
	}

	if actionsCount == 0 {
		return blake2b.Sum256Personalized(personalization("ZTxIdOrchardHash"), nil), nil
	}

	compactHasher := blake2b.New256Personalized(personalization("ZTxIdOrcActCHash"))
	memosHasher := blake2b.New256Personalized(personalization("ZTxIdOrcActMHash"))
	noncompactHasher := blake2b.New256Personalized(personalization("ZTxIdOrcActNHash"))

	for i := 0; i < actionsCount; i++ {
		// cv(32) + nullifier(32) + rk(32) + cmx(32) + ephemeralKey(32) + encCiphertext(580) + outCiphertext(80)
		var cv, nullifier, rk, cmx, ephemeralKey, encCiphertext, outCiphertext []byte
		if !s.ReadBytes(&cv, 32) {
			return [32]byte{}, fmt.Errorf("could not read action %d cv", i)
		}
		if !s.ReadBytes(&nullifier, 32) {
			return [32]byte{}, fmt.Errorf("could not read action %d nullifier", i)
		}
		if !s.ReadBytes(&rk, 32) {
			return [32]byte{}, fmt.Errorf("could not read action %d rk", i)
		}
		if !s.ReadBytes(&cmx, 32) {
			return [32]byte{}, fmt.Errorf("could not read action %d cmx", i)
		}
		if !s.ReadBytes(&ephemeralKey, 32) {
			return [32]byte{}, fmt.Errorf("could not read action %d ephemeralKey", i)
		}
		if !s.ReadBytes(&encCiphertext, 580) {
			return [32]byte{}, fmt.Errorf("could not read action %d encCiphertext", i)
		}
		if !s.ReadBytes(&outCiphertext, 80) {
			return [32]byte{}, fmt.Errorf("could not read action %d outCiphertext", i)
		}

		compactHasher.Write(nullifier)
		compactHasher.Write(cmx)
		compactHasher.Write(ephemeralKey)
		compactHasher.Write(encCiphertext[:52])

		memosHasher.Write(encCiphertext[52:564])

		noncompactHasher.Write(cv)
		noncompactHasher.Write(rk)
		noncompactHasher.Write(encCiphertext[564:])
		noncompactHasher.Write(outCiphertext)
	}

	// flags(1) + valueBalance(8) + anchor(32)
	var flags, valueBalance, anchor []byte
	if !s.ReadBytes(&flags, 1) {
		return [32]byte{}, errors.New("could not read flagsOrchard")
	}
	if !s.ReadBytes(&valueBalance, 8) {
		return [32]byte{}, errors.New("could not read valueBalanceOrchard")
	}
	if !s.ReadBytes(&anchor, 32) {
		return [32]byte{}, errors.New("could not read anchorOrchard")
	}

	compactDigest := sumDigest(compactHasher)
	memosDigest := sumDigest(memosHasher)
	noncompactDigest := sumDigest(noncompactHasher)

	h := blake2b.New256Personalized(personalization("ZTxIdOrchardHash"))
	h.Write(compactDigest[:])
	h.Write(memosDigest[:])
	h.Write(noncompactDigest[:])
	h.Write(flags)
	h.Write(valueBalance)
	h.Write(anchor)
	return sumDigest(h), nil
}
