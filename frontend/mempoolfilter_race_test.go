// Copyright (c) 2019-present The Zcash developers
// Distributed under the MIT software license, see the accompanying
// file COPYING or https://www.opensource.org/licenses/mit-license.php .
package frontend

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/zcash/lightwalletd/common"
	"github.com/zcash/lightwalletd/walletrpc"
)

// GetMempoolTx reads its snapshot of the shared mempoolList without holding
// s.mutex, which is only safe if nothing writes to that slice once it has
// been published. MempoolFilter sorted its arguments in place, so every
// GetMempoolTx call rewrote the shared list, concurrently with other callers.

// seedMempool publishes a fresh cache of n Orchard txids, in the unsorted
// order getrawmempool returns them, and returns a copy of that list.
func seedMempool(n int) []string {
	list := make([]string, n)
	m := make(map[string]*walletrpc.CompactTx, n)
	for i := range list {
		list[i] = fmt.Sprintf("%064x", (n-i)*7919%1009) // unsorted, distinct
		m[list[i]] = &walletrpc.CompactTx{
			Actions: []*walletrpc.CompactOrchardAction{{Nullifier: make([]byte, 32)}},
		}
	}
	mempoolList = list
	mempoolMap = &m
	lastMempool = time.Now()
	return slices.Clone(list)
}

func noRefreshExpected(context.Context, string, []json.RawMessage) (json.RawMessage, error) {
	testT.Fatal("no mempool refresh expected; the cache was seeded fresh")
	return nil, nil
}

func TestMempoolFilterDoesNotModifyItsArguments(t *testing.T) {
	items := []string{"cc", "aa", "bb"}
	exclude := []string{"b", "a"}
	got := MempoolFilter(items, exclude)
	if !slices.Equal(got, []string{"cc"}) {
		t.Fatalf("MempoolFilter = %v, want [cc]", got)
	}
	if !slices.Equal(items, []string{"cc", "aa", "bb"}) {
		t.Fatalf("items modified to %v", items)
	}
	if !slices.Equal(exclude, []string{"b", "a"}) {
		t.Fatalf("exclude modified to %v", exclude)
	}
}

func TestGetMempoolTxLeavesPublishedListUntouched(t *testing.T) {
	testT = t
	defer resetGlobals()
	lwd, _ := testsetup()
	want := seedMempool(50)
	common.RawRequest = noRefreshExpected

	if err := lwd.GetMempoolTx(&walletrpc.GetMempoolTxRequest{}, &testmempooltx{}); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(mempoolList, want) {
		t.Fatal("GetMempoolTx rewrote the shared mempoolList outside s.mutex")
	}
}

// Run with -race: concurrent callers must not write to the shared snapshot,
// and each must receive every transaction exactly once.
func TestGetMempoolTxConcurrentCallers(t *testing.T) {
	testT = t
	defer resetGlobals()
	lwd, _ := testsetup()
	const n = 200
	seedMempool(n)
	common.RawRequest = noRefreshExpected

	var wg sync.WaitGroup
	streams := make([]*testmempooltx, 8)
	errs := make([]error, len(streams))
	for i := range streams {
		streams[i] = &testmempooltx{}
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			errs[i] = lwd.GetMempoolTx(&walletrpc.GetMempoolTxRequest{}, streams[i])
		}(i)
	}
	wg.Wait()
	for i, s := range streams {
		if errs[i] != nil {
			t.Fatal(errs[i])
		}
		if s.sent != n {
			t.Fatalf("caller %d received %d transactions, want %d", i, s.sent, n)
		}
	}
}
