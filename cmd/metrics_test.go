// Copyright (c) 2019-present The Zcash developers
// Distributed under the MIT software license, see the accompanying
// file COPYING or https://www.opensource.org/licenses/mit-license.php .
package cmd

import (
	"io"
	"net"
	"net/http"
	"testing"
	"time"
)

func startMetricsServer(t *testing.T) net.Addr {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := newMetricsServer(listener.Addr().String())
	go server.Serve(listener)
	t.Cleanup(func() { server.Close() })
	return listener.Addr()
}

func TestMetricsServerServes(t *testing.T) {
	addr := startMetricsServer(t)
	resp, err := http.Get("http://" + addr.String() + "/metrics")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("/metrics returned %d, want 200", resp.StatusCode)
	}
}

// A connection whose request headers never finish arriving is closed once
// the header timeout passes (GHSA-w24w-x7vw-f2w8).
func TestMetricsServerClosesSlowHeaders(t *testing.T) {
	saved := metricsReadHeaderTimeout
	metricsReadHeaderTimeout = 200 * time.Millisecond
	defer func() { metricsReadHeaderTimeout = saved }()
	addr := startMetricsServer(t)

	conn, err := net.Dial("tcp", addr.String())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if _, err := conn.Write([]byte("GET /metrics HTTP/1.1\r\nHost: x\r\n")); err != nil {
		t.Fatal(err)
	}
	conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	if _, err := io.ReadAll(conn); err != nil {
		t.Fatalf("server did not close the stalled connection: %v", err)
	}
}
