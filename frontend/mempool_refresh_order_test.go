// Copyright (c) 2026 The Zcash developers
// Distributed under the MIT software license, see the accompanying
// file COPYING or https://www.opensource.org/licenses/mit-license.php .

package frontend

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/zcash/lightwalletd/common"
	"github.com/zcash/lightwalletd/walletrpc"
	"google.golang.org/grpc"
)

type refreshOrderStream struct {
	grpc.ServerStream
	txids []string
}

func (s *refreshOrderStream) Context() context.Context { return context.Background() }
func (s *refreshOrderStream) Send(tx *walletrpc.CompactTx) error {
	s.txids = append(s.txids, fmt.Sprintf("%x", tx.Txid))
	return nil
}

func TestMempoolRefreshPublicationOrder(t *testing.T) {
	data, err := os.ReadFile("../testdata/tx_v5.json")
	if err != nil {
		t.Fatal(err)
	}
	var rows []json.RawMessage
	if err = json.Unmarshal(data, &rows); err != nil {
		t.Fatal(err)
	}
	var row []json.RawMessage
	if err = json.Unmarshal(rows[2], &row); err != nil {
		t.Fatal(err)
	}
	var txid string
	if err = json.Unmarshal(row[1], &txid); err != nil {
		t.Fatal(err)
	}
	populated, _ := json.Marshal([]string{txid})
	empty := json.RawMessage(`[]`)
	for _, tc := range []struct {
		name                    string
		first, second           json.RawMessage
		firstCount, secondCount int
	}{
		{"older_cannot_resurrect_removed_transaction", populated, empty, 1, 0},
		{"older_cannot_erase_new_transaction", empty, populated, 0, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resetGlobals()
			defer resetGlobals()
			service, err := NewLwdStreamer(nil, "main", false)
			if err != nil {
				t.Fatal(err)
			}
			entered, release := make(chan struct{}), make(chan struct{})
			var once sync.Once
			unblock := func() { once.Do(func() { close(release) }) }
			defer unblock()
			var calls atomic.Int32
			common.RawRequest = func(ctx context.Context, method string, params []json.RawMessage) (json.RawMessage, error) {
				switch method {
				case "getrawmempool":
					if calls.Add(1) == 1 {
						close(entered)
						<-release
						return tc.first, nil
					}
					return tc.second, nil
				case "getrawtransaction":
					return row[0], nil
				default:
					return nil, fmt.Errorf("unexpected RPC %s", method)
				}
			}
			request := func() *walletrpc.GetMempoolTxRequest {
				return &walletrpc.GetMempoolTxRequest{PoolTypes: []walletrpc.PoolType{walletrpc.PoolType_TRANSPARENT, walletrpc.PoolType_SAPLING, walletrpc.PoolType_ORCHARD}}
			}
			first := &refreshOrderStream{}
			done := make(chan error, 1)
			go func() { done <- service.GetMempoolTx(request(), first) }()
			select {
			case <-entered:
			case <-time.After(5 * time.Second):
				t.Fatal("first refresh did not start")
			}
			// Model the two-second refresh window expiring while the first backend
			// request is blocked. No wall-clock sleep or production hook is needed.
			service.(*lwdStreamer).mutex.Lock()
			lastMempool = time.Now().Add(-3 * time.Second)
			service.(*lwdStreamer).mutex.Unlock()
			second := &refreshOrderStream{}
			if err := service.GetMempoolTx(request(), second); err != nil {
				t.Error(err)
			}
			unblock()
			select {
			case err := <-done:
				if err != nil {
					t.Fatal(err)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("first refresh did not finish")
			}
			if len(first.txids) != tc.firstCount || len(second.txids) != tc.secondCount {
				t.Fatalf("snapshot controls: first=%v second=%v", first.txids, second.txids)
			}
			cached := &refreshOrderStream{}
			if err := service.GetMempoolTx(request(), cached); err != nil {
				t.Fatal(err)
			}
			if calls.Load() != 2 {
				t.Fatalf("cache read unexpectedly refreshed: calls=%d", calls.Load())
			}
			if !reflect.DeepEqual(cached.txids, second.txids) {
				t.Fatalf("cached snapshot=%v, want newer snapshot=%v", cached.txids, second.txids)
			}
		})
	}
}
