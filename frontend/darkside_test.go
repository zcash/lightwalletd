// Copyright (c) 2019-present The Zcash developers
// Distributed under the MIT software license, see the accompanying
// file COPYING or https://www.opensource.org/licenses/mit-license.php .
package frontend

import (
	"context"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/zcash/lightwalletd/walletrpc"
)

// A short txid used to panic in hash32.FromSlice (GHSA-pp3p-5vmm-3cvw).
func TestDarksideAddAddressUtxoRejectsShortTxid(t *testing.T) {
	_, err := (&DarksideStreamer{}).AddAddressUtxo(context.Background(),
		&walletrpc.GetAddressUtxosReply{Txid: []byte{1, 2, 3}})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("got %v, want InvalidArgument", err)
	}
}
