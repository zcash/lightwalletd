// Copyright (c) 2019-present The Zcash developers
// Distributed under the MIT software license, see the accompanying
// file COPYING or https://www.opensource.org/licenses/mit-license.php .
package frontend

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/zcash/lightwalletd/common"
	"github.com/zcash/lightwalletd/walletrpc"
)

func TestByteBudget(t *testing.T) {
	if release, err := (*byteBudget)(nil).reserve(1 << 30); err != nil {
		t.Fatal("a nil budget must not limit:", err)
	} else {
		release()
	}

	b := newByteBudget(3_000_000)
	r1, err := b.reserve(2_000_000)
	if err != nil {
		t.Fatal(err)
	}
	r2, err := b.reserve(1_000_000)
	if err != nil {
		t.Fatal("reserving exactly up to the limit failed:", err)
	}
	if _, err := b.reserve(1); status.Code(err) != codes.Unavailable {
		t.Fatalf("reserving past the limit: got %v, expected Unavailable", err)
	}
	r1()
	r3, err := b.reserve(2_000_000)
	if err != nil {
		t.Fatal("released bytes were not returned to the budget:", err)
	}
	r2()
	r3()
	if b.used != 0 {
		t.Fatalf("%d bytes still reserved after every release", b.used)
	}

	// Below the largest transaction the budget is raised to it, or a
	// maximum-size transaction could never be sent.
	if _, err := newByteBudget(1).reserve(maxRawTxSize); err != nil {
		t.Fatal("a maximum-size transaction was refused by a small budget:", err)
	}
}

func TestSendTransactionByteBudget(t *testing.T) {
	testT = t
	defer resetGlobals()
	s, _ := NewLwdStreamer(nil, "main", false, 0, maxRawTxSize)
	lwd := s.(*lwdStreamer)

	var mu sync.Mutex
	calls := 0
	entered := make(chan struct{}, 4)
	release := make(chan struct{})
	common.RawRequest = func(_ context.Context, method string, _ []json.RawMessage) (json.RawMessage, error) {
		mu.Lock()
		calls++
		mu.Unlock()
		entered <- struct{}{}
		<-release
		return json.RawMessage(`"txid"`), nil
	}
	send := func() error {
		_, err := lwd.SendTransaction(context.Background(),
			&walletrpc.RawTransaction{Data: make([]byte, maxRawTxSize/2)})
		return err
	}

	// Two half-size transactions fill the budget and reach the backend.
	done := make(chan error, 2)
	for range 2 {
		go func() { done <- send() }()
	}
	<-entered
	<-entered
	// A third is refused at once, without reaching the backend.
	third := make(chan error, 1)
	go func() { third <- send() }()
	select {
	case err := <-third:
		if status.Code(err) != codes.Unavailable {
			close(release)
			t.Fatalf("got %v, expected Unavailable while the budget is full", err)
		}
	case <-time.After(2 * time.Second):
		close(release)
		t.Fatal("a third send was not refused while the budget was full")
	}
	close(release)
	for range 2 {
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	}
	if calls != 2 {
		t.Fatalf("the backend was called %d times, expected 2", calls)
	}
	// Once they finish, the budget is free again.
	if err := send(); err != nil {
		t.Fatal("budget not returned after the sends finished:", err)
	}
}

// A send that fails at the backend must still give its bytes back.
func TestSendTransactionByteBudgetReleasedOnError(t *testing.T) {
	testT = t
	defer resetGlobals()
	s, _ := NewLwdStreamer(nil, "main", false, 0, maxRawTxSize)
	lwd := s.(*lwdStreamer)
	common.RawRequest = func(context.Context, string, []json.RawMessage) (json.RawMessage, error) {
		return nil, errors.New("-26: rejected")
	}
	for range 3 {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		_, err := lwd.SendTransaction(ctx, &walletrpc.RawTransaction{Data: make([]byte, maxRawTxSize)})
		cancel()
		if status.Code(err) == codes.Unavailable {
			t.Fatal("a failed send kept its share of the budget")
		}
	}
	if lwd.sendTxBudget.used != 0 {
		t.Fatalf("%d bytes still reserved", lwd.sendTxBudget.used)
	}
}
