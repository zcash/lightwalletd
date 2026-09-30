// Copyright (c) 2026 The Zcash developers
// Distributed under the MIT software license, see the accompanying
// file COPYING or https://www.opensource.org/licenses/mit-license.php .

package frontend

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
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

type transparentRangeBackendTx struct {
	txid    string
	data    []byte
	height  int64
	mempool bool
	missing bool
}

type transparentRangeRPCStep struct {
	method string
	txid   string
	result any
	rpcErr any
}

// A controlled backend supplies legitimate metadata for a transaction whose
// location changed after getaddresstxids: an omitted mempool height, a -1
// side-chain height, or a different mined height. The test exercises the real
// HTTP adapter and gRPC service; it does not run a live reorg or wallet.
// Tests remain sequential because common.RawRequest is a package global.
func TestTransparentTransactionRangeChainChange(t *testing.T) {
	const (
		address   = "t1234567890123456789012345678901234"
		start     = uint64(419200)
		end       = uint64(419210)
		firstTxid = "1234000000000000000000000000000000000000000000000000000000000000"
		nextTxid  = "2234000000000000000000000000000000000000000000000000000000000000"
	)
	if len(rawTxData) < 2 {
		t.Fatal("upstream transaction fixtures were not loaded")
	}
	first := transparentRangeBackendTx{txid: firstTxid, data: rawTxData[0], height: 419205}
	next := transparentRangeBackendTx{txid: nextTxid, data: rawTxData[1]}
	changed := func(height int64, mempool bool) []transparentRangeBackendTx {
		tx := first
		tx.height, tx.mempool = height, mempool
		return []transparentRangeBackendTx{tx}
	}
	prefixThen := func(height int64, missing bool) []transparentRangeBackendTx {
		tx := next
		tx.height, tx.missing = height, missing
		return []transparentRangeBackendTx{first, tx}
	}
	cases := []struct {
		name       string
		start      uint64
		end        uint64
		omitEnd    bool
		tip        uint64
		txs        []transparentRangeBackendTx
		indexError bool
		want       []transparentRangeBackendTx
		wantCode   codes.Code
	}{
		{name: "stable_in_range", start: start, end: end, txs: []transparentRangeBackendTx{first}, want: []transparentRangeBackendTx{first}, wantCode: codes.OK},
		{name: "inclusive_start", start: start, end: end, txs: changed(int64(start), false), want: changed(int64(start), false), wantCode: codes.OK},
		{name: "inclusive_end", start: start, end: end, txs: changed(int64(end), false), want: changed(int64(end), false), wantCode: codes.OK},
		{name: "mempool_after_index_query", start: start, end: end, txs: changed(0, true), wantCode: codes.Aborted},
		{name: "side_chain_after_index_query", start: start, end: end, txs: changed(-1, false), wantCode: codes.Aborted},
		{name: "remined_above_end", start: start, end: end, txs: changed(int64(end+1), false), wantCode: codes.Aborted},
		{name: "remined_below_start", start: start, end: end, txs: changed(int64(start-1), false), wantCode: codes.Aborted},
		{name: "valid_prefix_then_side_chain", start: start, end: end, txs: prefixThen(-1, false), want: []transparentRangeBackendTx{first}, wantCode: codes.Aborted},
		{name: "valid_prefix_then_missing_transaction", start: start, end: end, txs: prefixThen(0, true), want: []transparentRangeBackendTx{first}, wantCode: codes.NotFound},
		{name: "reversed_range_preserves_backend_error", start: end, end: start, indexError: true, wantCode: codes.InvalidArgument},
		{name: "zero_start_still_excludes_mempool", start: 0, end: 10, txs: changed(0, true), wantCode: codes.Aborted},
		{name: "omitted_end_uses_concrete_tip", start: start, omitEnd: true, tip: end, txs: changed(int64(end), false), want: changed(int64(end), false), wantCode: codes.OK},
		{name: "omitted_end_rejects_later_mined_height", start: start, omitEnd: true, tip: end, txs: changed(int64(end+1), false), wantCode: codes.Aborted},
	}

	for _, method := range []string{"GetTaddressTransactions", "GetTaddressTxids"} {
		t.Run(method, func(t *testing.T) {
			for _, tc := range cases {
				t.Run(tc.name, func(t *testing.T) {
					concreteEnd := tc.end
					var steps []transparentRangeRPCStep
					if tc.omitEnd {
						concreteEnd = tc.tip
						steps = append(steps, transparentRangeRPCStep{method: "getblockchaininfo", result: map[string]any{"blocks": tc.tip}})
					}
					ids := make([]string, len(tc.txs))
					for i, tx := range tc.txs {
						ids[i] = tx.txid
					}
					indexStep := transparentRangeRPCStep{method: "getaddresstxids", result: ids}
					if tc.indexError {
						indexStep.result = nil
						indexStep.rpcErr = map[string]any{"code": -8, "message": "start must be less than or equal to end"}
					}
					steps = append(steps, indexStep)
					for _, tx := range tc.txs {
						txStep := transparentRangeRPCStep{method: "getrawtransaction", txid: tx.txid}
						if tx.missing {
							txStep.rpcErr = map[string]any{"code": -5, "message": "transaction not found"}
						} else {
							result := map[string]any{"hex": hex.EncodeToString(tx.data)}
							if !tx.mempool {
								result["height"] = tx.height
							}
							txStep.result = result
						}
						steps = append(steps, txStep)
					}
					verifyBackend := transparentRangeBackend(t, address, tc.start, concreteEnd, steps)
					client := transparentRangeGRPCClient(t)
					ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
					defer cancel()
					span := &walletrpc.BlockRange{Start: &walletrpc.BlockID{Height: tc.start}}
					if !tc.omitEnd {
						span.End = &walletrpc.BlockID{Height: tc.end}
					}
					filter := &walletrpc.TransparentAddressBlockFilter{Address: address, Range: span}
					var stream interface {
						Recv() (*walletrpc.RawTransaction, error)
					}
					var err error
					if method == "GetTaddressTransactions" {
						stream, err = client.GetTaddressTransactions(ctx, filter)
					} else {
						stream, err = client.GetTaddressTxids(ctx, filter)
					}
					if err != nil {
						t.Fatal(err)
					}
					var got []*walletrpc.RawTransaction
					var terminal error
					for {
						tx, recvErr := stream.Recv()
						if recvErr != nil {
							terminal = recvErr
							break
						}
						got = append(got, tx)
					}
					gotCode := status.Code(terminal)
					if terminal == io.EOF {
						gotCode = codes.OK
					}
					if gotCode != tc.wantCode {
						t.Errorf("final status = %s (%v), want %s", gotCode, terminal, tc.wantCode)
					}
					if len(got) != len(tc.want) {
						heights := make([]uint64, len(got))
						for i, tx := range got {
							heights[i] = tx.Height
						}
						t.Errorf("received heights %v, want exactly %d valid prefix transactions; changed metadata must not be sent", heights, len(tc.want))
					}
					for i, want := range tc.want {
						if i >= len(got) {
							break
						}
						if got[i].Height != uint64(want.height) || !bytes.Equal(got[i].Data, want.data) {
							t.Errorf("transaction %d differs from expected valid prefix: height %d (want %d), bytes equal %v", i, got[i].Height, want.height, bytes.Equal(got[i].Data, want.data))
						}
					}
					verifyBackend()
				})
			}
		})
	}
}

