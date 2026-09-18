// Copyright (c) 2019-present The Zcash developers
// Distributed under the MIT software license, see the accompanying
// file COPYING or https://www.opensource.org/licenses/mit-license.php .
package frontend

import (
	"context"
	"errors"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// shortBlockRangeWait shrinks the queue cap so a test that means to wait it
// out finishes promptly.
func shortBlockRangeWait(t *testing.T, d time.Duration) {
	t.Helper()
	saved := blockRangeWait
	blockRangeWait = d
	t.Cleanup(func() { blockRangeWait = saved })
}

func newLimited(t *testing.T, permits uint64) *lwdStreamer {
	t.Helper()
	s, err := NewLwdStreamer(nil, "main", false, permits)
	if err != nil {
		t.Fatal(err)
	}
	return s.(*lwdStreamer)
}

func TestBlockRangeLimitBoundsConcurrency(t *testing.T) {
	shortBlockRangeWait(t, 50*time.Millisecond)
	s := newLimited(t, 2)

	first, err := s.acquireBlockRange(context.Background())
	if err != nil {
		t.Fatalf("first acquire: %v", err)
	}
	if _, err := s.acquireBlockRange(context.Background()); err != nil {
		t.Fatalf("second acquire: %v", err)
	}
	// The permits are gone, so a third request must be turned away.
	if _, err := s.acquireBlockRange(context.Background()); status.Code(err) != codes.Unavailable {
		t.Fatalf("third acquire = %v, want Unavailable", err)
	}
	// Releasing one lets the next request through.
	first()
	if _, err := s.acquireBlockRange(context.Background()); err != nil {
		t.Fatalf("acquire after release: %v", err)
	}
}

func TestBlockRangeLimitServesQueuedRequest(t *testing.T) {
	shortBlockRangeWait(t, 2*time.Second)
	s := newLimited(t, 1)

	release, err := s.acquireBlockRange(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	queued := make(chan error, 1)
	go func() {
		_, err := s.acquireBlockRange(context.Background())
		queued <- err
	}()
	// Hand the permit back while the second request is still waiting for it.
	time.Sleep(100 * time.Millisecond)
	release()
	select {
	case err := <-queued:
		if err != nil {
			t.Fatalf("queued request = %v, want it to be served", err)
		}
	case <-time.After(time.Second):
		t.Fatal("queued request was never served")
	}
}

func TestBlockRangeLimitClientCancelIsNotUnavailable(t *testing.T) {
	shortBlockRangeWait(t, time.Minute)
	s := newLimited(t, 1)

	if _, err := s.acquireBlockRange(context.Background()); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	waited := make(chan error, 1)
	go func() {
		_, err := s.acquireBlockRange(ctx)
		waited <- err
	}()
	time.Sleep(100 * time.Millisecond)
	cancel()
	select {
	case err := <-waited:
		// The client gave up; that is not the server refusing service.
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("cancelled request = %v (code %v), want context.Canceled",
				err, status.Code(err))
		}
	case <-time.After(2 * time.Second):
		t.Fatal("cancelled request did not return")
	}
}

func TestBlockRangeLimitHonorsEarlierClientDeadline(t *testing.T) {
	shortBlockRangeWait(t, time.Minute)
	s := newLimited(t, 1)

	if _, err := s.acquireBlockRange(context.Background()); err != nil {
		t.Fatal(err)
	}
	// The client's own deadline is far shorter than our queue cap, so it wins.
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := s.acquireBlockRange(ctx)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("acquire = %v, want DeadlineExceeded", err)
	}
	if waited := time.Since(start); waited > 5*time.Second {
		t.Fatalf("waited %v, should have stopped at the client's deadline", waited)
	}
}

func TestBlockRangeLimitZeroMeansUnlimited(t *testing.T) {
	shortBlockRangeWait(t, 50*time.Millisecond)
	s := newLimited(t, 0)
	if s.permits != nil {
		t.Fatal("zero should leave the limiter off entirely")
	}
	for i := 0; i < 1000; i++ {
		if _, err := s.acquireBlockRange(context.Background()); err != nil {
			t.Fatalf("acquire %d: %v", i, err)
		}
	}
}
