// Copyright (c) 2019-2020 The Zcash developers
// Distributed under the MIT software license, see the accompanying
// file COPYING or https://www.opensource.org/licenses/mit-license.php .

// Package parser deserializes (full) transactions (zcashd).
package parser

import (
	"crypto/sha256"
	"fmt"

	"github.com/pkg/errors"
	"github.com/zcash/lightwalletd/parser/internal/bytestring"
	"github.com/zcash/lightwalletd/walletrpc"
)

type rawTransaction struct {
	fOverwintered       bool
	version             uint32
	nVersionGroupID     uint32
	consensusBranchID   uint32
	transparentInputs   []*txIn
	transparentOutputs  []*txOut
	nLockTime           uint32
	nExpiryHeight       uint32
	valueBalanceSapling int64
	shieldedSpends      []*spend
	shieldedOutputs     []*output
	joinSplits          []*joinSplit
	joinSplitPubKey     []byte
	joinSplitSig        []byte
	bindingSigSapling   []byte
}

// Txin format as described in https://en.bitcoin.it/wiki/Transaction
type txIn struct {
	// SHA256d of a previous (to-be-used) transaction
	PrevTxHash []byte

	// Index of the to-be-used output in the previous tx
	PrevTxOutIndex uint32

	// CompactSize-prefixed, could be a pubkey or a script
	ScriptSig []byte

	// Bitcoin: "normally 0xFFFFFFFF; irrelevant unless transaction's lock_time > 0"
	SequenceNumber uint32
}

func (tx *txIn) ParseFromSlice(data []byte) ([]byte, error) {
	s := bytestring.String(data)

	if !s.ReadBytes(&tx.PrevTxHash, 32) {
		return nil, errors.New("could not read PrevTxHash")
	}

	if !s.ReadUint32(&tx.PrevTxOutIndex) {
		return nil, errors.New("could not read PrevTxOutIndex")
	}

	if !s.ReadCompactLengthPrefixed((*bytestring.String)(&tx.ScriptSig)) {
		return nil, errors.New("could not read ScriptSig")
	}

	if !s.ReadUint32(&tx.SequenceNumber) {
		return nil, errors.New("could not read SequenceNumber")
	}

	return []byte(s), nil
}

// Txout format as described in https://en.bitcoin.it/wiki/Transaction
type txOut struct {
	// Non-negative int giving the number of zatoshis to be transferred
	Value uint64

	// Script. CompactSize-prefixed.
	Script []byte
}

func (tx *txOut) ParseFromSlice(data []byte) ([]byte, error) {
	s := bytestring.String(data)

	if !s.ReadUint64(&tx.Value) {
		return nil, errors.New("could not read txOut value")
	}

	if !s.ReadCompactLengthPrefixed((*bytestring.String)(&tx.Script)) {
		return nil, errors.New("could not read txOut script")
	}

	return []byte(s), nil
}

// parse the transparent parts of the transaction
func (tx *Transaction) ParseTransparent(data []byte) ([]byte, error) {
	s := bytestring.String(data)
	var txInCount int
	if !s.ReadCompactSize(&txInCount) {
		return nil, errors.New("could not read tx_in_count")
	}
	var err error
	// TODO: Duplicate/otherwise-too-many transactions are a possible DoS
	// TODO: vector. At the moment we're assuming trusted input.
	// See https://nvd.nist.gov/vuln/detail/CVE-2018-17144 for an example.
	tx.transparentInputs = make([]*txIn, txInCount)
	for i := 0; i < txInCount; i++ {
		ti := &txIn{}
		s, err = ti.ParseFromSlice([]byte(s))
		if err != nil {
			return nil, errors.Wrap(err, "while parsing transparent input")
		}
		tx.transparentInputs[i] = ti
	}

	var txOutCount int
	if !s.ReadCompactSize(&txOutCount) {
		return nil, errors.New("could not read tx_out_count")
	}
	tx.transparentOutputs = make([]*txOut, txOutCount)
	for i := 0; i < txOutCount; i++ {
		to := &txOut{}
		s, err = to.ParseFromSlice([]byte(s))
		if err != nil {
			return nil, errors.Wrap(err, "while parsing transparent output")
		}
		tx.transparentOutputs[i] = to
	}
	return []byte(s), nil
}

