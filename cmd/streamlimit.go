// Copyright (c) 2019-present The Zcash developers
// Distributed under the MIT software license, see the accompanying
// file COPYING or https://www.opensource.org/licenses/mit-license.php .
package cmd

import (
	"net"
	"sync"

	"github.com/zcash/lightwalletd/walletrpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"
)

// streamLimitInterceptor bounds active streaming handlers across connections.
// Block-range handlers share a separate limit in the service.
// A zero limit disables that particular cap.
func streamLimitInterceptor(max, maxPerIP uint64) grpc.StreamServerInterceptor {
	var mu sync.Mutex
	var active uint64
	activeByIP := make(map[string]uint64)

	return func(srv any, stream grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		if max == 0 && maxPerIP == 0 {
			return handler(srv, stream)
		}
		switch info.FullMethod {
		case walletrpc.CompactTxStreamer_GetBlockRange_FullMethodName,
			walletrpc.CompactTxStreamer_GetBlockRangeNullifiers_FullMethodName:
			return handler(srv, stream)
		}

		ctx := stream.Context()
		if err := ctx.Err(); err != nil {
			return err
		}

		var ip string
		if maxPerIP > 0 {
			if remote, ok := peer.FromContext(ctx); ok {
				if tcp, ok := remote.Addr.(*net.TCPAddr); ok {
					ip = tcp.IP.String()
				}
			}
		}

		mu.Lock()
		if (max > 0 && active >= max) || (ip != "" && activeByIP[ip] >= maxPerIP) {
			mu.Unlock()
			// Wallet clients can retry Unavailable with backoff.
			return status.Error(codes.Unavailable, "too many concurrent streaming requests; retry shortly")
		}
		active++
		if ip != "" {
			activeByIP[ip]++
		}
		mu.Unlock()

		defer func() {
			mu.Lock()
			defer mu.Unlock()
			active--
			if ip != "" {
				activeByIP[ip]--
				if activeByIP[ip] == 0 {
					delete(activeByIP, ip)
				}
			}
		}()
		return handler(srv, stream)
	}
}
