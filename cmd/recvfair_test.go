// Copyright (c) 2019-present The Zcash developers
// Distributed under the MIT software license, see the accompanying
// file COPYING or https://www.opensource.org/licenses/mit-license.php .
package cmd

import (
	"context"
	"net"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"

	"github.com/zcash/lightwalletd/walletrpc"
)

// startFairServer returns a function that opens a new connection per call.
func startFairServer(t *testing.T, opts []grpc.ServerOption) func() *grpc.ClientConn {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := grpc.NewServer(opts...)
	walletrpc.RegisterCompactTxStreamerServer(server, recvLimitStub{})
	go server.Serve(listener)
	t.Cleanup(server.Stop)
	return func() *grpc.ClientConn {
		conn, err := grpc.NewClient(listener.Addr().String(),
			grpc.WithTransportCredentials(insecure.NewCredentials()))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { conn.Close() })
		return conn
	}
}

func waitInUse(t *testing.T, p *recvPool, want int) {
	t.Helper()
	for range 250 {
		p.mu.Lock()
		n := p.inUse
		p.mu.Unlock()
		if n == want {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("pool never reached %d slots in use", want)
}

// The reviewer's attack, 32 stalled streams on one connection.
func TestRecvLimiterOneConnCantLockOut(t *testing.T) {
	l := newRecvLimiter(32)
	dial := startFairServer(t, l.serverOptions())
	attacker := dial()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var stalled []grpc.ClientStream
	for range 32 {
		stalled = append(stalled, openStalled(t, ctx, attacker))
	}
	waitInUse(t, l.slots, 32)

	if got := callCode(dial()); got != codes.Unimplemented {
		t.Fatalf("other client got %v, want Unimplemented", got)
	}
	// The attacker's oldest stream was the one given up.
	if err := stalled[0].RecvMsg(&walletrpc.BlockID{}); status.Code(err) != codes.Canceled {
		t.Fatalf("evicted stream ended with %v, want Canceled", err)
	}
}

func TestRecvLimiterOneConnCantLockOutClientStreams(t *testing.T) {
	l := newRecvLimiter(32)
	dial := startFairServer(t, l.serverOptions())
	attacker := walletrpc.NewCompactTxStreamerClient(dial())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	for range 32 {
		s, err := attacker.GetTaddressBalanceStream(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if err := s.Send(&walletrpc.Address{}); err != nil {
			t.Fatal(err)
		}
	}
	waitInUse(t, l.streams, 32)

	tctx, tcancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer tcancel()
	s, err := walletrpc.NewCompactTxStreamerClient(dial()).GetTaddressBalanceStream(tctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CloseAndRecv(); err != nil {
		t.Fatalf("other client's stream got %v", err)
	}
}

// What's left: one stalled stream on each of 32 connections still locks out.
func TestRecvLimiterManyConnsStillLockOut(t *testing.T) {
	l := newRecvLimiter(32)
	dial := startFairServer(t, l.serverOptions())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	for range 32 {
		openStalled(t, ctx, dial())
	}
	waitInUse(t, l.slots, 32)
	if got := callCode(dial()); got != codes.Unavailable {
		t.Fatalf("other client got %v, want Unavailable", got)
	}
}