// spend is a Sapling Spend Description as described in 7.3 of the Zcash
// protocol specification.
type spend struct {
	cv           []byte // 32
	anchor       []byte // 32
	nullifier    []byte // 32
	rk           []byte // 32
	zkproof      []byte // 192
	spendAuthSig []byte // 64
}

func (p *spend) ParseFromSlice(data []byte, version uint32) ([]byte, error) {
	s := bytestring.String(data)

	if !s.ReadBytes(&p.cv, 32) {
		return nil, errors.New("could not read cv")
	}

	if version <= 4 && !s.ReadBytes(&p.anchor, 32) {
		return nil, errors.New("could not read anchor")
	}

	if !s.ReadBytes(&p.nullifier, 32) {
		return nil, errors.New("could not read nullifier")
	}

	if !s.ReadBytes(&p.rk, 32) {
		return nil, errors.New("could not read rk")
	}

	if version <= 4 && !s.ReadBytes(&p.zkproof, 192) {
		return nil, errors.New("could not read zkproof")
	}

	if version <= 4 && !s.ReadBytes(&p.spendAuthSig, 64) {
		return nil, errors.New("could not read spendAuthSig")
	}

	return []byte(s), nil
}

func (p *spend) ToCompact() *walletrpc.CompactSpend {
	return &walletrpc.CompactSpend{
		Nf: p.nullifier,
	}
}

// output is a Sapling Output Description as described in section 7.4 of the
// Zcash protocol spec.
type output struct {
	cv            []byte // 32
	cmu           []byte // 32
	ephemeralKey  []byte // 32
	encCiphertext []byte // 580
	outCiphertext []byte // 80
	zkproof       []byte // 192
}

func (p *output) ParseFromSlice(data []byte, version uint32) ([]byte, error) {
	s := bytestring.String(data)

	if !s.ReadBytes(&p.cv, 32) {
		return nil, errors.New("could not read cv")
	}

	if !s.ReadBytes(&p.cmu, 32) {
		return nil, errors.New("could not read cmu")
	}

	if !s.ReadBytes(&p.ephemeralKey, 32) {
		return nil, errors.New("could not read ephemeralKey")
	}

	if !s.ReadBytes(&p.encCiphertext, 580) {
		return nil, errors.New("could not read encCiphertext")
	}

	if !s.ReadBytes(&p.outCiphertext, 80) {
		return nil, errors.New("could not read outCiphertext")
	}

	if version <= 4 && !s.ReadBytes(&p.zkproof, 192) {
		return nil, errors.New("could not read zkproof")
	}

	return []byte(s), nil
}

func (p *output) ToCompact() *walletrpc.CompactOutput {
	return &walletrpc.CompactOutput{
		Cmu:        p.cmu,
		Epk:        p.ephemeralKey,
		Ciphertext: p.encCiphertext[:52],
	}
}

// joinSplit is a JoinSplit description as described in 7.2 of the Zcash
// protocol spec. Its exact contents differ by transaction version and network
// upgrade level. Only version 4 is supported, no need for proofPHGR13.
type joinSplit struct {
	vpubOld        uint64
	vpubNew        uint64
	anchor         []byte    // 32
	nullifiers     [2][]byte // 64 [N_old][32]byte
	commitments    [2][]byte // 64 [N_new][32]byte
	ephemeralKey   []byte    // 32
	randomSeed     []byte    // 32
	vmacs          [2][]byte // 64 [N_old][32]byte
	proofGroth16   []byte    // 192 (version 4 only)
	encCiphertexts [2][]byte // 1202 [N_new][601]byte
}

