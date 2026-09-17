// Copyright (c) 2019-present The Zcash developers
// Distributed under the MIT software license, see the accompanying
// file COPYING or https://www.opensource.org/licenses/mit-license.php .
package cmd

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// fakeStream stands in for a grpc.ServerStream whose RecvMsg blocks until the
// test decides a message (or EOF) arrives.
type fakeStream struct {
	grpc.ServerStream
	ctx  context.Context
	recv chan error
}

func (f *fakeStream) Context() context.Context { return f.ctx }
func (f *fakeStream) RecvMsg(any) error        { return <-f.recv }

func newFake() *fakeStream {
	return &fakeStream{ctx: context.Background(), recv: make(chan error, 1)}
}

// call runs the interceptor with the given timeout and returns what the wrapped
// RecvMsg produced, or a marker if the handler was never entered.
func recvUnder(t *testing.T, timeout time.Duration, drive func(*fakeStream)) error {
	t.Helper()
	fs := newFake()
	info := &grpc.StreamServerInfo{IsClientStream: true}
	got := make(chan error, 1)
	handler := func(_ any, ss grpc.ServerStream) error {
		go drive(fs)
		return ss.RecvMsg(nil)
	}
	go func() { got <- idleStreamInterceptor(timeout)(nil, fs, info, handler) }()
	select {
	case err := <-got:
		return err
	case <-time.After(5 * time.Second):
		t.Fatal("interceptor never returned")
		return nil
	}
}

func TestIdleStreamTimesOut(t *testing.T) {
	// Client sends nothing; the wrapped RecvMsg must fail with DeadlineExceeded.
	err := recvUnder(t, 50*time.Millisecond, func(*fakeStream) {})
	if status.Code(err) != codes.DeadlineExceeded {
		t.Fatalf("got %v, want DeadlineExceeded", err)
	}
}

func TestIdleStreamPassesMessageThrough(t *testing.T) {
	// A message arrives in time; RecvMsg must return it, not a timeout.
	err := recvUnder(t, 2*time.Second, func(fs *fakeStream) {
		time.Sleep(20 * time.Millisecond)
		fs.recv <- nil
	})
	if err != nil {
		t.Fatalf("got %v, want nil", err)
	}
}

func TestIdleStreamPassesEOF(t *testing.T) {
	err := recvUnder(t, 2*time.Second, func(fs *fakeStream) { fs.recv <- io.EOF })
	if !errors.Is(err, io.EOF) {
		t.Fatalf("got %v, want EOF", err)
	}
}

func TestIdleStreamDisabled(t *testing.T) {
	// Timeout 0 leaves the stream unwrapped, so a silent client is not cut off.
	fs := newFake()
	info := &grpc.StreamServerInfo{IsClientStream: true}
	handler := func(_ any, ss grpc.ServerStream) error {
		if _, wrapped := ss.(*idleStream); wrapped {
			t.Fatal("stream was wrapped despite timeout 0")
		}
		return nil
	}
	if err := idleStreamInterceptor(0)(nil, fs, info, handler); err != nil {
		t.Fatal(err)
	}
}

func TestIdleStreamServerStreamingUntouched(t *testing.T) {
	// A server-streaming RPC (client does not stream) must not be wrapped.
	fs := newFake()
	info := &grpc.StreamServerInfo{IsClientStream: false}
	handler := func(_ any, ss grpc.ServerStream) error {
		if _, wrapped := ss.(*idleStream); wrapped {
			t.Fatal("server-streaming RPC was wrapped")
		}
		return nil
	}
	if err := idleStreamInterceptor(time.Second)(nil, fs, info, handler); err != nil {
		t.Fatal(err)
	}
}

// The real GetTaddressBalanceStream handler turns any Recv error into
// codes.Internal. The interceptor must still surface DeadlineExceeded when it
// was the timeout that fired.
func TestIdleStreamOverridesHandlerInternal(t *testing.T) {
	fs := newFake()
	info := &grpc.StreamServerInfo{IsClientStream: true}
	handler := func(_ any, ss grpc.ServerStream) error {
		err := ss.RecvMsg(nil)
		if err != nil {
			return status.Errorf(codes.Internal, "wrapped: %s", err)
		}
		return nil
	}
	err := idleStreamInterceptor(50*time.Millisecond)(nil, fs, info, handler)
	if status.Code(err) != codes.DeadlineExceeded {
		t.Fatalf("got %v, want DeadlineExceeded", err)
	}
}
