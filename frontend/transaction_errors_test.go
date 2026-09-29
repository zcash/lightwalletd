// Copyright (c) 2026 The Zcash developers
// Distributed under the MIT software license, see the accompanying
// file COPYING or https://www.opensource.org/licenses/mit-license.php .

package frontend

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/btcsuite/btcd/rpcclient"
	"github.com/zcash/lightwalletd/common"
	"github.com/zcash/lightwalletd/walletrpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
)

func TestGetTransactionBackendStatus(t *testing.T) {
	defer resetGlobals()
	service, err := NewLwdStreamer(nil, "main", false)
	if err != nil {
		t.Fatal(err)
	}
	listener := bufconn.Listen(1024 * 1024)
	defer listener.Close()
	server := grpc.NewServer()
	walletrpc.RegisterCompactTxStreamerServer(server, service)
	go func() { _ = server.Serve(listener) }()
	defer server.Stop()
	conn, err := grpc.NewClient("passthrough:///transaction-errors",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) { return listener.DialContext(ctx) }),
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	client := walletrpc.NewCompactTxStreamerClient(conn)
	cases := []struct {
		name       string
		httpStatus int
		body       string
		want       codes.Code
	}{
		{"missing_transaction", 500, `{"result":null,"error":{"code":-5,"message":"No such mempool or main chain transaction"}}`, codes.NotFound},
		{"internal_backend_error", 500, `{"result":null,"error":{"code":-32603,"message":"Internal error"}}`, codes.Unknown},
		{"backend_warming_up", 500, `{"result":null,"error":{"code":-28,"message":"Loading block index"}}`, codes.Unknown},
		{"unstructured_backend_unavailable", 503, "Service unavailable", codes.Unknown},
		{"unstructured_missing_text", 502, "No such mempool or main chain transaction", codes.Unknown},
		{"valid_transaction", 200, `{"result":{"hex":"deadbeef","height":123},"error":null}`, codes.OK},
		{"malformed_transaction", 200, `{"result":{"hex":"not-hex"},"error":null}`, codes.Internal},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var request struct {
					Method string `json:"method"`
				}
				if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
					t.Error(err)
				}
				if request.Method != "getrawtransaction" {
					t.Errorf("method=%s", request.Method)
				}
				w.WriteHeader(tc.httpStatus)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer backend.Close()
			raw, err := NewContextRawRequest(&rpcclient.ConnConfig{Host: backend.Listener.Addr().String(), HTTPPostMode: true, DisableTLS: true})
			if err != nil {
				t.Fatal(err)
			}
			common.RawRequest = raw
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			tx, err := client.GetTransaction(ctx, &walletrpc.TxFilter{Hash: make([]byte, 32)})
			if status.Code(err) != tc.want {
				t.Fatalf("code=%v err=%v want=%v", status.Code(err), err, tc.want)
			}
			if tc.want == codes.OK && (fmt.Sprintf("%x", tx.Data) != "deadbeef" || tx.Height != 123) {
				t.Fatalf("unexpected transaction: %v", tx)
			}
		})
	}
	// Keep the client context live: otherwise grpc itself can mask a handler
	// that incorrectly converts the backend's context error to NotFound.
	for _, tc := range []struct {
		name string
		err  error
		want codes.Code
	}{
		{"canceled", fmt.Errorf("backend: %w", context.Canceled), codes.Canceled},
		{"deadline", fmt.Errorf("backend: %w", context.DeadlineExceeded), codes.DeadlineExceeded},
		{"connection_failure", fmt.Errorf("dial tcp: connection refused"), codes.Unknown},
	} {
		t.Run(tc.name, func(t *testing.T) {
			common.RawRequest = func(context.Context, string, []json.RawMessage) (json.RawMessage, error) { return nil, tc.err }
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_, err := client.GetTransaction(ctx, &walletrpc.TxFilter{Hash: make([]byte, 32)})
			if status.Code(err) != tc.want {
				t.Fatalf("code=%v err=%v want=%v", status.Code(err), err, tc.want)
			}
		})
	}
	t.Run("invalid_hash_never_reaches_backend", func(t *testing.T) {
		common.RawRequest = func(context.Context, string, []json.RawMessage) (json.RawMessage, error) {
			t.Error("unexpected backend call")
			return nil, nil
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, err := client.GetTransaction(ctx, &walletrpc.TxFilter{Hash: []byte{1}})
		if status.Code(err) != codes.InvalidArgument {
			t.Fatalf("invalid hash: %v", err)
		}
	})
}