func (p *joinSplit) ParseFromSlice(data []byte) ([]byte, error) {
	s := bytestring.String(data)

	if !s.ReadUint64(&p.vpubOld) {
		return nil, errors.New("could not read vpubOld")
	}

	if !s.ReadUint64(&p.vpubNew) {
		return nil, errors.New("could not read vpubNew")
	}

	if !s.ReadBytes(&p.anchor, 32) {
		return nil, errors.New("could not read anchor")
	}

	for i := 0; i < 2; i++ {
		if !s.ReadBytes(&p.nullifiers[i], 32) {
			return nil, errors.New("could not read a nullifier")
		}
	}

	for i := 0; i < 2; i++ {
		if !s.ReadBytes(&p.commitments[i], 32) {
			return nil, errors.New("could not read a commitment")
		}
	}

	if !s.ReadBytes(&p.ephemeralKey, 32) {
		return nil, errors.New("could not read ephemeralKey")
	}

	if !s.ReadBytes(&p.randomSeed, 32) {
		return nil, errors.New("could not read randomSeed")
	}

	for i := 0; i < 2; i++ {
		if !s.ReadBytes(&p.vmacs[i], 32) {
			return nil, errors.New("could not read a vmac")
		}
	}

	if !s.ReadBytes(&p.proofGroth16, 192) {
		return nil, errors.New("could not read Groth16 proof")
	}

	for i := 0; i < 2; i++ {
		if !s.ReadBytes(&p.encCiphertexts[i], 601) {
			return nil, errors.New("could not read an encCiphertext")
		}
	}

	return []byte(s), nil
}

// Transaction encodes a full (zcashd) transaction.
type Transaction struct {
	*rawTransaction
	rawBytes   []byte
	cachedTxID []byte // cached for performance
}

// GetDisplayHash returns the transaction hash in big-endian display order.
func (tx *Transaction) GetDisplayHash() []byte {
	if tx.cachedTxID != nil {
		return tx.cachedTxID
	}

	// SHA256d
	digest := sha256.Sum256(tx.rawBytes)
	digest = sha256.Sum256(digest[:])
	// Convert to big-endian
	tx.cachedTxID = Reverse(digest[:])
	return tx.cachedTxID
}

// GetEncodableHash returns the transaction hash in little-endian wire format order.
func (tx *Transaction) GetEncodableHash() []byte {
	digest := sha256.Sum256(tx.rawBytes)
	digest = sha256.Sum256(digest[:])
	return digest[:]
}

// Bytes returns a full transaction's raw bytes.
func (tx *Transaction) Bytes() []byte {
	return tx.rawBytes
}

// HasSaplingElements indicates whether a transaction has
// at least one shielded input or output.
func (tx *Transaction) HasSaplingElements() bool {
	return tx.version >= 4 && (len(tx.shieldedSpends)+len(tx.shieldedOutputs)) > 0
}

// ToCompact converts the given (full) transaction to compact format.
func (tx *Transaction) ToCompact(index int) *walletrpc.CompactTx {
	ctx := &walletrpc.CompactTx{
		Index: uint64(index), // index is contextual
		Hash:  tx.GetEncodableHash(),
		//Fee:     0, // TODO: calculate fees
		Spends:  make([]*walletrpc.CompactSpend, len(tx.shieldedSpends)),
		Outputs: make([]*walletrpc.CompactOutput, len(tx.shieldedOutputs)),
	}
	for i, spend := range tx.shieldedSpends {
		ctx.Spends[i] = spend.ToCompact()
	}
	for i, output := range tx.shieldedOutputs {
		ctx.Outputs[i] = output.ToCompact()
	}
	return ctx
}

