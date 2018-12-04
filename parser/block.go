package parser

import (
	"fmt"

	"github.com/gtank/ctxd/parser/internal/bytestring"
	"github.com/gtank/ctxd/rpc"
	"github.com/pkg/errors"
)

type block struct {
	hdr *blockHeader
	vtx []*transaction
}

func NewBlock() *block {
	return &block{}
}

func (b *block) GetVersion() int {
	return int(b.hdr.Version)
}

func (b *block) GetTxCount() int {
	return len(b.vtx)
}

// GetDisplayHash returns the block hash in big-endian display order.
func (b *block) GetDisplayHash() []byte {
	return b.hdr.GetDisplayHash()
}

// TODO: encode hash endianness in a type?

// getEncodableHash returns the block hash in little-endian wire order.
func (b *block) getEncodableHash() []byte {
	return b.hdr.getEncodableHash()
}

func (b *block) HasSaplingTransactions() bool {
	for _, tx := range b.vtx {
		if tx.HasSaplingTransactions() {
			return true
		}
	}
	return false
}

// GetHeight() extracts the block height from the coinbase transaction. See
// BIP34. Returns block height on success, or -1 on error.
func (b *block) GetHeight() int {
	coinbaseScript := bytestring.String(b.vtx[0].transparentInputs[0].ScriptSig)
	var heightByte byte
	if ok := coinbaseScript.ReadByte(&heightByte); !ok {
		return -1
	}
	heightLen := int(heightByte)
	var heightBytes = make([]byte, heightLen)
	if ok := coinbaseScript.ReadBytes(&heightBytes, heightLen); !ok {
		return -1
	}
	// uint32 should last us a while (Nov 2018)
	var blockHeight uint32
	for i := heightLen - 1; i >= 0; i-- {
		blockHeight <<= 8
		blockHeight = blockHeight | uint32(heightBytes[i])
	}
	return int(blockHeight)
}

func (b *block) ToCompact() *rpc.CompactBlock {
	compactBlock := &rpc.CompactBlock{
		//TODO ProtoVersion: 1,
		Height: uint64(b.GetHeight()),
		Hash:   b.getEncodableHash(),
		//TODO Time:   b.hdr.Time,
	}
	compactBlock.Vtx = make([]*rpc.CompactTx, len(b.vtx))
	for idx, tx := range b.vtx {
		compactBlock.Vtx[idx] = tx.ToCompact(idx)
	}
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

	vtx := make([]*transaction, 0, txCount)
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
