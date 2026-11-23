// Copyright (c) 2019-present The Zcash developers
// Distributed under the MIT software license, see the accompanying
// file COPYING or https://www.opensource.org/licenses/mit-license.php .
package frontend

import (
	"fmt"

	"google.golang.org/grpc/encoding"
	"google.golang.org/grpc/mem"
	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/proto"

	"github.com/zcash/lightwalletd/walletrpc"
)

// Element limits checked before decoding (GHSA-p4pq-3cf7-45rv); twice the handlers' caps, which still apply.
const (
	maxDecodedAddresses = 2 * maxTaddrsPerRequest
	maxDecodedExcludes  = 2 * maxExcludeTxidSuffixes
	maxDecodedPoolTypes = 1000
)

// repeatedBound limits the element count of one repeated field.
type repeatedBound struct {
	field  protowire.Number
	name   string
	max    int
	scalar bool // varint elements, which may also arrive packed
	// inner bounds a message field's own fields, summed over its occurrences.
	inner []repeatedBound
}

func requestBounds(v any) []repeatedBound {
	addresses := repeatedBound{field: 1, name: "addresses", max: maxDecodedAddresses}
	poolTypes := repeatedBound{field: 3, name: "poolTypes", max: maxDecodedPoolTypes, scalar: true}
	switch v.(type) {
	case *walletrpc.AddressList, *walletrpc.GetAddressUtxosArg:
		return []repeatedBound{addresses}
	case *walletrpc.GetMempoolTxRequest:
		return []repeatedBound{{field: 1, name: "exclude_txid_suffixes", max: maxDecodedExcludes}, poolTypes}
	case *walletrpc.BlockRange:
		return []repeatedBound{poolTypes}
	case *walletrpc.TransparentAddressBlockFilter:
		rangePoolTypes := poolTypes
		rangePoolTypes.name = "range.poolTypes"
		return []repeatedBound{{field: 2, name: "range", inner: []repeatedBound{rangePoolTypes}}}
	}
	return nil
}

// BoundedCodec is gRPC's protobuf codec, except that it counts the elements
// of repeated request fields in the encoded message before decoding it.
type BoundedCodec struct {
	encoding.CodecV2
}

// NewBoundedCodec wraps the protobuf codec registered with gRPC.
func NewBoundedCodec() BoundedCodec {
	return BoundedCodec{encoding.GetCodecV2("proto")}
}

func (c BoundedCodec) Unmarshal(data mem.BufferSlice, v any) error {
	bounds := requestBounds(v)
	if bounds == nil {
		return c.CodecV2.Unmarshal(data, v)
	}
	buf := data.MaterializeToBuffer(mem.DefaultBufferPool())
	defer buf.Free()
	b := buf.ReadOnlyData()
	if err := checkRepeatedCounts(b, bounds); err != nil {
		return err
	}
	return proto.Unmarshal(b, v.(proto.Message))
}

// checkRepeatedCounts scans the encoded message without decoding it. Malformed
// input is left for proto.Unmarshal to reject.
func checkRepeatedCounts(b []byte, bounds []repeatedBound) error {
	return countRepeated(b, bounds, map[*repeatedBound]int{})
}

func countRepeated(b []byte, bounds []repeatedBound, counts map[*repeatedBound]int) error {
	for len(b) > 0 {
		num, typ, n := protowire.ConsumeTag(b)
		if n < 0 {
			return nil
		}
		b = b[n:]
		n = protowire.ConsumeFieldValue(num, typ, b)
		if n < 0 {
			return nil
		}
		for i := range bounds {
			bound := &bounds[i]
			if num != bound.field {
				continue
			}
			if bound.inner != nil {
				if typ == protowire.BytesType {
					payload, _ := protowire.ConsumeBytes(b[:n])
					if err := countRepeated(payload, bound.inner, counts); err != nil {
						return err
					}
				}
				continue
			}
			switch {
			case bound.scalar && typ == protowire.VarintType:
				counts[bound]++
			case bound.scalar && typ == protowire.BytesType:
				// Packed: every varint ends in a byte with the high bit clear.
				payload, _ := protowire.ConsumeBytes(b[:n])
				for _, c := range payload {
					if c < 0x80 {
						counts[bound]++
					}
				}
			case !bound.scalar && typ == protowire.BytesType:
				counts[bound]++
			}
			if counts[bound] > bound.max {
				return fmt.Errorf("%s has more than %d elements", bound.name, bound.max)
			}
		}
		b = b[n:]
	}
	return nil
}