// parse version 4 transaction data after the nVersionGroupId field.
func (tx *Transaction) parseV4(data []byte) ([]byte, error) {
	s := bytestring.String(data)
	var err error
	if tx.nVersionGroupID != 0x892F2085 {
		return nil, errors.New(fmt.Sprintf("version group ID %x must be 0x892F2085", tx.nVersionGroupID))
	}
	s, err = tx.ParseTransparent([]byte(s))
	if err != nil {
		return nil, err
	}
	if !s.ReadUint32(&tx.nLockTime) {
		return nil, errors.New("could not read nLockTime")
	}

	if !s.ReadUint32(&tx.nExpiryHeight) {
		return nil, errors.New("could not read nExpiryHeight")
	}

	var spendCount, outputCount int

	if !s.ReadInt64(&tx.valueBalanceSapling) {
		return nil, errors.New("could not read valueBalance")
	}
	if !s.ReadCompactSize(&spendCount) {
		return nil, errors.New("could not read nShieldedSpend")
	}
	tx.shieldedSpends = make([]*spend, spendCount)
	for i := 0; i < spendCount; i++ {
		newSpend := &spend{}
		s, err = newSpend.ParseFromSlice([]byte(s), 4)
		if err != nil {
			return nil, errors.Wrap(err, "while parsing shielded Spend")
		}
		tx.shieldedSpends[i] = newSpend
	}
	if !s.ReadCompactSize(&outputCount) {
		return nil, errors.New("could not read nShieldedOutput")
	}
	tx.shieldedOutputs = make([]*output, outputCount)
	for i := 0; i < outputCount; i++ {
		newOutput := &output{}
		s, err = newOutput.ParseFromSlice([]byte(s), 4)
		if err != nil {
			return nil, errors.Wrap(err, "while parsing shielded Output")
		}
		tx.shieldedOutputs[i] = newOutput
	}
	var joinSplitCount int
	if !s.ReadCompactSize(&joinSplitCount) {
		return nil, errors.New("could not read nJoinSplit")
	}

	tx.joinSplits = make([]*joinSplit, joinSplitCount)
	if joinSplitCount > 0 {
		for i := 0; i < joinSplitCount; i++ {
			js := &joinSplit{}
			s, err = js.ParseFromSlice([]byte(s))
			if err != nil {
				return nil, errors.Wrap(err, "while parsing JoinSplit")
			}
			tx.joinSplits[i] = js
		}

		if !s.ReadBytes(&tx.joinSplitPubKey, 32) {
			return nil, errors.New("could not read joinSplitPubKey")
		}

		if !s.ReadBytes(&tx.joinSplitSig, 64) {
			return nil, errors.New("could not read joinSplitSig")
		}
	}
	if spendCount+outputCount > 0 && !s.ReadBytes(&tx.bindingSigSapling, 64) {
		return nil, errors.New("could not read bindingSigSapling")
	}
	return s, nil
}

