// Copyright (c) 2019-present The Zcash developers
// Distributed under the MIT software license, see the accompanying
// file COPYING or https://www.opensource.org/licenses/mit-license.php .
package cmd

import (
	"sync/atomic"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// idleStreamInterceptor bounds how long a client-streaming handler will wait
// for the client to make progress. Without it, a client that opens a stream
// and sends nothing parks a handler goroutine until the process dies
// (GHSA-5xg7-x58j-m5vr).
func idleStreamInterceptor(timeout time.Duration) grpc.StreamServerInterceptor {
	return func(srv any, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		if timeout <= 0 || !info.IsClientStream {
			return handler(srv, ss)
		}
		w := &idleStream{ServerStream: ss, timeout: timeout}
		err := handler(srv, w)
		if w.timedOut.Load() {
			// Report the deadline directly; the handler turns any Recv error
			// into codes.Internal, which would mislabel a client that stalled.
			return status.Errorf(codes.DeadlineExceeded,
				"client stream idle longer than %s", timeout)
		}
		return err
	}
}

// idleStream fails a RecvMsg that the client leaves unanswered past the
// timeout, covering both the wait for the first message and the gap between
// messages.
type idleStream struct {
	grpc.ServerStream
	timeout  time.Duration
	timedOut atomic.Bool
}

func (s *idleStream) RecvMsg(m any) error {
	done := make(chan error, 1)
	go func() { done <- s.ServerStream.RecvMsg(m) }()
	timer := time.NewTimer(s.timeout)
	defer timer.Stop()
	select {
	case err := <-done:
		return err
	case <-timer.C:
		// The handler returns on this error, which tears down the stream and
		// unblocks the RecvMsg still running above.
		s.timedOut.Store(true)
		return status.Errorf(codes.DeadlineExceeded,
			"client stream idle longer than %s", s.timeout)
	case <-s.ServerStream.Context().Done():
		return s.ServerStream.Context().Err()
	}
}
