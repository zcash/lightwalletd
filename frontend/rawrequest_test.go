// Copyright (c) 2019-2020 The Zcash developers
// Distributed under the MIT software license, see the accompanying
// file COPYING or https://www.opensource.org/licenses/mit-license.php .

package frontend

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/btcsuite/btcd/rpcclient"
)

func TestContextRawRequestSuccess(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read body: %v", err)
		}
		if string(body) == "" {
			t.Error("empty request body")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"result":"ok","error":null}`))
	}))
	defer server.Close()

	cfg := &rpcclient.ConnConfig{
		Host:         server.Listener.Addr().String(),
		User:         "user",
		Pass:         "pass",
		HTTPPostMode: true,
		DisableTLS:   true,
	}
	rawRequest := NewContextRawRequest(cfg)

	result, err := rawRequest(context.Background(), "getblockchaininfo", nil)
	if err != nil {
		t.Fatalf("RawRequest failed: %v", err)
	}
	var reply string
	if err := json.Unmarshal(result, &reply); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if reply != "ok" {
		t.Fatalf("unexpected result %q", reply)
	}
}
