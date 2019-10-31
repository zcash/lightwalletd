package parser

import (
	"fmt"

	"github.com/pkg/errors"
	"github.com/zcash-hackworks/lightwalletd/parser/internal/bytestring"
	"github.com/zcash-hackworks/lightwalletd/walletrpc"
)

type Block struct {
	hdr    *blockHeader
	vtx    []*Transaction
	height int
}

func NewBlock() *Block {
	return &Block{height: -1}
}

func (b *Block) GetVersion() int {
	return int(b.hdr.Version)
}

func (b *Block) GetTxCount() int {
	return len(b.vtx)
}

func (b *Block) Transactions() []*Transaction {
	// TODO: these should NOT be mutable
	return b.vtx
}

// GetDisplayHash returns the block hash in big-endian display order.
func (b *Block) GetDisplayHash() []byte {
	return b.hdr.GetDisplayHash()
}

// TODO: encode hash endianness in a type?

// GetEncodableHash returns the block hash in little-endian wire order.
func (b *Block) GetEncodableHash() []byte {
	return b.hdr.GetEncodableHash()
}

func (b *Block) GetDisplayPrevHash() []byte {
	rhash := make([]byte, len(b.hdr.HashPrevBlock))
	copy(rhash, b.hdr.HashPrevBlock)
	// Reverse byte order
	for i := 0; i < len(rhash)/2; i++ {
		j := len(rhash) - 1 - i
		rhash[i], rhash[j] = rhash[j], rhash[i]
	}
	return rhash
}

func (b *Block) HasSaplingTransactions() bool {
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
func (b *Block) GetHeight() int {
	if b.height != -1 {
		return b.height
	}
	coinbaseScript := bytestring.String(b.vtx[0].transparentInputs[0].ScriptSig)
	var heightNum int64
	if !coinbaseScript.ReadScriptInt64(&heightNum) {
		return -1
	}
	if heightNum < 0 {
		return -1
	}
	// uint32 should last us a while (Nov 2018)
	if heightNum > int64(^uint32(0)) {
		return -1
	}
	blockHeight := uint32(heightNum)

	if blockHeight == genesisTargetDifficulty {
		blockHeight = 0
	}

	b.height = int(blockHeight)
	return int(blockHeight)
}

func (b *Block) GetPrevHash() []byte {
	return b.hdr.HashPrevBlock
}

func (b *Block) ToCompact() *walletrpc.CompactBlock {
	compactBlock := &walletrpc.CompactBlock{
		//TODO ProtoVersion: 1,
		Height:   uint64(b.GetHeight()),
		PrevHash: b.hdr.HashPrevBlock,
		Hash:     b.GetEncodableHash(),
		Time:     b.hdr.Time,
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

func (b *Block) ParseFromSlice(data []byte) (rest []byte, err error) {
	hdr := NewBlockHeader()
	data, err = hdr.ParseFromSlice(data)
	if err != nil {
		return nil, errors.Wrap(err, "parsing block header")
	}

	s := bytestring.String(data)
	var txCount int
	if !s.ReadCompactSize(&txCount) {
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
