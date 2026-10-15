// Copyright (c) 2019-present The Zcash developers
// Distributed under the MIT software license, see the accompanying
// file COPYING or https://www.opensource.org/licenses/mit-license.php .
package common

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/zcash/lightwalletd/walletrpc"
)

func stableMempoolGlobals() {
	Time.Now = time.Now
	Time.After = time.After
	g_lastBlockChainInfo = &ZcashdRpcReplyGetblockchaininfo{BestBlockHash: "stable-hash"}
}

func stableTip() json.RawMessage {
	r, _ := json.Marshal(&ZcashdRpcReplyGetblockchaininfo{BestBlockHash: "stable-hash", Blocks: 100})
	return r
}

// A client that cancels while its refresh is fetching a transaction must not
// hide that transaction from later callers (GHSA-pj5c-jx29-gmxr).
func TestMempoolCancelledRefreshDoesNotHideTx(t *testing.T) {
	defer resetGlobals()
	stableMempoolGlobals()
	attackerCtx, cancelAttacker := context.WithCancel(context.Background())
	RawRequest = func(ctx context.Context, method string, params []json.RawMessage) (json.RawMessage, error) {
		switch method {
		case "getblockchaininfo":
			return stableTip(), nil
		case "getrawmempool":
			return json.RawMessage(`["victim"]`), nil
		case "getrawtransaction":
			if ctx == attackerCtx {
				cancelAttacker()
				return nil, ctx.Err()
			}
			return json.RawMessage(`{"hex": "aabb"}`), nil
		}
		return nil, errors.New("unexpected RPC: " + method)
	}

	err := GetMempool(attackerCtx, func(*walletrpc.RawTransaction) error { return nil })
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("attacker GetMempool returned %v, want context.Canceled", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	var got int
	GetMempool(ctx, func(*walletrpc.RawTransaction) error {
		got++
		cancel()
		return nil
	})
	if got != 1 {
		t.Fatalf("honest client received %d transactions, want 1", got)
	}
}

// While a refresh is slow, other callers don't start their own, and both they
// and the refreshing caller return promptly when cancelled
// (GHSA-hp3q-vfr4-v8m6).
func TestMempoolSlowRefreshDoesNotBlockCancel(t *testing.T) {
	defer resetGlobals()
	stableMempoolGlobals()
	inRPC := make(chan struct{}, 10)
	RawRequest = func(ctx context.Context, method string, params []json.RawMessage) (json.RawMessage, error) {
		if method != "getblockchaininfo" {
			return nil, errors.New("unexpected RPC: " + method)
		}
		inRPC <- struct{}{}
		<-ctx.Done()
		return nil, ctx.Err()
	}

	refresherCtx, cancelRefresher := context.WithCancel(context.Background())
	waiterCtx, cancelWaiter := context.WithCancel(context.Background())
	refresherDone := make(chan error, 1)
	waiterDone := make(chan error, 1)
	noSend := func(*walletrpc.RawTransaction) error { return nil }
	go func() { refresherDone <- GetMempool(refresherCtx, noSend) }()
	<-inRPC
	go func() { waiterDone <- GetMempool(waiterCtx, noSend) }()

	// The waiter loops on the cached state rather than refreshing too.
	time.Sleep(500 * time.Millisecond)
	if n := len(inRPC); n != 0 {
		t.Fatalf("%d more refreshes started while one was in progress, want 0", n)
	}

	for _, c := range []struct {
		name   string
		cancel context.CancelFunc
		done   chan error
	}{
		{"waiter", cancelWaiter, waiterDone},
		{"refresher", cancelRefresher, refresherDone},
	} {
		c.cancel()
		select {
		case err := <-c.done:
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("%s returned %v, want context.Canceled", c.name, err)
			}
		case <-time.After(2 * time.Second):
			t.Fatalf("%s did not return within 2s of cancel", c.name)
		}
	}
}
