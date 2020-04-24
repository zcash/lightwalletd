// Copyright (c) 2019-2020 The Zcash developers
// Distributed under the MIT software license, see the accompanying
// file COPYING or https://www.opensource.org/licenses/mit-license.php .
package parser

import (
	"crypto/sha256"

	"github.com/pkg/errors"
	"github.com/zcash/lightwalletd/parser/internal/bytestring"
	"github.com/zcash/lightwalletd/walletrpc"
)

type rawTransaction struct {
	fOverwintered      bool
	version            uint32
	nVersionGroupId    uint32
	transparentInputs  []*txIn
	transparentOutputs []*txOut
	nLockTime          uint32
	nExpiryHeight      uint32
	valueBalance       int64
	shieldedSpends     []*spend
	shieldedOutputs    []*output
	joinSplits         []*joinSplit
	joinSplitPubKey    []byte
	joinSplitSig       []byte
	bindingSig         []byte
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

// spend is a Sapling Spend Description as described in 7.3 of the Zcash
// protocol spec.  Total size is 384 bytes.
type spend struct {
	cv           []byte // 32
	anchor       []byte // 32
	nullifier    []byte // 32
	rk           []byte // 32
	zkproof      []byte // 192
	spendAuthSig []byte // 64
}

func (p *spend) ParseFromSlice(data []byte) ([]byte, error) {
	s := bytestring.String(data)

	if !s.ReadBytes(&p.cv, 32) {
		return nil, errors.New("could not read cv")
	}

	if !s.ReadBytes(&p.anchor, 32) {
		return nil, errors.New("could not read anchor")
	}

	if !s.ReadBytes(&p.nullifier, 32) {
		return nil, errors.New("could not read nullifier")
	}

	if !s.ReadBytes(&p.rk, 32) {
		return nil, errors.New("could not read rk")
	}

	if !s.ReadBytes(&p.zkproof, 192) {
		return nil, errors.New("could not read zkproof")
	}

	if !s.ReadBytes(&p.spendAuthSig, 64) {
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
// Zcash protocol spec. Total size is 948.
type output struct {
	cv            []byte // 32
	cmu           []byte // 32
	ephemeralKey  []byte // 32
	encCiphertext []byte // 580
	outCiphertext []byte // 80
	zkproof       []byte // 192
}

func (p *output) ParseFromSlice(data []byte) ([]byte, error) {
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

	if !s.ReadBytes(&p.zkproof, 192) {
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
// upgrade level.
type joinSplit struct {
	vpubOld        uint64
	vpubNew        uint64
	anchor         []byte    // 32
	nullifiers     [2][]byte // 64 [N_old][32]byte
	commitments    [2][]byte // 64 [N_new][32]byte
	ephemeralKey   []byte    // 32
	randomSeed     []byte    // 32
	vmacs          [2][]byte // 64 [N_old][32]byte
	proofPHGR13    []byte    // 296
	proofGroth16   []byte    // 192
	encCiphertexts [2][]byte // 1202 [N_new][601]byte

	// not actually in the format, but needed for parsing
	version uint32
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

	if p.version == 2 || p.version == 3 {
		if !s.ReadBytes(&p.proofPHGR13, 296) {
			return nil, errors.New("could not read PHGR13 proof")
		}
	} else if p.version >= 4 {
		if !s.ReadBytes(&p.proofGroth16, 192) {
			return nil, errors.New("could not read Groth16 proof")
		}
	} else {
		return nil, errors.New("unexpected transaction version")
	}

	for i := 0; i < 2; i++ {
		if !s.ReadBytes(&p.encCiphertexts[i], 601) {
			return nil, errors.New("could not read an encCiphertext")
		}
	}

	return []byte(s), nil
}

type Transaction struct {
	*rawTransaction
	rawBytes []byte
	txId     []byte
}

// GetDisplayHash returns the transaction hash in big-endian display order.
func (tx *Transaction) GetDisplayHash() []byte {
	if tx.txId != nil {
		return tx.txId
	}

	// SHA256d
	digest := sha256.Sum256(tx.rawBytes)
	digest = sha256.Sum256(digest[:])

	// Reverse byte order
	for i := 0; i < len(digest)/2; i++ {
		j := len(digest) - 1 - i
		digest[i], digest[j] = digest[j], digest[i]
	}

	tx.txId = digest[:]
	return tx.txId
}

// GetEncodableHash returns the transaction hash in little-endian wire format order.
func (tx *Transaction) GetEncodableHash() []byte {
	digest := sha256.Sum256(tx.rawBytes)
	digest = sha256.Sum256(digest[:])
	return digest[:]
}

func (tx *Transaction) Bytes() []byte {
	return tx.rawBytes
}

func (tx *Transaction) HasSaplingElements() bool {
	return tx.version >= 4 && (len(tx.shieldedSpends)+len(tx.shieldedOutputs)) > 0
}

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

func (tx *Transaction) ParseFromSlice(data []byte) ([]byte, error) {
	s := bytestring.String(data)

	// declare here to prevent shadowing problems in cryptobyte assignments
	var err error

	var header uint32
	if !s.ReadUint32(&header) {
		return nil, errors.New("could not read header")
	}

	tx.fOverwintered = (header >> 31) == 1
	tx.version = header & 0x7FFFFFFF

	if tx.version >= 3 {
		if !s.ReadUint32(&tx.nVersionGroupId) {
			return nil, errors.New("could not read nVersionGroupId")
		}
	}

	var txInCount int
	if !s.ReadCompactSize(&txInCount) {
		return nil, errors.New("could not read tx_in_count")
	}

	// TODO: Duplicate/otherwise-too-many transactions are a possible DoS
	// TODO: vector. At the moment we're assuming trusted input.
	// See https://nvd.nist.gov/vuln/detail/CVE-2018-17144 for an example.

	if txInCount > 0 {
		tx.transparentInputs = make([]*txIn, txInCount)
		for i := 0; i < txInCount; i++ {
			ti := &txIn{}
			s, err = ti.ParseFromSlice([]byte(s))
			if err != nil {
				return nil, errors.Wrap(err, "while parsing transparent input")
			}
			tx.transparentInputs[i] = ti
		}
	}

	var txOutCount int
	if !s.ReadCompactSize(&txOutCount) {
		return nil, errors.New("could not read tx_out_count")
	}

	if txOutCount > 0 {
		tx.transparentOutputs = make([]*txOut, txOutCount)
		for i := 0; i < txOutCount; i++ {
			to := &txOut{}
			s, err = to.ParseFromSlice([]byte(s))
			if err != nil {
				return nil, errors.Wrap(err, "while parsing transparent output")
			}
			tx.transparentOutputs[i] = to
		}
	}

	if !s.ReadUint32(&tx.nLockTime) {
		return nil, errors.New("could not read nLockTime")
	}

	if tx.fOverwintered {
		if !s.ReadUint32(&tx.nExpiryHeight) {
			return nil, errors.New("could not read nExpiryHeight")
		}
	}

	var spendCount, outputCount int

	if tx.version >= 4 {
		if !s.ReadInt64(&tx.valueBalance) {
			return nil, errors.New("could not read valueBalance")
		}

		if !s.ReadCompactSize(&spendCount) {
			return nil, errors.New("could not read nShieldedSpend")
		}

		if spendCount > 0 {
			tx.shieldedSpends = make([]*spend, spendCount)
			for i := 0; i < spendCount; i++ {
				newSpend := &spend{}
				s, err = newSpend.ParseFromSlice([]byte(s))
				if err != nil {
					return nil, errors.Wrap(err, "while parsing shielded Spend")
				}
				tx.shieldedSpends[i] = newSpend
			}
		}

		if !s.ReadCompactSize(&outputCount) {
			return nil, errors.New("could not read nShieldedOutput")
		}

		if outputCount > 0 {
			tx.shieldedOutputs = make([]*output, outputCount)
			for i := 0; i < outputCount; i++ {
				newOutput := &output{}
				s, err = newOutput.ParseFromSlice([]byte(s))
				if err != nil {
					return nil, errors.Wrap(err, "while parsing shielded Output")
				}
				tx.shieldedOutputs[i] = newOutput
			}
		}
	}

	if tx.version >= 2 {
		var joinSplitCount int
		if !s.ReadCompactSize(&joinSplitCount) {
			return nil, errors.New("could not read nJoinSplit")
		}

		if joinSplitCount > 0 {
			tx.joinSplits = make([]*joinSplit, joinSplitCount)
			for i := 0; i < joinSplitCount; i++ {
				js := &joinSplit{version: tx.version}
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
	}

	if tx.version >= 4 && (spendCount+outputCount > 0) {
		if !s.ReadBytes(&tx.bindingSig, 64) {
			return nil, errors.New("could not read bindingSig")
		}
	}

	// TODO: implement rawBytes with MarshalBinary() instead
	txLen := len(data) - len(s)
	tx.rawBytes = data[:txLen]

	return []byte(s), nil
}

func NewTransaction() *Transaction {
	return &Transaction{
		rawTransaction: new(rawTransaction),
	}
}
