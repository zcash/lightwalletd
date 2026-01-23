// Copyright (c) 2019-2020 The Zcash developers
// Distributed under the MIT software license, see the accompanying
// file COPYING or https://www.opensource.org/licenses/mit-license.php .

package pirclient

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// TestClientIsEnabled tests the IsEnabled method.
func TestClientIsEnabled(t *testing.T) {
	// Nil client
	var nilClient *Client
	if nilClient.IsEnabled() {
		t.Error("nil client should not be enabled")
	}

	// Empty URL client
	emptyClient := NewClient("", 30*time.Second)
	if emptyClient.IsEnabled() {
		t.Error("client with empty URL should not be enabled")
	}

	// Valid client
	validClient := NewClient("http://localhost:8080", 30*time.Second)
	if !validClient.IsEnabled() {
		t.Error("client with valid URL should be enabled")
	}
}

// TestGetStatus tests the GetStatus method against a mock server.
func TestGetStatus(t *testing.T) {
	// Create mock server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/pir/status" {
			t.Errorf("unexpected path: %s", r.URL.Path)
			http.NotFound(w, r)
			return
		}
		if r.Method != "GET" {
			t.Errorf("unexpected method: %s", r.Method)
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		status := PirStatus{
			Status:            "ready",
			PirDbHeight:       2500000,
			PendingBlocks:     5,
			NumNullifiers:     52000000,
			NumBuckets:        6500000,
			RebuildInProgress: false,
		}
		json.NewEncoder(w).Encode(status)
	}))
	defer server.Close()

	client := NewClient(server.URL, 30*time.Second)
	ctx := context.Background()

	status, err := client.GetStatus(ctx)
	if err != nil {
		t.Fatalf("GetStatus failed: %v", err)
	}

	if status.Status != "ready" {
		t.Errorf("expected status 'ready', got '%s'", status.Status)
	}
	if status.PirDbHeight != 2500000 {
		t.Errorf("expected PirDbHeight 2500000, got %d", status.PirDbHeight)
	}
	if status.NumNullifiers != 52000000 {
		t.Errorf("expected NumNullifiers 52000000, got %d", status.NumNullifiers)
	}
}

// TestIngestNullifiers tests the IngestNullifiers method.
func TestIngestNullifiers(t *testing.T) {
	var receivedReq IngestRequest

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/nullifiers/ingest" {
			http.NotFound(w, r)
			return
		}
		if r.Method != "POST" {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		if err := json.NewDecoder(r.Body).Decode(&receivedReq); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		resp := IngestResponse{
			Status:           "accepted",
			QueuedBlocks:     1,
			CurrentPirHeight: receivedReq.BlockHeight - 1,
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewClient(server.URL, 30*time.Second)
	ctx := context.Background()

	req := &IngestRequest{
		BlockHeight: 2500001,
		BlockHash:   "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		Nullifiers: []IngestNullifierEntry{
			{Nullifier: "abcd1234abcd1234abcd1234abcd1234abcd1234abcd1234abcd1234abcd1234", TxIndex: 0},
			{Nullifier: "efef5678efef5678efef5678efef5678efef5678efef5678efef5678efef5678", TxIndex: 1},
		},
	}

	resp, err := client.IngestNullifiers(ctx, req)
	if err != nil {
		t.Fatalf("IngestNullifiers failed: %v", err)
	}

	if resp.Status != "accepted" {
		t.Errorf("expected status 'accepted', got '%s'", resp.Status)
	}
	if resp.QueuedBlocks != 1 {
		t.Errorf("expected QueuedBlocks 1, got %d", resp.QueuedBlocks)
	}

	// Verify request was received correctly
	if receivedReq.BlockHeight != 2500001 {
		t.Errorf("expected BlockHeight 2500001, got %d", receivedReq.BlockHeight)
	}
	if len(receivedReq.Nullifiers) != 2 {
		t.Errorf("expected 2 nullifiers, got %d", len(receivedReq.Nullifiers))
	}
}

// TestHandleReorg tests the HandleReorg method.
func TestHandleReorg(t *testing.T) {
	var receivedHeight uint64

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/nullifiers/reorg" {
			http.NotFound(w, r)
			return
		}

		var req ReorgRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		receivedHeight = req.ReorgHeight

		resp := ReorgResponse{
			Status:        "success",
			BlocksRemoved: 5,
			NewHeight:     req.ReorgHeight,
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewClient(server.URL, 30*time.Second)
	ctx := context.Background()

	resp, err := client.HandleReorg(ctx, 2499995)
	if err != nil {
		t.Fatalf("HandleReorg failed: %v", err)
	}

	if resp.Status != "success" {
		t.Errorf("expected status 'success', got '%s'", resp.Status)
	}
	if resp.BlocksRemoved != 5 {
		t.Errorf("expected BlocksRemoved 5, got %d", resp.BlocksRemoved)
	}
	if receivedHeight != 2499995 {
		t.Errorf("expected reorg height 2499995, got %d", receivedHeight)
	}
}

