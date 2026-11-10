// Copyright (c) 2019-present The Zcash developers
// Distributed under the MIT software license, see the accompanying
// file COPYING or https://www.opensource.org/licenses/mit-license.php .
package common

import (
	"context"
	"encoding/json"
	"errors"
	"sync/atomic"
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

// waitRefreshIdle waits for a detached refresh to finish, so it can't touch
// the globals after the test resets them.
func waitRefreshIdle(t *testing.T) {
	t.Helper()
	for range 250 {
		g_lock.Lock()
		busy := g_refreshing
		g_lock.Unlock()
		if !busy {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("refresh still running")
}

// collectOne streams the mempool until one transaction arrives or 2s pass.
func collectOne() int {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	var got int
	GetMempool(ctx, func(*walletrpc.RawTransaction) error {
		got++
		cancel()
		return nil
	})
	return got
}

// A refresh that fails part way, here by timing out, must not hide the
// transaction it was fetching from later callers (GHSA-pj5c-jx29-gmxr).
func TestMempoolFailedRefreshDoesNotHideTx(t *testing.T) {
	defer resetGlobals()
	defer func(d time.Duration) { mempoolRefreshTimeout = d }(mempoolRefreshTimeout)
	stableMempoolGlobals()
	mempoolRefreshTimeout = 100 * time.Millisecond
	first := true
	RawRequest = func(ctx context.Context, method string, params []json.RawMessage) (json.RawMessage, error) {
		switch method {
		case "getblockchaininfo":
			return stableTip(), nil
		case "getrawmempool":
			return json.RawMessage(`["victim"]`), nil
		case "getrawtransaction":
			if first {
				first = false
				<-ctx.Done()
				return nil, ctx.Err()
			}
			return json.RawMessage(`{"hex": "aabb"}`), nil
		}
		return nil, errors.New("unexpected RPC: " + method)
	}

	err := GetMempool(context.Background(), func(*walletrpc.RawTransaction) error { return nil })
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("first GetMempool returned %v, want context.DeadlineExceeded", err)
	}
	if got := collectOne(); got != 1 {
		t.Fatalf("next client received %d transactions, want 1", got)
	}
	waitRefreshIdle(t)
}

// A client that cancels during its refresh doesn't stop it, so cancelling
// repeatedly can't keep the mempool stale (GHSA-pj5c-jx29-gmxr).
func TestMempoolCancelDoesNotAbortRefresh(t *testing.T) {
	defer resetGlobals()
	stableMempoolGlobals()
	attackerCtx, cancelAttacker := context.WithCancel(context.Background())
	release := make(chan struct{})
	var mempoolCalls atomic.Int32
	RawRequest = func(ctx context.Context, method string, params []json.RawMessage) (json.RawMessage, error) {
		switch method {
		case "getblockchaininfo":
			return stableTip(), nil
		case "getrawmempool":
			mempoolCalls.Add(1)
			return json.RawMessage(`["victim"]`), nil
		case "getrawtransaction":
			cancelAttacker()
			<-release
			return json.RawMessage(`{"hex": "aabb"}`), nil
		}
		return nil, errors.New("unexpected RPC: " + method)
	}

	err := GetMempool(attackerCtx, func(*walletrpc.RawTransaction) error { return nil })
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("attacker GetMempool returned %v, want context.Canceled", err)
	}
	close(release)
	waitRefreshIdle(t)
	if got := collectOne(); got != 1 {
		t.Fatalf("honest client received %d transactions, want 1", got)
	}
	if n := mempoolCalls.Load(); n != 1 {
		t.Fatalf("getrawmempool called %d times, want 1", n)
	}
}

// A transaction that fails to parse is skipped once, not retried on every
// refresh, and doesn't hold up the ones after it.
func TestMempoolUnparseableTxIsSkipped(t *testing.T) {
	defer resetGlobals()
	stableMempoolGlobals()
	var badCalls atomic.Int32
	RawRequest = func(ctx context.Context, method string, params []json.RawMessage) (json.RawMessage, error) {
		switch method {
		case "getblockchaininfo":
			return stableTip(), nil
		case "getrawmempool":
			return json.RawMessage(`["bad","good"]`), nil
		case "getrawtransaction":
			if string(params[0]) == `"bad"` {
				badCalls.Add(1)
				return json.RawMessage(`{"hex": "zz"}`), nil
			}
			return json.RawMessage(`{"hex": "aabb"}`), nil
		}
		return nil, errors.New("unexpected RPC: " + method)
	}

	if got := collectOne(); got != 1 {
		t.Fatalf("client received %d transactions, want 1", got)
	}
	waitRefreshIdle(t)
	g_refreshing = true
	if err := refreshMempool(context.Background()); err != nil {
		t.Fatal(err)
	}
	if n := badCalls.Load(); n != 1 {
		t.Fatalf("unparseable transaction fetched %d times, want 1", n)
	}
}

// A successful refresh holds off further refreshes for 2s.
func TestMempoolRefreshInterval(t *testing.T) {
	defer resetGlobals()
	stableMempoolGlobals()
	var mempoolCalls atomic.Int32
	RawRequest = func(ctx context.Context, method string, params []json.RawMessage) (json.RawMessage, error) {
		switch method {
		case "getblockchaininfo":
			return stableTip(), nil
		case "getrawmempool":
			mempoolCalls.Add(1)
			return json.RawMessage(`[]`), nil
		}
		return nil, errors.New("unexpected RPC: " + method)
	}
	for range 2 {
		ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
		GetMempool(ctx, func(*walletrpc.RawTransaction) error { return nil })
		cancel()
	}
	if n := mempoolCalls.Load(); n != 1 {
		t.Fatalf("getrawmempool called %d times within 2s, want 1", n)
	}
}

// While a refresh is slow, other callers don't start their own, and both they
// and the refreshing caller return promptly when cancelled
// (GHSA-hp3q-vfr4-v8m6).
func TestMempoolSlowRefreshDoesNotBlockCancel(t *testing.T) {
	defer resetGlobals()
	defer func(d time.Duration) { mempoolRefreshTimeout = d }(mempoolRefreshTimeout)
	stableMempoolGlobals()
	mempoolRefreshTimeout = time.Second
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
	waitRefreshIdle(t)
}

// A stream on an old tip returns once another stream has seen the new block,
// without starting a refresh of its own.
func TestMempoolStaleStreamDoesNotRefresh(t *testing.T) {
	defer resetGlobals()
	stableMempoolGlobals()
	g_lastTime = time.Now()
	var calls atomic.Int32
	RawRequest = func(ctx context.Context, method string, params []json.RawMessage) (json.RawMessage, error) {
		calls.Add(1)
		return nil, errors.New("unexpected RPC: " + method)
	}
	done := make(chan error, 1)
	go func() { done <- GetMempool(context.Background(), func(*walletrpc.RawTransaction) error { return nil }) }()
	time.Sleep(50 * time.Millisecond)

	// What another stream's refresh does on seeing a new block.
	g_lock.Lock()
	g_lastBlockChainInfo = &ZcashdRpcReplyGetblockchaininfo{BestBlockHash: "new-hash"}
	g_lastTime = time.Time{}
	g_lock.Unlock()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("stale stream returned %v, want nil", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("stale stream did not return")
	}
	if n := calls.Load(); n != 0 {
		t.Fatalf("stale stream made %d backend calls, want 0", n)
	}
}
