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

type panicStub struct {
	walletrpc.UnimplementedCompactTxStreamerServer
}

func (panicStub) GetLightdInfo(context.Context, *walletrpc.Empty) (*walletrpc.LightdInfo, error) {
	var b []byte
	_ = b[0]
	return nil, nil
}

func (panicStub) GetBlockRange(*walletrpc.BlockRange, walletrpc.CompactTxStreamer_GetBlockRangeServer) error {
	panic("stream handler panic")
}

// A handler panic fails that request and leaves the server running.
func TestServerOptionsRecoverHandlerPanic(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := grpc.NewServer(serverOptions(&common.Options{})...)
	walletrpc.RegisterCompactTxStreamerServer(server, panicStub{})
	go server.Serve(listener)
	defer server.Stop()
	conn, err := grpc.NewClient(listener.Addr().String(),
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	client := walletrpc.NewCompactTxStreamerClient(conn)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err = client.GetLightdInfo(ctx, &walletrpc.Empty{})
	if status.Code(err) != codes.Internal {
		t.Fatalf("unary panic returned %v, want Internal", err)
	}
	stream, err := client.GetBlockRange(ctx, &walletrpc.BlockRange{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := stream.Recv(); status.Code(err) != codes.Internal {
		t.Fatalf("stream panic returned %v, want Internal", err)
	}
	// The server is still serving.
	_, err = client.GetLatestBlock(ctx, &walletrpc.ChainSpec{})
	if status.Code(err) != codes.Unimplemented {
		t.Fatalf("call after panic returned %v, want Unimplemented", err)
	}
}
