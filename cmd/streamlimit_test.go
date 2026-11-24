// Copyright (c) 2019-present The Zcash developers
// Distributed under the MIT software license, see the accompanying
// file COPYING or https://www.opensource.org/licenses/mit-license.php .
package cmd

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/zcash/lightwalletd/common"
	"github.com/zcash/lightwalletd/walletrpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
)

type holdStreamStub struct {
	walletrpc.UnimplementedCompactTxStreamerServer
}

func (holdStreamStub) GetMempoolStream(_ *walletrpc.Empty, stream walletrpc.CompactTxStreamer_GetMempoolStreamServer) error {
	if err := stream.Send(&walletrpc.RawTransaction{}); err != nil {
		return err
	}
	<-stream.Context().Done()
	return stream.Context().Err()
}

func (holdStreamStub) GetBlockRange(_ *walletrpc.BlockRange, stream walletrpc.CompactTxStreamer_GetBlockRangeServer) error {
	if err := stream.Send(&walletrpc.CompactBlock{}); err != nil {
		return err
	}
	<-stream.Context().Done()
	return stream.Context().Err()
}

func (s holdStreamStub) GetBlockRangeNullifiers(span *walletrpc.BlockRange, stream walletrpc.CompactTxStreamer_GetBlockRangeNullifiersServer) error {
	return s.GetBlockRange(span, stream)
}

func startStreamLimitTestServer(t *testing.T, opts common.Options) func() walletrpc.CompactTxStreamerClient {
	t.Helper()
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := grpc.NewServer(serverOptions(&opts)...)
	walletrpc.RegisterCompactTxStreamerServer(server, holdStreamStub{})
	go server.Serve(listener)
	t.Cleanup(server.Stop)

	return func() walletrpc.CompactTxStreamerClient {
		t.Helper()
		conn, err := grpc.NewClient(listener.Addr().String(),
			grpc.WithTransportCredentials(insecure.NewCredentials()))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { conn.Close() })
		return walletrpc.NewCompactTxStreamerClient(conn)
	}
}

// Receiving the first message proves the stream entered its handler.
func startMempoolStream(ctx context.Context, client walletrpc.CompactTxStreamerClient) error {
	stream, err := client.GetMempoolStream(ctx, &walletrpc.Empty{})
	if err != nil {
		return err
	}
	_, err = stream.Recv()
	return err
}

func TestServerOptionsLimitStreamingRPCAcrossConnections(t *testing.T) {
	for _, tt := range []struct {
		name string
		opts common.Options
	}{
		{"server-wide cap", common.Options{MaxStreamRPCs: 1}},
		{"per-IP cap", common.Options{MaxStreamRPCs: 2, MaxStreamRPCsPerIP: 1}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			client := startStreamLimitTestServer(t, tt.opts)
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			firstCtx, cancelFirst := context.WithCancel(ctx)
			defer cancelFirst()
			if err := startMempoolStream(firstCtx, client()); err != nil {
				t.Fatalf("first stream did not start: %v", err)
			}

			second := client()
			if err := startMempoolStream(ctx, second); status.Code(err) != codes.Unavailable {
				t.Fatalf("second connection got %v, want Unavailable", err)
			}

			cancelFirst()
			// Cancellation propagates asynchronously to the server handler.
			for {
				err := startMempoolStream(ctx, second)
				if err == nil {
					return
				}
				if status.Code(err) != codes.Unavailable {
					t.Fatalf("stream after cancellation got %v, want success", err)
				}
				select {
				case <-ctx.Done():
					t.Fatal("canceled stream did not free its slot")
				case <-time.After(10 * time.Millisecond):
				}
			}
		})
	}
}

func TestServerOptionsExemptBlockRangeFromStreamLimits(t *testing.T) {
	for _, tt := range []struct {
		name string
		opts common.Options
	}{
		{"server-wide cap", common.Options{MaxStreamRPCs: 1}},
		{"per-IP cap", common.Options{MaxStreamRPCsPerIP: 1}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			client := startStreamLimitTestServer(t, tt.opts)
			blockClient := client()
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()

			startBlockStreams := func(when string) {
				t.Helper()
				for _, rpc := range []struct {
					name string
					open func(context.Context, *walletrpc.BlockRange, ...grpc.CallOption) (grpc.ServerStreamingClient[walletrpc.CompactBlock], error)
				}{
					{"GetBlockRange", blockClient.GetBlockRange},
					{"GetBlockRangeNullifiers", blockClient.GetBlockRangeNullifiers},
				} {
					stream, err := rpc.open(ctx, &walletrpc.BlockRange{})
					if err == nil {
						_, err = stream.Recv()
					}
					if err != nil {
						t.Fatalf("%s did not start %s: %v", rpc.name, when, err)
					}
				}
			}

			// Both block-range handlers stay open until ctx is canceled.
			startBlockStreams("before opening a mempool stream")
			if err := startMempoolStream(ctx, client()); err != nil {
				t.Fatalf("mempool stream did not start with block-range streams active: %v", err)
			}
			startBlockStreams("while the mempool stream was active")
			if err := startMempoolStream(ctx, client()); status.Code(err) != codes.Unavailable {
				t.Fatalf("second mempool stream got %v, want Unavailable", err)
			}
		})
	}
}
