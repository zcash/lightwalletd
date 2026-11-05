// Copyright (c) 2019-present The Zcash developers
// Distributed under the MIT software license, see the accompanying
// file COPYING or https://www.opensource.org/licenses/mit-license.php .
package frontend

import (
	"runtime"
	"testing"

	"google.golang.org/grpc/encoding"
	"google.golang.org/grpc/mem"
	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/proto"

	"github.com/zcash/lightwalletd/walletrpc"
)

// emptyElements encodes count empty length-delimited values of one field.
func emptyElements(field protowire.Number, count int) []byte {
	var b []byte
	for range count {
		b = protowire.AppendTag(b, field, protowire.BytesType)
		b = protowire.AppendBytes(b, nil)
	}
	return b
}

func packedPoolTypes(count int) []byte {
	payload := make([]byte, count)
	for i := range payload {
		payload[i] = byte(walletrpc.PoolType_SAPLING)
	}
	b := protowire.AppendTag(nil, 3, protowire.BytesType)
	return protowire.AppendBytes(b, payload)
}

func unpackedPoolTypes(count int) []byte {
	var b []byte
	for range count {
		b = protowire.AppendTag(b, 3, protowire.VarintType)
		b = protowire.AppendVarint(b, uint64(walletrpc.PoolType_SAPLING))
	}
	return b
}

func decode(b []byte, v any) error {
	return NewBoundedCodec().Unmarshal(mem.BufferSlice{mem.SliceBuffer(b)}, v)
}

func TestBoundedCodecLimits(t *testing.T) {
	for _, tc := range []struct {
		name   string
		encode func(int) []byte
		limit  int
		msg    func() proto.Message
	}{
		{"AddressList", func(n int) []byte { return emptyElements(1, n) }, maxDecodedAddresses,
			func() proto.Message { return &walletrpc.AddressList{} }},
		{"GetAddressUtxosArg", func(n int) []byte { return emptyElements(1, n) }, maxDecodedAddresses,
			func() proto.Message { return &walletrpc.GetAddressUtxosArg{} }},
		{"GetMempoolTxRequest excludes", func(n int) []byte { return emptyElements(1, n) }, maxDecodedExcludes,
			func() proto.Message { return &walletrpc.GetMempoolTxRequest{} }},
		{"GetMempoolTxRequest packed poolTypes", packedPoolTypes, maxDecodedPoolTypes,
			func() proto.Message { return &walletrpc.GetMempoolTxRequest{} }},
		{"GetMempoolTxRequest unpacked poolTypes", unpackedPoolTypes, maxDecodedPoolTypes,
			func() proto.Message { return &walletrpc.GetMempoolTxRequest{} }},
		{"BlockRange packed poolTypes", packedPoolTypes, maxDecodedPoolTypes,
			func() proto.Message { return &walletrpc.BlockRange{} }},
		{"BlockRange unpacked poolTypes", unpackedPoolTypes, maxDecodedPoolTypes,
			func() proto.Message { return &walletrpc.BlockRange{} }},
	} {
		// At the limit the message decodes exactly as the stock codec decodes it.
		atLimit := tc.encode(tc.limit)
		got, want := tc.msg(), tc.msg()
		if err := decode(atLimit, got); err != nil {
			t.Errorf("%s: %d elements rejected: %v", tc.name, tc.limit, err)
		} else if err := proto.Unmarshal(atLimit, want); err != nil || !proto.Equal(got, want) {
			t.Errorf("%s: decoded differently from proto.Unmarshal", tc.name)
		}
		if err := decode(tc.encode(tc.limit+1), tc.msg()); err == nil {
			t.Errorf("%s: %d elements accepted", tc.name, tc.limit+1)
		}
	}
}

// The point is to refuse before decoding: a 4 MiB request of empty elements
// costs hundreds of MiB to decode, but only a scan of the bytes to refuse.
func TestBoundedCodecRefusesBeforeDecoding(t *testing.T) {
	b := emptyElements(1, (4<<20)/2-1)
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	err := decode(b, &walletrpc.GetMempoolTxRequest{})
	runtime.ReadMemStats(&after)
	if err == nil {
		t.Fatal("2 million excluded suffixes were accepted")
	}
	if allocated := after.TotalAlloc - before.TotalAlloc; allocated > 16<<20 {
		t.Fatalf("refusing allocated %d MiB", allocated>>20)
	}
}

func TestBoundedCodecLeavesOtherMessagesAlone(t *testing.T) {
	tx := &walletrpc.RawTransaction{Data: make([]byte, 3<<20), Height: 7}
	b, err := proto.Marshal(tx)
	if err != nil {
		t.Fatal(err)
	}
	var got walletrpc.RawTransaction
	if err := decode(b, &got); err != nil || !proto.Equal(&got, tx) {
		t.Fatalf("RawTransaction did not round-trip: %v", err)
	}
	if NewBoundedCodec().Name() != encoding.GetCodecV2("proto").Name() {
		t.Fatal("the codec must keep the protobuf content subtype")
	}
}
