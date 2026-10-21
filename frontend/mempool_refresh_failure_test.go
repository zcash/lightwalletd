// Copyright (c) 2026 The Zcash developers
// Distributed under the MIT software license, see the accompanying
// file COPYING or https://www.opensource.org/licenses/mit-license.php .

package frontend

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/zcash/lightwalletd/common"
	"github.com/zcash/lightwalletd/walletrpc"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestMempoolNewerFailedRefreshPreservesSuccessfulSnapshot(t *testing.T) {
	resetGlobals()
	defer resetGlobals()
	data, err := os.ReadFile("../testdata/tx_v5.json")
	if err != nil {
		t.Fatal(err)
	}
	var rows []json.RawMessage
	if err := json.Unmarshal(data, &rows); err != nil {
		t.Fatal(err)
	}
	var row []json.RawMessage
	if err := json.Unmarshal(rows[2], &row); err != nil {
		t.Fatal(err)
	}
	var id string
	if err := json.Unmarshal(row[1], &id); err != nil {
		t.Fatal(err)
	}
	list, _ := json.Marshal([]string{id})
	service, err := NewLwdStreamer(nil, "main", false)
	if err != nil {
		t.Fatal(err)
	}
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	var calls atomic.Int32
	common.RawRequest = func(_ context.Context, method string, _ []json.RawMessage) (json.RawMessage, error) {
		if method == "getrawmempool" {
			if calls.Add(1) == 1 {
				close(entered)
				<-release
				return list, nil
			}
			return nil, errors.New("newer refresh connection reset")
		}
		return row[0], nil
	}
	req := func() *walletrpc.GetMempoolTxRequest {
		return &walletrpc.GetMempoolTxRequest{PoolTypes: []walletrpc.PoolType{walletrpc.PoolType_TRANSPARENT, walletrpc.PoolType_SAPLING, walletrpc.PoolType_ORCHARD}}
	}
	first := &refreshOrderStream{}
	done := make(chan error, 1)
	go func() { done <- service.GetMempoolTx(req(), first) }()
	joined := false
	defer func() {
		unblock()
		if !joined {
			<-done
		}
	}()
	<-entered
	service.(*lwdStreamer).mutex.Lock()
	lastMempool = time.Now().Add(-3 * time.Second)
	service.(*lwdStreamer).mutex.Unlock()
	if err := service.GetMempoolTx(req(), &refreshOrderStream{}); err == nil {
		t.Fatal("newer refresh should fail")
	}
	unblock()
	err = <-done
	joined = true
	if err != nil {
		t.Fatal(err)
	}
	if len(first.txids) != 1 {
		t.Fatalf("successful refresh returned %d transactions", len(first.txids))
	}
	cached := &refreshOrderStream{}
	if err := service.GetMempoolTx(req(), cached); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 2 {
		t.Fatalf("expected cache read, got %d refreshes", calls.Load())
	}
	if len(cached.txids) != 1 {
		t.Fatalf("successful snapshot discarded after newer refresh failed: cached=%d, want 1", len(cached.txids))
	}
}

func TestMempoolSuccessfulRefreshPublishesWhileNewerInFlight(t *testing.T) {
	resetGlobals()
	defer resetGlobals()
	data, err := os.ReadFile("../testdata/tx_v5.json")
	if err != nil {
		t.Fatal(err)
	}
	var rows []json.RawMessage
	if err := json.Unmarshal(data, &rows); err != nil {
		t.Fatal(err)
	}
	var row []json.RawMessage
	if err := json.Unmarshal(rows[2], &row); err != nil {
		t.Fatal(err)
	}
	var id string
	if err := json.Unmarshal(row[1], &id); err != nil {
		t.Fatal(err)
	}
	list, _ := json.Marshal([]string{id})
	service, err := NewLwdStreamer(nil, "main", false)
	if err != nil {
		t.Fatal(err)
	}
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	newerEntered, newerRelease := make(chan struct{}), make(chan struct{})
	var newerOnce sync.Once
	unblockNewer := func() { newerOnce.Do(func() { close(newerRelease) }) }
	defer unblockNewer()
	var calls atomic.Int32
	common.RawRequest = func(_ context.Context, method string, _ []json.RawMessage) (json.RawMessage, error) {
		if method == "getrawmempool" {
			if calls.Add(1) == 1 {
				close(entered)
				<-release
				return list, nil
			}
			close(newerEntered)
			<-newerRelease
			return json.RawMessage(`[]`), nil
		}
		return row[0], nil
	}
	req := func() *walletrpc.GetMempoolTxRequest {
		return &walletrpc.GetMempoolTxRequest{PoolTypes: []walletrpc.PoolType{walletrpc.PoolType_TRANSPARENT, walletrpc.PoolType_SAPLING, walletrpc.PoolType_ORCHARD}}
	}
	first := &refreshOrderStream{}
	done := make(chan error, 1)
	go func() { done <- service.GetMempoolTx(req(), first) }()
	joined := false
	defer func() {
		unblock()
		if !joined {
			<-done
		}
	}()
	<-entered
	service.(*lwdStreamer).mutex.Lock()
	lastMempool = time.Now().Add(-3 * time.Second)
	service.(*lwdStreamer).mutex.Unlock()
	newerDone := make(chan error, 1)
	go func() { newerDone <- service.GetMempoolTx(req(), &refreshOrderStream{}) }()
	defer func() { unblockNewer(); <-newerDone }()
	<-newerEntered
	unblock()
	err = <-done
	joined = true
	if err != nil {
		t.Fatal(err)
	}
	if len(first.txids) != 1 {
		t.Fatalf("successful refresh returned %d transactions", len(first.txids))
	}
	cached := &refreshOrderStream{}
	if err := service.GetMempoolTx(req(), cached); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 2 {
		t.Fatalf("expected cache read, got %d refreshes", calls.Load())
	}
	if len(cached.txids) != 1 {
		t.Fatalf("successful snapshot discarded while newer refresh still running: cached=%d, want 1", len(cached.txids))
	}
}