// parse version 5 transaction data after the nVersionGroupId field.
func (tx *Transaction) parseV5(data []byte) ([]byte, error) {
	s := bytestring.String(data)
	var err error
	if !s.ReadUint32(&tx.consensusBranchID) {
		return nil, errors.New("could not read nVersionGroupId")
	}
	if tx.nVersionGroupID != 0x26A7270A {
		return nil, errors.New(fmt.Sprintf("version group ID %d must be 0x26A7270A", tx.nVersionGroupID))
	}
	if tx.consensusBranchID != 0x37519621 {
		return nil, errors.New("unknown consensusBranchID")
	}
	if !s.ReadUint32(&tx.nLockTime) {
		return nil, errors.New("could not read nLockTime")
	}
	if !s.ReadUint32(&tx.nExpiryHeight) {
		return nil, errors.New("could not read nExpiryHeight")
	}
	s, err = tx.ParseTransparent([]byte(s))
	if err != nil {
		return nil, err
	}

	var spendCount, outputCount int
	if !s.ReadCompactSize(&spendCount) {
		return nil, errors.New("could not read nShieldedSpend")
	}
	if spendCount >= (1 << 16) {
		return nil, errors.New(fmt.Sprintf("spentCount (%d) must be less than 2^16", spendCount))
	}
	tx.shieldedSpends = make([]*spend, spendCount)
	for i := 0; i < spendCount; i++ {
		newSpend := &spend{}
		s, err = newSpend.ParseFromSlice([]byte(s), tx.version)
		if err != nil {
			return nil, errors.Wrap(err, "while parsing shielded Spend")
		}
		tx.shieldedSpends[i] = newSpend
	}
	if !s.ReadCompactSize(&outputCount) {
		return nil, errors.New("could not read nShieldedOutput")
	}
	if outputCount >= (1 << 16) {
		return nil, errors.New(fmt.Sprintf("outputCount (%d) must be less than 2^16", outputCount))
	}
	tx.shieldedOutputs = make([]*output, outputCount)
	for i := 0; i < outputCount; i++ {
		newOutput := &output{}
		s, err = newOutput.ParseFromSlice([]byte(s), tx.version)
		if err != nil {
			return nil, errors.Wrap(err, "while parsing shielded Output")
		}
		tx.shieldedOutputs[i] = newOutput
	}
	if spendCount+outputCount > 0 && !s.ReadInt64(&tx.valueBalanceSapling) {
		return nil, errors.New("could not read valueBalance")
	}
	if spendCount > 0 && !s.Skip(32) {
		return nil, errors.New("could not skip anchorSapling")
	}
	if !s.Skip(192 * spendCount) {
		return nil, errors.New("could not skip vSpendProofsSapling")
	}
	if !s.Skip(64 * spendCount) {
		return nil, errors.New("could not skip vSpendAuthSigsSapling")
	}
	if !s.Skip(192 * outputCount) {
		return nil, errors.New("could not skip vOutputProofsSapling")
	}
	if spendCount+outputCount > 0 && !s.ReadBytes(&tx.bindingSigSapling, 64) {
		return nil, errors.New("could not read bindingSigSapling")
	}
	var actionsCount int
	if !s.ReadCompactSize(&actionsCount) {
		return nil, errors.New("could not read nActionsOrchard")
	}
	if actionsCount >= (1 << 16) {
		return nil, errors.New(fmt.Sprintf("actionsCount (%d) must be less than 2^16", actionsCount))
	}
	if !s.Skip(820 * actionsCount) {
		return nil, errors.New("could not skip vActionsOrchard")
	}
	if actionsCount > 0 {
		if !s.Skip(1) {
			return nil, errors.New("could not skip flagsOrchard")
		}
		if !s.Skip(8) {
			return nil, errors.New("could not skip valueBalanceOrchard")
		}
		if !s.Skip(32) {
			return nil, errors.New("could not skip anchorOrchard")
		}
		var proofsCount int
		if !s.ReadCompactSize(&proofsCount) {
			return nil, errors.New("could not read sizeProofsOrchard")
		}
		if !s.Skip(proofsCount) {
			return nil, errors.New("could not skip proofsOrchard")
		}
		if !s.Skip(64 * actionsCount) {
			return nil, errors.New("could not skip vSpendAuthSigsOrchard")
		}
		if !s.Skip(64) {
			return nil, errors.New("could not skip bindingSigOrchard")
		}
	}
	return s, nil
}

// ParseFromSlice deserializes a single transaction from the given data.
func (tx *Transaction) ParseFromSlice(data []byte) ([]byte, error) {
	s := bytestring.String(data)

	// declare here to prevent shadowing problems in cryptobyte assignments
	var err error

	var header uint32
	if !s.ReadUint32(&header) {
		return nil, errors.New("could not read header")
	}

	tx.fOverwintered = (header >> 31) == 1
	if !tx.fOverwintered {
		return nil, errors.New("fOverwinter flag must be set")
	}
	tx.version = header & 0x7FFFFFFF
	if tx.version < 4 {
		return nil, errors.New(fmt.Sprintf("version number %d must be greater or equal to 4", tx.version))
	}

	if !s.ReadUint32(&tx.nVersionGroupID) {
		return nil, errors.New("could not read nVersionGroupId")
	}
	// parse the main part of the transaction
	if tx.version <= 4 {
		s, err = tx.parseV4([]byte(s))
	} else {
		s, err = tx.parseV5([]byte(s))
	}
	if err != nil {
		return nil, err
	}
	// TODO: implement rawBytes with MarshalBinary() instead
	txLen := len(data) - len(s)
	tx.rawBytes = data[:txLen]

	return []byte(s), nil
}

// NewTransaction is the constructor for a full transaction.
func NewTransaction() *Transaction {
	return &Transaction{
		rawTransaction: new(rawTransaction),
	}
}
