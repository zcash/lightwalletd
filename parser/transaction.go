// Copyright (c) 2019-2020 The Zcash developers
// Distributed under the MIT software license, see the accompanying
// file COPYING or https://www.opensource.org/licenses/mit-license.php .

// Package parser deserializes (full) transactions (zcashd).
package parser

import (
	"errors"
	"fmt"

	"github.com/zcash/lightwalletd/hash32"
	"github.com/zcash/lightwalletd/parser/internal/bytestring"
	"github.com/zcash/lightwalletd/walletrpc"
)

type rawTransaction struct {
	fOverwintered      bool
	version            uint32
	nVersionGroupID    uint32
	consensusBranchID  uint32
	transparentInputs  []txIn
	transparentOutputs []txOut
	//nLockTime           uint32
	//nExpiryHeight       uint32
	//valueBalanceSapling int64
	shieldedSpends  []spend
	shieldedOutputs []output
	joinSplits      []joinSplit
	//joinSplitPubKey     []byte
	//joinSplitSig        []byte
	//bindingSigSapling   []byte
	orchardActions []action
}

// Txin format as described in https://en.bitcoin.it/wiki/Transaction
type txIn struct {
	// SHA256d of a previous (to-be-used) transaction
	//PrevTxHash []byte

	// Index of the to-be-used output in the previous tx
	//PrevTxOutIndex uint32

	// CompactSize-prefixed, could be a pubkey or a script
	ScriptSig []byte

	// Bitcoin: "normally 0xFFFFFFFF; irrelevant unless transaction's lock_time > 0"
	//SequenceNumber uint32
}

func (tx *txIn) ParseFromSlice(data []byte) ([]byte, error) {
	s := bytestring.String(data)

	if !s.Skip(32) {
		return nil, errors.New("could not skip PrevTxHash")
	}

	if !s.Skip(4) {
		return nil, errors.New("could not skip PrevTxOutIndex")
	}

	if !s.ReadCompactLengthPrefixed((*bytestring.String)(&tx.ScriptSig)) {
		return nil, errors.New("could not read ScriptSig")
	}

	if !s.Skip(4) {
		return nil, errors.New("could not skip SequenceNumber")
	}

	return []byte(s), nil
}

// Txout format as described in https://en.bitcoin.it/wiki/Transaction
type txOut struct {
	// Non-negative int giving the number of zatoshis to be transferred
	Value uint64

	// Script. CompactSize-prefixed.
	//Script []byte
}

