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
	"google.golang.org/grpc/reflection"
	reflectionv1 "google.golang.org/grpc/reflection/grpc_reflection_v1"
	"google.golang.org/grpc/status"

	"github.com/zcash/lightwalletd/common"
	"github.com/zcash/lightwalletd/walletrpc"
)

// recvLimitStub holds GetLatestBlock calls and client streams open until the
// client ends them.
type recvLimitStub struct {
	walletrpc.UnimplementedCompactTxStreamerServer
}

func (recvLimitStub) GetLatestBlock(ctx context.Context, _ *walletrpc.ChainSpec) (*walletrpc.BlockID, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

func (recvLimitStub) GetTaddressBalanceStream(s walletrpc.CompactTxStreamer_GetTaddressBalanceStreamServer) error {
	for {
		if _, err := s.Recv(); err != nil {
			return s.SendAndClose(&walletrpc.Balance{})
		}
	}
}

func startRecvLimitServer(t *testing.T, l *recvLimiter) *grpc.ClientConn {
	return startStubServer(t, l.serverOptions())
}

func startStubServer(t *testing.T, opts []grpc.ServerOption) *grpc.ClientConn {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := grpc.NewServer(opts...)
	walletrpc.RegisterCompactTxStreamerServer(server, recvLimitStub{})
	go server.Serve(listener)
	t.Cleanup(server.Stop)
	conn, err := grpc.NewClient(listener.Addr().String(),
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	return conn
}

// openStalled opens a unary-method stream whose request body never arrives.
func openStalled(t *testing.T, ctx context.Context, conn *grpc.ClientConn) grpc.ClientStream {
	t.Helper()
	stream, err := conn.NewStream(ctx, &grpc.StreamDesc{},
		"/cash.z.wallet.sdk.rpc.CompactTxStreamer/GetLatestBlock")
	if err != nil {
		t.Fatal(err)
	}
	return stream
}

// callCode makes a unary call, which the stub server answers Unimplemented
// once the request has been received.
func callCode(conn *grpc.ClientConn) codes.Code {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := walletrpc.NewCompactTxStreamerClient(conn).GetLightdInfo(ctx, &walletrpc.Empty{})
	return status.Code(err)
}

// waitCode retries until callCode returns want, since slots free asynchronously.
func waitCode(t *testing.T, conn *grpc.ClientConn, want codes.Code) {
	t.Helper()
	var got codes.Code
	for range 100 {
		if got = callCode(conn); got == want {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("call returned %v, want %v", got, want)
}

func TestRecvLimiterRefusesWhileBodyPending(t *testing.T) {
	conn := startRecvLimitServer(t, newRecvLimiter(1))
	waitCode(t, conn, codes.Unimplemented)

	ctx, cancel := context.WithCancel(context.Background())
	stream := openStalled(t, ctx, conn)
	waitCode(t, conn, codes.Unavailable)

	// Once the stalled request's body arrives, its slot is freed, even
	// though its handler is still running.
	if err := stream.SendMsg(&walletrpc.ChainSpec{}); err != nil {
		t.Fatal(err)
	}
	waitCode(t, conn, codes.Unimplemented)
	cancel()
}

func TestRecvLimiterFreesSlotOnCancel(t *testing.T) {
	conn := startRecvLimitServer(t, newRecvLimiter(1))
	ctx, cancel := context.WithCancel(context.Background())
	openStalled(t, ctx, conn)
	waitCode(t, conn, codes.Unavailable)
	cancel()
	waitCode(t, conn, codes.Unimplemented)
}

func TestRecvLimiterTimesOutStalledRequest(t *testing.T) {
	l := newRecvLimiter(1)
	l.timeout = 200 * time.Millisecond
	conn := startRecvLimitServer(t, l)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	stream := openStalled(t, ctx, conn)
	if err := stream.RecvMsg(&walletrpc.BlockID{}); status.Code(err) != codes.Canceled {
		t.Fatalf("stalled request ended with %v, want Canceled", err)
	}
	waitCode(t, conn, codes.Unimplemented)
}

func TestRecvLimiterClientStreamsUseOwnPool(t *testing.T) {
	conn := startRecvLimitServer(t, newRecvLimiter(1))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	client := walletrpc.NewCompactTxStreamerClient(conn)
	stream, err := client.GetTaddressBalanceStream(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := stream.Send(&walletrpc.Address{}); err != nil {
		t.Fatal(err)
	}
	// The open client stream doesn't block unary calls...
	waitCode(t, conn, codes.Unimplemented)
	// ...but does block a second client stream.
	second, err := client.GetTaddressBalanceStream(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := second.CloseAndRecv(); status.Code(err) != codes.Unavailable {
		t.Fatalf("second client stream got %v, want Unavailable", err)
	}
}

func TestServerOptionsBoundMessageSize(t *testing.T) {
	conn := startStubServer(t, serverOptions(&common.Options{}))
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := walletrpc.NewCompactTxStreamerClient(conn).SendTransaction(ctx,
		&walletrpc.RawTransaction{Data: make([]byte, maxRecvMsgSize)})
	if status.Code(err) != codes.ResourceExhausted {
		t.Fatalf("oversized request got %v, want ResourceExhausted", err)
	}
}

func TestRecvLimiterReflectionUsesStreamPool(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := grpc.NewServer(newRecvLimiter(1).serverOptions()...)
	walletrpc.RegisterCompactTxStreamerServer(server, recvLimitStub{})
	reflection.Register(server)
	go server.Serve(listener)
	t.Cleanup(server.Stop)
	conn, err := grpc.NewClient(listener.Addr().String(),
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	client := reflectionv1.NewServerReflectionClient(conn)
	req := &reflectionv1.ServerReflectionRequest{
		MessageRequest: &reflectionv1.ServerReflectionRequest_ListServices{},
	}
	first, err := client.ServerReflectionInfo(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := first.Send(req); err != nil {
		t.Fatal(err)
	}
	if _, err := first.Recv(); err != nil {
		t.Fatal(err)
	}
	// The first stream keeps its slot after its first message.
	second, err := client.ServerReflectionInfo(ctx)
	if err != nil {
		t.Fatal(err)
	}
	second.Send(req)
	if _, err := second.Recv(); status.Code(err) != codes.Unavailable {
		t.Fatalf("second reflection stream got %v, want Unavailable", err)
	}
}
