// Copyright (c) 2019-present The Zcash developers
// Distributed under the MIT software license, see the accompanying
// file COPYING or https://www.opensource.org/licenses/mit-license.php .
package cmd

import (
	"net"
	"testing"
	"time"

	"golang.org/x/net/http2"
	"google.golang.org/grpc"

	"github.com/zcash/lightwalletd/common"
)

// serverMaxConcurrentStreams starts a server with the given limit and returns
// the SETTINGS_MAX_CONCURRENT_STREAMS it announces to a connecting client, and
// whether it announced one at all. gRPC omits the setting entirely when the
// limit is unset, which is what tells the client it may open as many streams
// as it likes.
func serverMaxConcurrentStreams(t *testing.T, limit uint64) (uint32, bool) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := grpc.NewServer(serverOptions(&common.Options{
		GRPCMaxConcurrentStreams: limit,
	})...)
	go server.Serve(listener)
	defer server.Stop()

	conn, err := net.Dial("tcp", listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if err := conn.SetDeadline(time.Now().Add(10 * time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Write([]byte(http2.ClientPreface)); err != nil {
		t.Fatal(err)
	}
	frame, err := http2.NewFramer(conn, conn).ReadFrame()
	if err != nil {
		t.Fatal(err)
	}
	settings, ok := frame.(*http2.SettingsFrame)
	if !ok {
		t.Fatalf("first frame is %T, want *http2.SettingsFrame", frame)
	}
	return settings.Value(http2.SettingMaxConcurrentStreams)
}

func TestServerOptionsLimitsConcurrentStreams(t *testing.T) {
	got, ok := serverMaxConcurrentStreams(t, 100)
	if !ok {
		t.Fatal("server announced no SETTINGS_MAX_CONCURRENT_STREAMS")
	}
	if got != 100 {
		t.Fatalf("server announced %d concurrent streams, want 100", got)
	}
}

// Zero is the operator's escape hatch back to the unlimited default.
func TestServerOptionsZeroMeansUnlimited(t *testing.T) {
	if got, ok := serverMaxConcurrentStreams(t, 0); ok {
		t.Fatalf("server announced a limit of %d, want none", got)
	}
}