// TestGetYpirParams tests the GetYpirParams method.
func TestGetYpirParams(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/pir/params/ypir" {
			http.NotFound(w, r)
			return
		}

		params := YpirParams{
			Protocol:            "ypir",
			NumRecords:          52000000,
			RecordSize:          32,
			SuperItemBits:       13,
			SuperItemBytes:      8192,
			NumSuperItems:       203125,
			RecordsPerSuperItem: 256,
			Instances:           1,
			CuckooParams: CuckooParams{
				Seed:       "1234567890123456",
				NumBuckets: 6500000,
				ValueSize:  32,
				EntrySize:  36,
				BucketSize: 288,
			},
			PirDbHeight: 2500000,
		}
		json.NewEncoder(w).Encode(params)
	}))
	defer server.Close()

	client := NewClient(server.URL, 30*time.Second)
	ctx := context.Background()

	params, err := client.GetYpirParams(ctx)
	if err != nil {
		t.Fatalf("GetYpirParams failed: %v", err)
	}

	if params.Protocol != "ypir" {
		t.Errorf("expected protocol 'ypir', got '%s'", params.Protocol)
	}
	if params.NumRecords != 52000000 {
		t.Errorf("expected NumRecords 52000000, got %d", params.NumRecords)
	}
	if params.CuckooParams.NumBuckets != 6500000 {
		t.Errorf("expected NumBuckets 6500000, got %d", params.CuckooParams.NumBuckets)
	}
}

// TestWaitForReady tests the WaitForReady method.
func TestWaitForReady(t *testing.T) {
	callCount := 0

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		status := "building"
		if callCount >= 3 {
			status = "ready"
		}

		resp := PirStatus{
			Status:      status,
			PirDbHeight: 2500000,
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewClient(server.URL, 30*time.Second)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := client.WaitForReady(ctx, 100*time.Millisecond)
	if err != nil {
		t.Fatalf("WaitForReady failed: %v", err)
	}

	if callCount < 3 {
		t.Errorf("expected at least 3 calls, got %d", callCount)
	}
}

// TestWaitForReadyTimeout tests that WaitForReady respects context timeout.
func TestWaitForReadyTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Always return "building"
		resp := PirStatus{
			Status:      "building",
			PirDbHeight: 2500000,
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewClient(server.URL, 30*time.Second)
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()

	err := client.WaitForReady(ctx, 100*time.Millisecond)
	if err == nil {
		t.Error("expected timeout error, got nil")
	}
}

// TestQueryYpir tests the QueryYpir method.
func TestQueryYpir(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/pir/query/ypir" {
			http.NotFound(w, r)
			return
		}

		var req YpirQueryRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		resp := YpirQueryResponse{
			CtsB64:           []string{"base64encodedct1", "base64encodedct2"},
			ProcessingTimeMs: 123.45,
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewClient(server.URL, 30*time.Second)
	ctx := context.Background()

	req := &YpirQueryRequest{
		PackedQueryRowB64: "querydata",
		PubParamsB64:      "pubparams",
	}

	resp, err := client.QueryYpir(ctx, req)
	if err != nil {
		t.Fatalf("QueryYpir failed: %v", err)
	}

	if len(resp.CtsB64) != 2 {
		t.Errorf("expected 2 ciphertexts, got %d", len(resp.CtsB64))
	}
	if resp.ProcessingTimeMs != 123.45 {
		t.Errorf("expected ProcessingTimeMs 123.45, got %f", resp.ProcessingTimeMs)
	}
}

// TestErrorHandling tests error handling for various failure cases.
func TestErrorHandling(t *testing.T) {
	// Server that returns 500
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "internal error", http.StatusInternalServerError)
	}))
	defer server.Close()

	client := NewClient(server.URL, 30*time.Second)
	ctx := context.Background()

	_, err := client.GetStatus(ctx)
	if err == nil {
		t.Error("expected error for 500 response")
	}

	_, err = client.IngestNullifiers(ctx, &IngestRequest{})
	if err == nil {
		t.Error("expected error for 500 response")
	}
}
