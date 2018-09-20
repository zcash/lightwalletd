package parser

import (
	"github.com/gtank/ctxd/parser/internal/bytestring"
	"github.com/pkg/errors"
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
	joinSplitsPHGR13   []*phgr13JoinSplit
	joinSplitsGroth16  []*groth16JoinSplit
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

	if ok := s.ReadBytes(&tx.PrevTxHash, 32); !ok {
		return nil, errors.New("could not read PrevTxHash")
	}

	if ok := s.ReadUint32(&tx.PrevTxOutIndex); !ok {
		return nil, errors.New("could not read PrevTxOutIndex")
	}

	if ok := s.ReadCompactLengthPrefixed((*bytestring.String)(&tx.ScriptSig)); !ok {
		return nil, errors.New("could not read ScriptSig")
	}

	if ok := s.ReadUint32(&tx.SequenceNumber); !ok {
		return nil, errors.New("could not read SequenceNumber")
	}

	return []byte(s), nil
}

// Txout format as described in https://en.bitcoin.it/wiki/Transaction
type txOut struct {
	// Non-negative int giving the number of Satoshis to be transferred
	Value uint64

	// Script. CompactSize-prefixed.
	Script []byte
}

func (tx *txOut) ParseFromSlice(data []byte) ([]byte, error) {
	s := bytestring.String(data)

	if ok := s.ReadUint64(&tx.Value); !ok {
		return nil, errors.New("could not read txOut value")
	}

	if ok := s.ReadCompactLengthPrefixed((*bytestring.String)(&tx.Script)); !ok {
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

	if ok := s.ReadBytes(&p.cv, 32); !ok {
		return nil, errors.New("could not read cv")
	}

	if ok := s.ReadBytes(&p.anchor, 32); !ok {
		return nil, errors.New("could not read anchor")
	}

	if ok := s.ReadBytes(&p.nullifier, 32); !ok {
		return nil, errors.New("could not read nullifier")
	}

	if ok := s.ReadBytes(&p.rk, 32); !ok {
		return nil, errors.New("could not read rk")
	}

	if ok := s.ReadBytes(&p.zkproof, 192); !ok {
		return nil, errors.New("could not read zkproof")
	}

	if ok := s.ReadBytes(&p.spendAuthSig, 64); !ok {
		return nil, errors.New("could not read spendAuthSig")
	}

	return []byte(s), nil
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

	if ok := s.ReadBytes(&p.cv, 32); !ok {
		return nil, errors.New("could not read cv")
	}

	if ok := s.ReadBytes(&p.cmu, 32); !ok {
		return nil, errors.New("could not read cmu")
	}

	if ok := s.ReadBytes(&p.ephemeralKey, 32); !ok {
		return nil, errors.New("could not read ephemeralKey")
	}

	if ok := s.ReadBytes(&p.encCiphertext, 580); !ok {
		return nil, errors.New("could not read encCiphertext")
	}

	if ok := s.ReadBytes(&p.outCiphertext, 80); !ok {
		return nil, errors.New("could not read outCiphertext")
	}

	if ok := s.ReadBytes(&p.zkproof, 192); !ok {
		return nil, errors.New("could not read zkproof")
	}

	return []byte(s), nil
}

type phgr13JoinSplit struct {
	// something
}

type groth16JoinSplit struct {
	// something
}

type transaction struct {
	*rawTransaction
}

func (tx *transaction) ParseFromSlice(data []byte) ([]byte, error) {
	s := bytestring.String(data)

	var header uint32
	if ok := s.ReadUint32(&header); !ok {
		return nil, errors.New("could not read header")
	}

	tx.fOverwintered = (header >> 31) == 1
	tx.version = header & 0x7FFFFFFF

	if ok := s.ReadUint32(&tx.nVersionGroupId); !ok {
		return nil, errors.New("could not read nVersionGroupId")
	}

	var txInCount uint64
	if ok := s.ReadCompactSize(&txInCount); !ok {
		return nil, errors.New("could not read tx_in_count")
	}

	// TODO: Duplicate/otherwise-too-many transactions are a possible DoS
	// TODO: vector. At the moment we're assuming trusted input.
	// See https://nvd.nist.gov/vuln/detail/CVE-2018-17144 for an example.

	txInputs := make([]*txIn, txInCount)
	for i := 0; i < txInCount; i++ {
		ti := &txIn{}
		s, err = ti.ParseFromSlice([]byte(s))
		if err != nil {
			return nil, errors.Wrap(err, "while parsing transparent input")
		}
		txInputs[i] = ti
	}
	tx.transparentInputs = txInputs

	var txOutCount uint64
	if ok := s.ReadCompactSize(&txOutCount); !ok {
		return nil, errors.New("could not read tx_out_count")
	}

	txOutputs := make([]*txOut, txOutCount)
	for i := 0; i < txOutCount; i++ {
		to := &txOut{}
		s, err = to.ParseFromSlice([]byte(s))
		if err != nil {
			return nil, errors.Wrap(err, "while parsing transparent output")
		}
		txOutputs[i] = to
	}
	tx.transparentOutputs = txOutputs

	if ok := s.ReadUint32(&tx.nLockTime); !ok {
		return nil, errors.New("could not read nLockTime")
	}

	if ok := s.ReadUint32(&tx.nExpiryHeight); !ok {
		return nil, errors.New("could not read nExpiryHeight")
	}

	if ok := s.ReadInt64(&tx.valueBalance); !ok {
		return nil, errors.New("could not read valueBalance")
	}

	var spendCount uint64
	if ok := s.ReadCompactSize(&spendCount); !ok {
		return nil, errors.New("could not read nShieldedSpend")
	}

	txSpends := make([]*spend, spendCount)
	for i := 0; i < spendCount; i++ {
		newSpend := &spend{}
		s, err = newSpend.ParseFromSlice([]byte(s))
		if err != nil {
			return nil, errors.Wrap(err, "while parsing shielded Spend")
		}
		txSpends[i] = newSpend
	}
	tx.shieldedSpends = txSpends

	var outputCount uint64
	if ok := s.ReadCompactSize(&outputCount); !ok {
		return nil, errors.New("could not read nShieldedOutput")
	}

	txOutputs := make([]*output, outputCount)
	for i := 0; i < outputCount; i++ {
		newOutput := &output{}
		s, err = newOutput.ParseFromSlice([]byte(s))
		if err != nil {
			return nil, errors.Wrap(err, "while parsing shielded Output")
		}
		txOutputs[i] = newOutput
	}
	tx.shieldedOutputs = txOutputs
}

func newTransaction() *transaction {
	return &transaction{
		rawTransaction: new(rawTransaction),
	}
}
