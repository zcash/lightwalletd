package parser

import (
	"fmt"

	"github.com/gtank/ctxd/parser/internal/bytestring"
	"github.com/pkg/errors"
)

type block struct {
	hdr *blockHeader
	vtx []*transaction
}

func NewBlock() *block {
	return &block{}
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
