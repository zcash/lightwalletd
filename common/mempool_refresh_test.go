// Copyright (c) 2019-present The Zcash developers
// Distributed under the MIT software license, see the accompanying
// file COPYING or https://www.opensource.org/licenses/mit-license.php .
package common

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
)

// g_txidSeen and g_txList are shared by every GetMempoolStream client, and
// refreshMempoolTxns runs on whichever client's goroutine gets there first,
// with that client's context. What one refresh records must hold for all.

// mempoolRPC answers getrawmempool with txids and getrawtransaction with a
// transaction, unless fail returns an error for that txid.
func mempoolRPC(txids []string, fail func(ctx context.Context, txid string) error) func(context.Context, string, []json.RawMessage) (json.RawMessage, error) {
	return func(ctx context.Context, method string, params []json.RawMessage) (json.RawMessage, error) {
		if err := ctx.Err(); err != nil {
			// The real client aborts the HTTP request once ctx is done.
			return nil, err
		}
		switch method {
		case "getrawmempool":
			return json.Marshal(txids)
		case "getrawtransaction":
			var id string
			json.Unmarshal(params[0], &id)
			if err := fail(ctx, id); err != nil {
				return nil, err
			}
			return json.Marshal(map[string]string{"hex": "aabb"})
		}
		return nil, errors.New("unexpected method " + method)
	}
}

func TestMempoolRefreshAfterClientCancelFetchesRemainingTxns(t *testing.T) {
	defer resetGlobals()
	txids := []string{"tx-a", "tx-b", "tx-c"}

	// The client whose goroutine is refreshing disconnects after tx-a.
	ctx, cancel := context.WithCancel(context.Background())
	RawRequest = mempoolRPC(txids, func(_ context.Context, id string) error {
		if id == "tx-a" {
			cancel()
		}
		return nil
	})
	if err := refreshMempoolTxns(ctx); !errors.Is(err, context.Canceled) {
		t.Errorf("refresh with a cancelled client returned %v, want context.Canceled", err)
	}

	// The next refresh, on behalf of another client, must still fetch the
	// transactions the cancelled one never got to.
	RawRequest = mempoolRPC(txids, func(context.Context, string) error { return nil })
	if err := refreshMempoolTxns(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(g_txList) != len(txids) {
		t.Fatalf("mempool list has %d transactions, want %d", len(g_txList), len(txids))
	}
}

func TestMempoolRefreshRetriesTransientFetchFailure(t *testing.T) {
	defer resetGlobals()
	txids := []string{"tx-a", "tx-b", "tx-c"}

	failed := false
	RawRequest = mempoolRPC(txids, func(_ context.Context, id string) error {
		if id == "tx-b" && !failed {
			failed = true
			return errors.New("connection reset by peer")
		}
		return nil
	})
	if err := refreshMempoolTxns(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(g_txList) != 2 {
		t.Fatalf("first refresh has %d transactions, want 2", len(g_txList))
	}
	// tx-b is still in the mempool, so the next refresh fetches it.
	if err := refreshMempoolTxns(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(g_txList) != 3 {
		t.Fatalf("second refresh has %d transactions, want 3", len(g_txList))
	}
}

func TestMempoolRefreshDoesNotRefetchKnownTxns(t *testing.T) {
	defer resetGlobals()
	txids := []string{"tx-a", "tx-b"}
	fetches := 0
	RawRequest = mempoolRPC(txids, func(context.Context, string) error {
		fetches++
		return nil
	})
	for i := 0; i < 3; i++ {
		if err := refreshMempoolTxns(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if fetches != 2 || len(g_txList) != 2 {
		t.Fatalf("fetched %d times for %d transactions, want 2 and 2", fetches, len(g_txList))
	}
}
