package parser

import (
	"fmt"

	"github.com/pkg/errors"
	"github.com/zcash-hackworks/lightwalletd/parser/internal/bytestring"
	"github.com/zcash-hackworks/lightwalletd/walletrpc"
)

type block struct {
	hdr    *blockHeader
	vtx    []*Transaction
	height int
}

func NewBlock() *block {
	return &block{height: -1}
}

func (b *block) GetVersion() int {
	return int(b.hdr.Version)
}

func (b *block) GetTxCount() int {
	return len(b.vtx)
}

func (b *block) Transactions() []*Transaction {
	// TODO: these should NOT be mutable
	return b.vtx
}

// GetDisplayHash returns the block hash in big-endian display order.
func (b *block) GetDisplayHash() []byte {
	return b.hdr.GetDisplayHash()
}

// TODO: encode hash endianness in a type?

// GetEncodableHash returns the block hash in little-endian wire order.
func (b *block) GetEncodableHash() []byte {
	return b.hdr.GetEncodableHash()
}

func (b *block) HasSaplingTransactions() bool {
	for _, tx := range b.vtx {
		if tx.HasSaplingTransactions() {
			return true
		}
	}
	return false
}

// see https://github.com/zcash-hackworks/lightwalletd/issues/17#issuecomment-467110828
const genesisTargetDifficulty = 520617983

// GetHeight() extracts the block height from the coinbase transaction. See
// BIP34. Returns block height on success, or -1 on error.
func (b *block) GetHeight() int {
	if b.height != -1 {
		return b.height
	}
	coinbaseScript := bytestring.String(b.vtx[0].transparentInputs[0].ScriptSig)
	var heightNum int64
	if ok := coinbaseScript.ReadScriptInt64(&heightNum); !ok {
		return -1
	}
	if heightNum < 0 {
		return -1
	}
	// uint32 should last us a while (Nov 2018)
	blockHeight := uint32(heightNum)

	if blockHeight == genesisTargetDifficulty {
		blockHeight = 0
	}

	b.height = int(blockHeight)
	return int(blockHeight)
}

func (b *block) ToCompact() *walletrpc.CompactBlock {
	compactBlock := &walletrpc.CompactBlock{
		//TODO ProtoVersion: 1,
		Height: uint64(b.GetHeight()),
		Hash:   b.GetEncodableHash(),
		Time:   b.hdr.Time,
	}

	// Only Sapling transactions have a meaningful compact encoding
	saplingTxns := make([]*walletrpc.CompactTx, 0, len(b.vtx))
	for idx, tx := range b.vtx {
		if tx.HasSaplingTransactions() {
			saplingTxns = append(saplingTxns, tx.ToCompact(idx))
		}
	}
	compactBlock.Vtx = saplingTxns
	return compactBlock
}

func (b *block) ParseFromSlice(data []byte) (rest []byte, err error) {
	hdr := NewBlockHeader()
	data, err = hdr.ParseFromSlice(data)
	if err != nil {
		return nil, errors.Wrap(err, "parsing block header")
	}

	s := bytestring.String(data)
	var txCount int
	if ok := s.ReadCompactSize(&txCount); !ok {
		return nil, errors.New("could not read tx_count")
	}
	data = []byte(s)

	vtx := make([]*Transaction, 0, txCount)
	for i := 0; len(data) > 0; i++ {
		tx := NewTransaction()
		data, err = tx.ParseFromSlice(data)
		if err != nil {
			return nil, errors.Wrap(err, fmt.Sprintf("parsing transaction %d", i))
		}
		vtx = append(vtx, tx)
	}

	b.hdr = hdr
	b.vtx = vtx

	return data, nil
}