// The backend validates the complete ordered index/lookup sequence, including
// concrete endpoints and big-endian transaction IDs, independently of results.
func transparentRangeBackend(t *testing.T, address string, start, end uint64, steps []transparentRangeRPCStep) func() {
	t.Helper()
	previous := common.RawRequest
	t.Cleanup(func() { common.RawRequest = previous })
	var mutex sync.Mutex
	next := 0
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mutex.Lock()
		defer mutex.Unlock()
		var req struct {
			Method string            `json:"method"`
			Params []json.RawMessage `json:"params"`
		}
		var checkErr error
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			checkErr = err
		}
		var step transparentRangeRPCStep
		if next >= len(steps) {
			checkErr = fmt.Errorf("unexpected extra backend request %s", req.Method)
		} else {
			step = steps[next]
			next++
			if req.Method != step.method {
				checkErr = fmt.Errorf("backend request %d = %s, want %s", next, req.Method, step.method)
			} else {
				switch req.Method {
				case "getblockchaininfo":
					if len(req.Params) != 0 {
						checkErr = fmt.Errorf("unexpected getblockchaininfo parameters %s", req.Params)
					}
				case "getaddresstxids":
					var filter struct {
						Addresses []string `json:"addresses"`
						Start     uint64   `json:"start"`
						End       uint64   `json:"end"`
					}
					if len(req.Params) != 1 {
						checkErr = fmt.Errorf("unexpected getaddresstxids parameters %s", req.Params)
					} else if err := json.Unmarshal(req.Params[0], &filter); err != nil {
						checkErr = err
					} else if len(filter.Addresses) != 1 || filter.Addresses[0] != address || filter.Start != start || filter.End != end {
						checkErr = fmt.Errorf("unexpected address filter %+v, want address %s range [%d,%d]", filter, address, start, end)
					}
				case "getrawtransaction":
					var txid string
					var verbose int
					if len(req.Params) != 2 {
						checkErr = fmt.Errorf("unexpected getrawtransaction parameters %s", req.Params)
					} else if err := json.Unmarshal(req.Params[0], &txid); err != nil {
						checkErr = err
					} else if err := json.Unmarshal(req.Params[1], &verbose); err != nil {
						checkErr = err
					} else if txid != step.txid || verbose != 1 {
						checkErr = fmt.Errorf("lookup txid %s verbosity %d, want txid %s verbosity 1", txid, verbose, step.txid)
					}
				}
			}
		}
		if checkErr != nil {
			t.Error(checkErr)
			step.result = nil
			step.rpcErr = map[string]any{"code": -32603, "message": checkErr.Error()}
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(map[string]any{"result": step.result, "error": step.rpcErr, "id": 1}); err != nil {
			t.Error(err)
		}
	}))
	t.Cleanup(backend.Close)
	var err error
	common.RawRequest, err = NewContextRawRequest(&rpcclient.ConnConfig{Host: backend.Listener.Addr().String(), DisableTLS: true, HTTPPostMode: true})
	if err != nil {
		t.Fatal(err)
	}
	return func() {
		mutex.Lock()
		defer mutex.Unlock()
		if next != len(steps) {
			t.Errorf("backend consumed %d requests, want %d", next, len(steps))
		}
	}
}

func transparentRangeGRPCClient(t *testing.T) walletrpc.CompactTxStreamerClient {
	t.Helper()
	handler, err := NewLwdStreamer(nil, "main", false)
	if err != nil {
		t.Fatal(err)
	}
	listener := bufconn.Listen(1024 * 1024)
	server := grpc.NewServer()
	walletrpc.RegisterCompactTxStreamerServer(server, handler)
	stopped := make(chan error, 1)
	go func() { stopped <- server.Serve(listener) }()
	t.Cleanup(func() {
		server.Stop()
		listener.Close()
		select {
		case err := <-stopped:
			if err != nil && err != grpc.ErrServerStopped {
				t.Errorf("gRPC server shutdown: %v", err)
			}
		case <-time.After(time.Second):
			t.Error("gRPC server did not stop")
		}
	})
	conn, err := grpc.NewClient("passthrough:///transparent-range-test", grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return listener.Dial() }))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	return walletrpc.NewCompactTxStreamerClient(conn)
}
