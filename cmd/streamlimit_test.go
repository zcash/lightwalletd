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

func TestServerOptionsLimitStreamingRPCAcrossConnections(t *testing.T) {
	for _, tt := range []struct {
		name string
		opts common.Options
	}{
		{"server-wide cap", common.Options{MaxStreamRPCs: 1}},
		{"per-IP cap", common.Options{MaxStreamRPCs: 2, MaxStreamRPCsPerIP: 1}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			listener, err := net.Listen("tcp4", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			server := grpc.NewServer(serverOptions(&tt.opts)...)
			walletrpc.RegisterCompactTxStreamerServer(server, holdStreamStub{})
			go server.Serve(listener)
			t.Cleanup(server.Stop)

			client := func() walletrpc.CompactTxStreamerClient {
				t.Helper()
				conn, err := grpc.NewClient(listener.Addr().String(),
					grpc.WithTransportCredentials(insecure.NewCredentials()))
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { conn.Close() })
				return walletrpc.NewCompactTxStreamerClient(conn)
			}

			// Receiving the first message proves the stream entered its handler.
			start := func(ctx context.Context, c walletrpc.CompactTxStreamerClient) error {
				stream, err := c.GetMempoolStream(ctx, &walletrpc.Empty{})
				if err != nil {
					return err
				}
				_, err = stream.Recv()
				return err
			}

			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			firstCtx, cancelFirst := context.WithCancel(ctx)
			defer cancelFirst()
			if err := start(firstCtx, client()); err != nil {
				t.Fatalf("first stream did not start: %v", err)
			}

			second := client()
			if err := start(ctx, second); status.Code(err) != codes.Unavailable {
				t.Fatalf("second connection got %v, want Unavailable", err)
			}

			cancelFirst()
			// Cancellation propagates asynchronously to the server handler.
			for {
				err := start(ctx, second)
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