func (tx *txOut) ParseFromSlice(data []byte) ([]byte, error) {
	s := bytestring.String(data)

	if !s.Skip(8) {
		return nil, errors.New("could not skip txOut value")
	}

	if !s.SkipCompactLengthPrefixed() {
		return nil, errors.New("could not skip txOut script")
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
	tx.transparentInputs = make([]txIn, txInCount)
	for i := 0; i < txInCount; i++ {
		ti := &tx.transparentInputs[i]
		s, err = ti.ParseFromSlice([]byte(s))
		if err != nil {
			return nil, fmt.Errorf("error parsing transparent input: %w", err)
		}
	}

	var txOutCount int
	if !s.ReadCompactSize(&txOutCount) {
		return nil, errors.New("could not read tx_out_count")
	}
	tx.transparentOutputs = make([]txOut, txOutCount)
	for i := 0; i < txOutCount; i++ {
		to := &tx.transparentOutputs[i]
		s, err = to.ParseFromSlice([]byte(s))
		if err != nil {
			return nil, fmt.Errorf("error parsing transparent output: %w", err)
		}
	}
	return []byte(s), nil
}

// spend is a Sapling Spend Description as described in 7.3 of the Zcash
// protocol specification.
type spend struct {
	//cv           []byte // 32
	//anchor       []byte // 32
	nullifier []byte // 32
	//rk           []byte // 32
	//zkproof      []byte // 192
	//spendAuthSig []byte // 64
}

func (p *spend) ParseFromSlice(data []byte, version uint32) ([]byte, error) {
	s := bytestring.String(data)

	if !s.Skip(32) {
		return nil, errors.New("could not skip cv")
	}

	if version <= 4 && !s.Skip(32) {
		return nil, errors.New("could not skip anchor")
	}

	if !s.ReadBytes(&p.nullifier, 32) {
		return nil, errors.New("could not read nullifier")
	}

	if !s.Skip(32) {
		return nil, errors.New("could not skip rk")
	}

	if version <= 4 && !s.Skip(192) {
		return nil, errors.New("could not skip zkproof")
	}

	if version <= 4 && !s.Skip(64) {
		return nil, errors.New("could not skip spendAuthSig")
	}

	return []byte(s), nil
}

func (p *spend) ToCompact() *walletrpc.CompactSaplingSpend {
	return &walletrpc.CompactSaplingSpend{
		Nf: p.nullifier,
	}
}

// output is a Sapling Output Description as described in section 7.4 of the
// Zcash protocol spec.
type output struct {
	//cv            []byte // 32
	cmu           []byte // 32
	ephemeralKey  []byte // 32
	encCiphertext []byte // 580
	//outCiphertext []byte // 80
	//zkproof       []byte // 192
}

func (p *output) ParseFromSlice(data []byte, version uint32) ([]byte, error) {
	s := bytestring.String(data)

	if !s.Skip(32) {
		return nil, errors.New("could not skip cv")
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

	if !s.Skip(80) {
		return nil, errors.New("could not skip outCiphertext")
	}

	if version <= 4 && !s.Skip(192) {
		return nil, errors.New("could not skip zkproof")
	}

	return []byte(s), nil
}

func (p *output) ToCompact() *walletrpc.CompactSaplingOutput {
	return &walletrpc.CompactSaplingOutput{
		Cmu:          p.cmu,
		EphemeralKey: p.ephemeralKey,
		Ciphertext:   p.encCiphertext[:52],
	}
}

// joinSplit is a JoinSplit description as described in 7.2 of the Zcash
// protocol spec. Its exact contents differ by transaction version and network
// upgrade level. Only version 4 is supported, no need for proofPHGR13.
type joinSplit struct {
	//vpubOld        uint64
	//vpubNew        uint64
	//anchor         []byte    // 32
	//nullifiers     [2][]byte // 64 [N_old][32]byte
	//commitments    [2][]byte // 64 [N_new][32]byte
	//ephemeralKey   []byte    // 32
	//randomSeed     []byte    // 32
	//vmacs          [2][]byte // 64 [N_old][32]byte
	//proofGroth16   []byte    // 192 (version 4 only)
	//encCiphertexts [2][]byte // 1202 [N_new][601]byte
}

func (p *joinSplit) ParseFromSlice(data []byte) ([]byte, error) {
	s := bytestring.String(data)

	if !s.Skip(8) {
		return nil, errors.New("could not skip vpubOld")
	}

	if !s.Skip(8) {
		return nil, errors.New("could not skip vpubNew")
	}

	if !s.Skip(32) {
		return nil, errors.New("could not skip anchor")
	}

	for i := 0; i < 2; i++ {
		if !s.Skip(32) {
			return nil, errors.New("could not skip a nullifier")
		}
	}

	for i := 0; i < 2; i++ {
		if !s.Skip(32) {
			return nil, errors.New("could not skip a commitment")
		}
	}

	if !s.Skip(32) {
		return nil, errors.New("could not skip ephemeralKey")
	}

	if !s.Skip(32) {
		return nil, errors.New("could not skip randomSeed")
	}

	for i := 0; i < 2; i++ {
		if !s.Skip(32) {
			return nil, errors.New("could not skip a vmac")
		}
	}

	if !s.Skip(192) {
		return nil, errors.New("could not skip Groth16 proof")
	}

	for i := 0; i < 2; i++ {
		if !s.Skip(601) {
			return nil, errors.New("could not skip an encCiphertext")
		}
	}

	return []byte(s), nil
}

type action struct {
	//cv            []byte // 32
	nullifier []byte // 32
	//rk            []byte // 32
	cmx           []byte // 32
	ephemeralKey  []byte // 32
	encCiphertext []byte // 580
	//outCiphertext []byte // 80
}

func (a *action) ParseFromSlice(data []byte) ([]byte, error) {
	s := bytestring.String(data)
	if !s.Skip(32) {
		return nil, errors.New("could not read action cv")
	}
	if !s.ReadBytes(&a.nullifier, 32) {
		return nil, errors.New("could not read action nullifier")
	}
	if !s.Skip(32) {
		return nil, errors.New("could not read action rk")
	}
	if !s.ReadBytes(&a.cmx, 32) {
		return nil, errors.New("could not read action cmx")
	}
	if !s.ReadBytes(&a.ephemeralKey, 32) {
		return nil, errors.New("could not read action ephemeralKey")
	}
	if !s.ReadBytes(&a.encCiphertext, 580) {
		return nil, errors.New("could not read action encCiphertext")
	}
	if !s.Skip(80) {
		return nil, errors.New("could not read action outCiphertext")
	}
	return []byte(s), nil
}

func (p *action) ToCompact() *walletrpc.CompactOrchardAction {
	return &walletrpc.CompactOrchardAction{
		Nullifier:    p.nullifier,
		Cmx:          p.cmx,
		EphemeralKey: p.ephemeralKey,
		Ciphertext:   p.encCiphertext[:52],
	}
}

// Transaction encodes a full (zcashd) transaction.
type Transaction struct {
	*rawTransaction
	rawBytes []byte
	txID     hash32.T // from getblock verbose=1
}

func (tx *Transaction) SetTxID(txid hash32.T) {
	tx.txID = txid
}

// GetDisplayHash returns the transaction hash in big-endian display order.
func (tx *Transaction) GetDisplayHash() hash32.T {
	// Convert to big-endian
	return hash32.Reverse(tx.txID)
}

func (tx *Transaction) GetDisplayHashString() string {
	return hash32.Encode(tx.GetDisplayHash())
}

// GetEncodableHash returns the transaction hash in little-endian wire format order.
func (tx *Transaction) GetEncodableHash() hash32.T {
	return tx.txID
}

// Bytes returns a full transaction's raw bytes.
func (tx *Transaction) Bytes() []byte {
	return tx.rawBytes
}

// HasShieldedElements indicates whether a transaction has
// at least one shielded input or output.
func (tx *Transaction) HasShieldedElements() bool {
	nshielded := len(tx.shieldedSpends) + len(tx.shieldedOutputs) + len(tx.orchardActions)
	return tx.version >= 4 && nshielded > 0
}

// SaplingOutputsCount returns the number of Sapling outputs in the transaction.
func (tx *Transaction) SaplingOutputsCount() int {
	return len(tx.shieldedOutputs)
}

// OrchardActionsCount returns the number of Orchard actions in the transaction.
func (tx *Transaction) OrchardActionsCount() int {
	return len(tx.orchardActions)
}

// ToCompact converts the given (full) transaction to compact format.
func (tx *Transaction) ToCompact(index int) *walletrpc.CompactTx {
	ctx := &walletrpc.CompactTx{
		Index: uint64(index), // index is contextual
		Hash:  hash32.ToSlice(tx.GetEncodableHash()),
		//Fee:     0, // TODO: calculate fees
		Spends:  make([]*walletrpc.CompactSaplingSpend, len(tx.shieldedSpends)),
		Outputs: make([]*walletrpc.CompactSaplingOutput, len(tx.shieldedOutputs)),
		Actions: make([]*walletrpc.CompactOrchardAction, len(tx.orchardActions)),
	}
	for i, spend := range tx.shieldedSpends {
		ctx.Spends[i] = spend.ToCompact()
	}
	for i, output := range tx.shieldedOutputs {
		ctx.Outputs[i] = output.ToCompact()
	}
	for i, a := range tx.orchardActions {
		ctx.Actions[i] = a.ToCompact()
	}
	return ctx
}

// parse version 4 transaction data after the nVersionGroupId field.
func (tx *Transaction) parseV4(data []byte) ([]byte, error) {
	s := bytestring.String(data)
	var err error
	if tx.nVersionGroupID != 0x892F2085 {
		return nil, fmt.Errorf("version group ID %x must be 0x892F2085", tx.nVersionGroupID)
	}
	s, err = tx.ParseTransparent([]byte(s))
	if err != nil {
		return nil, err
	}
	if !s.Skip(4) {
		return nil, errors.New("could not skip nLockTime")
	}

	if !s.Skip(4) {
		return nil, errors.New("could not skip nExpiryHeight")
	}

	var spendCount, outputCount int

	if !s.Skip(8) {
		return nil, errors.New("could not skip valueBalance")
	}
	if !s.ReadCompactSize(&spendCount) {
		return nil, errors.New("could not read nShieldedSpend")
	}
	tx.shieldedSpends = make([]spend, spendCount)
	for i := 0; i < spendCount; i++ {
		newSpend := &tx.shieldedSpends[i]
		s, err = newSpend.ParseFromSlice([]byte(s), 4)
		if err != nil {
			return nil, fmt.Errorf("error parsing shielded Spend: %w", err)
		}
	}
	if !s.ReadCompactSize(&outputCount) {
		return nil, errors.New("could not read nShieldedOutput")
	}
	tx.shieldedOutputs = make([]output, outputCount)
	for i := 0; i < outputCount; i++ {
		newOutput := &tx.shieldedOutputs[i]
		s, err = newOutput.ParseFromSlice([]byte(s), 4)
		if err != nil {
			return nil, fmt.Errorf("error parsing shielded Output: %w", err)
		}
	}
	var joinSplitCount int
	if !s.ReadCompactSize(&joinSplitCount) {
		return nil, errors.New("could not read nJoinSplit")
	}

	tx.joinSplits = make([]joinSplit, joinSplitCount)
	if joinSplitCount > 0 {
		for i := 0; i < joinSplitCount; i++ {
			js := &tx.joinSplits[i]
			s, err = js.ParseFromSlice([]byte(s))
			if err != nil {
				return nil, fmt.Errorf("error parsing JoinSplit: %w", err)
			}
		}

		if !s.Skip(32) {
			return nil, errors.New("could not skip joinSplitPubKey")
		}

		if !s.Skip(64) {
			return nil, errors.New("could not skip joinSplitSig")
		}
	}
	if spendCount+outputCount > 0 && !s.Skip(64) {
		return nil, errors.New("could not skip bindingSigSapling")
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
	if !s.Skip(4) {
		return nil, errors.New("could not skip nLockTime")
	}
	if !s.Skip(4) {
		return nil, errors.New("could not skip nExpiryHeight")
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
	tx.shieldedSpends = make([]spend, spendCount)
	for i := 0; i < spendCount; i++ {
		newSpend := &tx.shieldedSpends[i]
		s, err = newSpend.ParseFromSlice([]byte(s), tx.version)
		if err != nil {
			return nil, fmt.Errorf("error parsing shielded Spend: %w", err)
		}
	}
	if !s.ReadCompactSize(&outputCount) {
		return nil, errors.New("could not read nShieldedOutput")
	}
	if outputCount >= (1 << 16) {
		return nil, errors.New(fmt.Sprintf("outputCount (%d) must be less than 2^16", outputCount))
	}
	tx.shieldedOutputs = make([]output, outputCount)
	for i := 0; i < outputCount; i++ {
		newOutput := &tx.shieldedOutputs[i]
		s, err = newOutput.ParseFromSlice([]byte(s), tx.version)
		if err != nil {
			return nil, fmt.Errorf("error parsing shielded Output: %w", err)
		}
	}
	if spendCount+outputCount > 0 && !s.Skip(8) {
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
	if spendCount+outputCount > 0 && !s.Skip(64) {
		return nil, errors.New("could not skip bindingSigSapling")
	}
	var actionsCount int
	if !s.ReadCompactSize(&actionsCount) {
		return nil, errors.New("could not read nActionsOrchard")
	}
	if actionsCount >= (1 << 16) {
		return nil, errors.New(fmt.Sprintf("actionsCount (%d) must be less than 2^16", actionsCount))
	}
	tx.orchardActions = make([]action, actionsCount)
	for i := 0; i < actionsCount; i++ {
		a := &tx.orchardActions[i]
		s, err = a.ParseFromSlice([]byte(s))
		if err != nil {
			return nil, fmt.Errorf("error parsing orchard action: %w", err)
		}
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
