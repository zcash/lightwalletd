// Copyright (c) 2019-present The Zcash developers
// Distributed under the MIT software license, see the accompanying
// file COPYING or https://www.opensource.org/licenses/mit-license.php .
package cmd

import (
	"context"
	"net"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"

	"github.com/zcash/lightwalletd/common"
	"github.com/zcash/lightwalletd/walletrpc"
)

// balanceRecorder records how many addresses each GetTaddressBalance received.
type balanceRecorder struct {
	walletrpc.UnimplementedCompactTxStreamerServer
	seen chan int
}

func (r *balanceRecorder) GetTaddressBalance(_ context.Context, a *walletrpc.AddressList) (*walletrpc.Balance, error) {
	r.seen <- len(a.Addresses)
	return &walletrpc.Balance{}, nil
}

func TestServerRefusesHugeRepeatedFieldsBeforeDecoding(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := grpc.NewServer(serverOptions(&common.Options{})...)
	recorder := &balanceRecorder{seen: make(chan int, 1)}
	walletrpc.RegisterCompactTxStreamerServer(server, recorder)
	go server.Serve(listener)
	defer server.Stop()

	conn, err := grpc.NewClient(listener.Addr().String(),
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	client := walletrpc.NewCompactTxStreamerClient(conn)

	// Many empty addresses, over the codec's bound but under maxRecvMsgSize.
	_, err = client.GetTaddressBalance(context.Background(),
		&walletrpc.AddressList{Addresses: make([]string, 100000)})
	if status.Code(err) != codes.Internal {
		t.Fatalf("got %v, expected the codec to refuse it", err)
	}
	select {
	case n := <-recorder.seen:
		t.Fatalf("the handler was reached with %d addresses", n)
	default:
	}

	// Over the handler's own cap but within the codec's bound: decoded and
	// passed on, so the handler can answer with its own InvalidArgument.
	if _, err := client.GetTaddressBalance(context.Background(),
		&walletrpc.AddressList{Addresses: make([]string, 15000)}); err != nil {
		t.Fatal(err)
	}
	if n := <-recorder.seen; n != 15000 {
		t.Fatalf("the handler saw %d addresses, expected 15000", n)
	}
}
