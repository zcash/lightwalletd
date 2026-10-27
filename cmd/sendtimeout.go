// Copyright (c) 2019-present The Zcash developers
// Distributed under the MIT software license, see the accompanying
// file COPYING or https://www.opensource.org/licenses/mit-license.php .
package cmd

import (
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// streamSendTimeout ends a stream whose client hasn't accepted a message in
// this long. A variable so tests can shorten it.
var streamSendTimeout = 60 * time.Second

var errSendStalled = status.Error(codes.DeadlineExceeded,
	"client stopped reading the stream")

// sendTimeoutStream fails SendMsg when the client stops reading, so the
// handler returns and frees whatever it holds.
type sendTimeoutStream struct {
	grpc.ServerStream
	timeout time.Duration
	stalled bool
}

func (s *sendTimeoutStream) SendMsg(m any) error {
	if s.stalled {
		return errSendStalled
	}
	// The pending send unblocks once the handler returns and the stream ends.
	done := make(chan error, 1)
	go func() { done <- s.ServerStream.SendMsg(m) }()
	timer := time.NewTimer(s.timeout)
	defer timer.Stop()
	select {
	case err := <-done:
		return err
	case <-timer.C:
		s.stalled = true
		return errSendStalled
	}
}

func sendTimeoutInterceptor(timeout time.Duration) grpc.StreamServerInterceptor {
	return func(srv any, ss grpc.ServerStream, _ *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		return handler(srv, &sendTimeoutStream{ServerStream: ss, timeout: timeout})
	}
}
