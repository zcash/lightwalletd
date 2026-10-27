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

	"github.com/zcash/lightwalletd/common"
	"github.com/zcash/lightwalletd/walletrpc"
)

// sendForeverStub streams 1 MiB blocks until Send fails, then reports how
// the stream ended.
type sendForeverStub struct {
	walletrpc.UnimplementedCompactTxStreamerServer
	ended chan error
}

func (s sendForeverStub) GetBlockRange(_ *walletrpc.BlockRange, stream walletrpc.CompactTxStreamer_GetBlockRangeServer) error {
	block := &walletrpc.CompactBlock{Hash: make([]byte, 1<<20)}
	for {
		if err := stream.Send(block); err != nil {
			s.ended <- err
			return err
		}
	}
}

func TestServerOptionsEndStreamWhenClientStopsReading(t *testing.T) {
	saved := streamSendTimeout
	streamSendTimeout = 200 * time.Millisecond
	defer func() { streamSendTimeout = saved }()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := grpc.NewServer(serverOptions(&common.Options{})...)
	stub := sendForeverStub{ended: make(chan error, 1)}
	walletrpc.RegisterCompactTxStreamerServer(server, stub)
	go server.Serve(listener)
	defer server.Stop()
	conn, err := grpc.NewClient(listener.Addr().String(),
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	stream, err := walletrpc.NewCompactTxStreamerClient(conn).GetBlockRange(ctx, &walletrpc.BlockRange{})
	if err != nil {
		t.Fatal(err)
	}
	// Never read, so the server's sends stall on flow control.
	select {
	case err := <-stub.ended:
		if status.Code(err) != codes.DeadlineExceeded {
			t.Fatalf("handler ended with %v, want DeadlineExceeded", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("handler did not return after the client stopped reading")
	}
	// The client learns why once it reads again.
	for {
		if _, err := stream.Recv(); err != nil {
			if status.Code(err) != codes.DeadlineExceeded {
				t.Fatalf("client got %v, want DeadlineExceeded", err)
			}
			return
		}
	}
}

func TestConcurrencyFlagsRejectNegative(t *testing.T) {
	for _, name := range []string{"grpc-max-concurrent-streams", "max-concurrent-blockrange"} {
		if err := rootCmd.Flags().Set(name, "-1"); err == nil {
			t.Errorf("--%s=-1 was accepted", name)
		}
	}
}
