// Copyright (c) 2026 The Zcash developers
// Distributed under the MIT software license, see the accompanying
// file COPYING or https://www.opensource.org/licenses/mit-license.php .

package frontend

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/zcash/lightwalletd/common"
	"github.com/zcash/lightwalletd/walletrpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
)

// Exercise the transport too: a nil response with a nil error becomes a
// successful zero-valued protobuf, and nil from a streaming handler becomes
// normal EOF. Neither is an acceptable response to a failed backend lookup.
func TestTransparentRPCBackendErrors(t *testing.T) {
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
	conn, err := grpc.NewClient("passthrough:///transparent-errors",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			return listener.DialContext(ctx)
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	client := walletrpc.NewCompactTxStreamerClient(conn)
	address := "t" + strings.Repeat("1", 34)

	// Each call returns the observed value as well as its final RPC status.
	// UTXO streams normalize a normal EOF to success, just like wallet clients.
	calls := []struct {
		name   string
		method string
		call   func(context.Context) (int64, error)
	}{
		{"balance", "getaddressbalance", func(ctx context.Context) (int64, error) {
			r, err := client.GetTaddressBalance(ctx, &walletrpc.AddressList{Addresses: []string{address}})
			return r.GetValueZat(), err
		}},
		{"balance_stream", "getaddressbalance", func(ctx context.Context) (int64, error) {
			stream, err := client.GetTaddressBalanceStream(ctx)
			if err != nil {
				return 0, err
			}
			if err := stream.Send(&walletrpc.Address{Address: address}); err != nil {
				return 0, err
			}
			r, err := stream.CloseAndRecv()
			return r.GetValueZat(), err
		}},
		{"utxos", "getaddressutxos", func(ctx context.Context) (int64, error) {
			r, err := client.GetAddressUtxos(ctx, &walletrpc.GetAddressUtxosArg{Addresses: []string{address}})
			var total int64
			for _, utxo := range r.GetAddressUtxos() {
				total += utxo.ValueZat
			}
			return total, err
		}},
		{"utxos_stream", "getaddressutxos", func(ctx context.Context) (int64, error) {
			stream, err := client.GetAddressUtxosStream(ctx, &walletrpc.GetAddressUtxosArg{Addresses: []string{address}})
			if err != nil {
				return 0, err
			}
			var total int64
			for {
				r, err := stream.Recv()
				if err == io.EOF {
					return total, nil
				}
				if err != nil {
					return total, err
				}
				total += r.ValueZat
			}
		}},
	}
	cases := []struct {
		name       string
		backendErr error
		wantCode   codes.Code
		value      int64
	}{
		{"connection_failure", errors.New("backend connection refused"), codes.Unknown, 0},
		{"backend_failure", errors.New("-32603: Internal error"), codes.Unknown, 0},
		{"invalid_address", errors.New("Invalid address"), codes.InvalidArgument, 0},
		{"not_found", errors.New("No information available"), codes.NotFound, 0},
		{"empty_success", nil, codes.OK, 0},
		{"nonempty_success", nil, codes.OK, 1234},
	}
	for _, call := range calls {
		for _, tc := range cases {
			t.Run(call.name+"/"+tc.name, func(t *testing.T) {
				common.RawRequest = func(_ context.Context, method string, _ []json.RawMessage) (json.RawMessage, error) {
					if method != call.method {
						t.Errorf("backend method = %q, want %q", method, call.method)
					}
					if tc.backendErr != nil {
						return nil, tc.backendErr
					}
					if method == "getaddressbalance" {
						return json.Marshal(map[string]int64{"balance": tc.value})
					}
					if tc.value == 0 {
						return json.RawMessage("[]"), nil
					}
					return json.Marshal([]map[string]interface{}{{
						"address": address, "txid": strings.Repeat("ab", 32),
						"script": "51", "satoshis": tc.value, "height": 100,
					}})
				}
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				value, err := call.call(ctx)
				if status.Code(err) != tc.wantCode {
					t.Fatalf("RPC status = %s (%v), want %s", status.Code(err), err, tc.wantCode)
				}
				if tc.backendErr != nil && !strings.Contains(err.Error(), tc.backendErr.Error()) {
					t.Errorf("RPC error %q does not contain backend error %q", err, tc.backendErr)
				}
				if value != tc.value {
					t.Errorf("value = %d, want %d", value, tc.value)
				}
			})
		}
	}
}
