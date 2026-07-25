// Copyright (c) 2019-2020 The Zcash developers
// Distributed under the MIT software license, see the accompanying
// file COPYING or https://www.opensource.org/licenses/mit-license.php .
package common

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/zcash/lightwalletd/walletrpc"
)

// ------------------------------------------ Setup
//
// This section does some setup things that may (even if not currently)
// be useful across multiple tests.

var (
	testT     *testing.T
	step      int // The various stub callbacks need to sequence through states
	logger    = logrus.New()
	blocks    [][]byte // four test blocks
	testcache *BlockCache
)

const (
	testTxid      = "1234000000000000000000000000000000000000000000000000000000000000"
	testBlockid40 = "0000000000000000000000000000000000000000000000000000000000380640"
	testBlockid41 = "0000000000000000000000000000000000000000000000000000000000380641"
	testBlockid42 = "0000000000000000000000000000000000000000000000000000000000380642"
)

// TestMain does common setup that's shared across multiple tests
func TestMain(m *testing.M) {
	output, err := os.OpenFile("test-log", os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0644)
	if err != nil {
		os.Stderr.WriteString(fmt.Sprintf("Cannot open test-log: %v", err))
		os.Exit(1)
	}
	logger.SetOutput(output)
	Log = logger.WithFields(logrus.Fields{
		"app": "test",
	})

	// Several tests need test blocks; read all 4 into memory just once
	// (for efficiency).
	testBlocks, err := os.Open("../testdata/blocks")
	if err != nil {
		os.Stderr.WriteString(fmt.Sprintf("Cannot open testdata/blocks: %v", err))
		os.Exit(1)
	}
	scan := bufio.NewScanner(testBlocks)
	for scan.Scan() { // each line (block)
		blockJSON, _ := json.Marshal(scan.Text())
		blocks = append(blocks, blockJSON)
	}
	testcache = NewBlockCache(unitTestPath, unitTestChain, 380640, 0)

	// Setup is done; run all tests.
	exitcode := m.Run()

	// cleanup
	os.Remove("test-log")

	os.Exit(exitcode)
}

// Allow tests to verify that sleep has been called (for retries)
var sleepCount int
var sleepDuration time.Duration

func sleepStub(d time.Duration) {
	sleepCount++
	sleepDuration += d
}
func nowStub() time.Time {
	start := time.Time{}
	return start.Add(sleepDuration)
}

// afterStub returns a pre-fired channel so the select case in GetMempool's
// cancel-aware sleep fires immediately, accumulating sleepDuration like
// sleepStub does. Used by tests that exercise GetMempool's wait loop.
func afterStub(d time.Duration) <-chan time.Time {
	sleepCount++
	sleepDuration += d
	ch := make(chan time.Time, 1)
	ch <- time.Time{}
	return ch
}

// resetGlobals restores the package globals that tests replace -- the RPC and
// time stubs, the state those stubs accumulate, and the mempool cache that
// GetMempool maintains -- to the values they have at package initialization.
// Any test that installs a stub or otherwise dirties this state should
// "defer resetGlobals()" so it doesn't leak into later tests, even if the test
// exits early via t.Fatal. Restoring the function pointers to nil (rather than
// to a working implementation) means that a test that forgets to install a
// stub panics rather than silently running against a leftover one.
func resetGlobals() {
	RawRequest = nil
	Time.Sleep = nil
	Time.Now = nil
	Time.After = nil
	step = 0
	sleepCount = 0
	sleepDuration = 0
	DonationAddress = ""
	g_lastBlockChainInfo = &ZcashdRpcReplyGetblockchaininfo{}
	g_lastTime = time.Time{}
	g_txidSeen = map[txid]struct{}{}
	g_txList = nil
}

// ------------------------------------------ GetLightdInfo()

func getLightdInfoStub(ctx context.Context, method string, params []json.RawMessage) (json.RawMessage, error) {
	step++
	switch method {
	case "getinfo":
		r, _ := json.Marshal(&ZcashdRpcReplyGetinfo{})
		return r, nil

	case "getblockchaininfo":
		// Test retry logic (for the moment, it's very simple, just one retry).
		switch step {
		case 1:
			return json.RawMessage{}, errors.New("first failure")
		case 2:
			if sleepCount != 1 || sleepDuration != 15*time.Second {
				testT.Error("unexpected sleeps", sleepCount, sleepDuration)
			}
		}
		r, _ := json.Marshal(&ZcashdRpcReplyGetblockchaininfo{
			Blocks:    9977,
			Chain:     "bugsbunny",
			Consensus: ConsensusInfo{Chaintip: "someid"},
			Upgrades: map[string]Upgradeinfo{
				"a": {
					Name:             "a",
					ActivationHeight: 5,
					Status:           "active",
				},
				"b": {
					Name:             "b",
					ActivationHeight: 6,
					Status:           "pending",
				},
				"c": {
					Name:             "c",
					ActivationHeight: 7,
					Status:           "pending",
				},
			},
		})
		return r, nil
	}
	return nil, nil
}

func TestGetLightdInfo(t *testing.T) {
	testT = t
	RawRequest = getLightdInfoStub
	defer resetGlobals()
	Time.Sleep = sleepStub
	// This calls the getblockchaininfo rpc just to establish connectivity with zcashd
	FirstRPC()

	DonationAddress = "ua1234test"

	// Ensure the retry happened as expected
	logFile, err := os.ReadFile("test-log")
	if err != nil {
		t.Fatal("Cannot read test-log", err)
	}
	logStr := string(logFile)
	if !strings.Contains(logStr, "retrying") {
		t.Fatal("Cannot find retrying in test-log")
	}
	if !strings.Contains(logStr, "retry=1") {
		t.Fatal("Cannot find retry=1 in test-log")
	}

	// Check the success case (second attempt)
	getLightdInfo, err := GetLightdInfo()
	if err != nil {
		t.Fatal("GetLightdInfo failed")
	}
	if getLightdInfo.SaplingActivationHeight != 0 {
		t.Error("unexpected saplingActivationHeight", getLightdInfo.SaplingActivationHeight)
	}
	if getLightdInfo.BlockHeight != 9977 {
		t.Error("unexpected blockHeight", getLightdInfo.BlockHeight)
	}
	if getLightdInfo.ChainName != "bugsbunny" {
		t.Error("unexpected chainName", getLightdInfo.ChainName)
	}
	if getLightdInfo.ConsensusBranchId != "someid" {
		t.Error("unexpected ConsensusBranchId", getLightdInfo.ConsensusBranchId)
	}
	if getLightdInfo.DonationAddress != "ua1234test" {
		t.Error("unexpected DonationAddress", getLightdInfo.DonationAddress)
	}
	// If more than one network upgrade is pending, the closest
	// (next) should be reported.
	if getLightdInfo.UpgradeName != "b" {
		t.Error("unexpected UpgradeName", getLightdInfo.UpgradeName)
	}
	if getLightdInfo.UpgradeHeight != 6 {
		t.Error("unexpected UpgradeHeight", getLightdInfo.UpgradeHeight)
	}

	if sleepCount != 1 || sleepDuration != 15*time.Second {
		t.Error("unexpected sleeps", sleepCount, sleepDuration)
	}
}

// ------------------------------------------ BlockIngestor()

func checkSleepMethod(count int, duration time.Duration, expected string, method string) {
	if sleepCount != count {
		testT.Fatal("unexpected sleep count")
	}
	if sleepDuration != duration*time.Second {
		testT.Fatal("unexpected sleep duration")
	}
	if method != expected {
		testT.Error("unexpected method")
	}
}

// There are four test blocks, 0..3
// To make the tests easier to understand, we fake out the hash of the block at height X
// by just prepending "0000" to X in string form. For example, the "hash" of block 380640
// is "0000380640". It may be better to make the (fake) hashes 32 bytes (64 characters),
// and that may be required in the future, but for now this works okay.
func blockIngestorStub(ctx context.Context, method string, params []json.RawMessage) (json.RawMessage, error) {
	var arg string
	if len(params) > 1 {
		err := json.Unmarshal(params[0], &arg)
		if err != nil {
			testT.Fatal("could not unmarshal", method, "arg:", params[0])
		}
	}
	step++
	// request the first two blocks very quickly (syncing),
	// then next block isn't yet available
	switch step {
	case 1:
		checkSleepMethod(0, 0, "getbestblockhash", method)
		// This hash doesn't matter, won't match anything
		r, _ := json.Marshal(strings.Repeat("01", 32))
		return r, nil
	case 2:
		checkSleepMethod(0, 0, "getblock", method)
		if arg != "380640" {
			testT.Fatal("incorrect height requested")
		}
		// height 380640
		return []byte("{\"Tx\": [\"" + testTxid + "\"], \"Hash\": \"" + testBlockid40 + "\"}"), nil
	case 3:
		checkSleepMethod(0, 0, "getblock", method)
		if arg != testBlockid40 {
			testT.Fatal("incorrect hash requested")
		}
		return blocks[0], nil
	case 4:
		checkSleepMethod(0, 0, "getbestblockhash", method)
		// This hash doesn't matter, won't match anything
		r, _ := json.Marshal(strings.Repeat("01", 32))
		return r, nil
	case 5:
		checkSleepMethod(0, 0, "getblock", method)
		if arg != "380641" {
			testT.Fatal("incorrect height requested")
		}
		// height 380641
		return []byte("{\"Tx\": [\"" + testTxid + "\"], \"Hash\": \"" + testBlockid41 + "\"}"), nil
	case 6:
		checkSleepMethod(0, 0, "getblock", method)
		if arg != testBlockid41 {
			testT.Fatal("incorrect hash requested")
		}
		return blocks[1], nil
	case 7:
		// Return the expected block hash, so we're synced, should
		// then sleep for 2 seconds, then another getbestblockhash
		checkSleepMethod(0, 0, "getbestblockhash", method)
		r, _ := json.Marshal(displayHash(testcache.GetLatestHash()))
		return r, nil
	case 8:
		// Simulate still no new block, still synced, should
		// sleep for 2 seconds, then another getbestblockhash
		checkSleepMethod(1, 2, "getbestblockhash", method)
		r, _ := json.Marshal(displayHash(testcache.GetLatestHash()))
		return r, nil
	case 9:
		// Simulate new block (any non-matching hash will do)
		checkSleepMethod(2, 4, "getbestblockhash", method)
		r, _ := json.Marshal(strings.Repeat("ab", 32))
		return r, nil
	case 10:
		checkSleepMethod(2, 4, "getblock", method)
		if arg != "380642" {
			testT.Fatal("incorrect height requested")
		}
		// height 380642
		return []byte("{\"Tx\": [\"" + testTxid + "\"], \"Hash\": \"" + testBlockid42 + "\"}"), nil
	case 11:
		checkSleepMethod(2, 4, "getblock", method)
		if arg != testBlockid42 {
			testT.Fatal("incorrect hash requested")
		}
		return blocks[2], nil
	case 12:
		// Simulate still no new block, still synced, should
		// sleep for 2 seconds, then another getbestblockhash
		checkSleepMethod(2, 4, "getbestblockhash", method)
		r, _ := json.Marshal(displayHash(testcache.GetLatestHash()))
		return r, nil
	case 13:
		// There are 3 blocks in the cache (380640-642), so let's
		// simulate a 1-block reorg, new version (replacement) of 380642
		checkSleepMethod(3, 6, "getbestblockhash", method)
		// hash doesn't matter, just something that doesn't match
		r, _ := json.Marshal(strings.Repeat("45", 32))
		return r, nil
	case 14:
		// It thinks there may simply be a new block, but we'll say
		// there is no block at this height (380642 was replaced).
		checkSleepMethod(3, 6, "getblock", method)
		if arg != "380643" {
			testT.Fatal("incorrect height requested")
		}
		return nil, errors.New("-8: Block height out of range")
	case 15:
		// It will re-ask the best hash (let's make no change)
		checkSleepMethod(3, 6, "getbestblockhash", method)
		// hash doesn't matter, just something that doesn't match
		r, _ := json.Marshal(strings.Repeat("45", 32))
		return r, nil
	case 16:
		// It should have backed up one block
		checkSleepMethod(3, 6, "getblock", method)
		if arg != "380642" {
			testT.Fatal("incorrect height requested")
		}
		// height 380642
		return []byte("{\"Tx\": [\"" + testTxid + "\"], \"Hash\": \"" + testBlockid42 + "\"}"), nil
	case 17:
		checkSleepMethod(3, 6, "getblock", method)
		if arg != testBlockid42 {
			testT.Fatal("incorrect height requested")
		}
		return blocks[2], nil
	case 18:
		// We're back to the same state as case 9, and this time
		// we'll make it back up 2 blocks (rather than one)
		checkSleepMethod(3, 6, "getbestblockhash", method)
		// hash doesn't matter, just something that doesn't match
		r, _ := json.Marshal(strings.Repeat("56", 32))
		return r, nil
	case 19:
		// It thinks there may simply be a new block, but we'll say
		// there is no block at this height (380642 was replaced).
		checkSleepMethod(3, 6, "getblock", method)
		if arg != "380643" {
			testT.Fatal("incorrect height requested")
		}
		return nil, errors.New("-8: Block height out of range")
	case 20:
		checkSleepMethod(3, 6, "getbestblockhash", method)
		// hash doesn't matter, just something that doesn't match
		r, _ := json.Marshal(strings.Repeat("56", 32))
		return r, nil
	case 21:
		// Like case 13, it should have backed up one block, but
		// this time we'll make it back up one more
		checkSleepMethod(3, 6, "getblock", method)
		if arg != "380642" {
			testT.Fatal("incorrect height requested")
		}
		return nil, errors.New("-8: Block height out of range")
	case 22:
		checkSleepMethod(3, 6, "getbestblockhash", method)
		// hash doesn't matter, just something that doesn't match
		r, _ := json.Marshal(strings.Repeat("56", 32))
		return r, nil
	case 23:
		// It should have backed up one more
		checkSleepMethod(3, 6, "getblock", method)
		if arg != "380641" {
			testT.Fatal("incorrect height requested")
		}
		return []byte("{\"Tx\": [\"" + testTxid + "\"], \"Hash\": \"" + testBlockid41 + "\"}"), nil
	case 24:
		checkSleepMethod(3, 6, "getblock", method)
		if arg != testBlockid41 {
			testT.Fatal("incorrect height requested")
		}
		return blocks[1], nil
	}
	testT.Error("blockIngestorStub called too many times")
	return nil, nil
}

func TestBlockIngestor(t *testing.T) {
	testT = t
	RawRequest = blockIngestorStub
	defer resetGlobals()
	Time.Sleep = sleepStub
	Time.Now = nowStub
	os.RemoveAll(unitTestPath)
	testcache = NewBlockCache(unitTestPath, unitTestChain, 380640, -1)
	BlockIngestor(testcache, 11)
	if step != 24 {
		t.Error("unexpected final step", step)
	}
	os.RemoveAll(unitTestPath)
}

// ------------------------------------------ GetBlockRange()

// There are four test blocks, 0..3
// (probably don't need all these cases)
func getblockStub(ctx context.Context, method string, params []json.RawMessage) (json.RawMessage, error) {
	if method != "getblock" {
		testT.Error("unexpected method")
	}
	var arg string
	err := json.Unmarshal(params[0], &arg)
	if err != nil {
		testT.Fatal("could not unmarshal height")
	}

	step++
	switch step {
	case 1:
		return []byte("{\"Tx\": [\"" + testTxid + "\"], \"Hash\": \"" + testBlockid40 + "\"}"), nil
	case 2:
		if arg != testBlockid40 {
			testT.Error("unexpected hash")
		}
		// Sunny-day
		return blocks[0], nil
	case 3:
		return []byte("{\"Tx\": [\"" + testTxid + "\"], \"Hash\": \"" + testBlockid41 + "\"}"), nil
	case 4:
		if arg != testBlockid41 {
			testT.Error("unexpected hash")
		}
		// Sunny-day
		return blocks[1], nil
	case 5:
		if arg != "380642" {
			testT.Error("unexpected height")
		}
		// Simulate that we're synced (caught up, latest block 380641).
		return nil, errors.New("-8: Block height out of range")
	}
	testT.Error("getblockStub called too many times")
	return nil, nil
}

func TestGetBlockRange(t *testing.T) {
	testT = t
	RawRequest = getblockStub
	defer resetGlobals()
	os.RemoveAll(unitTestPath)
	testcache = NewBlockCache(unitTestPath, unitTestChain, 380640, 0)
	blockChan := make(chan *walletrpc.CompactBlock)
	errChan := make(chan error)
	blockRange := &walletrpc.BlockRange{
		Start: &walletrpc.BlockID{Height: 380640},
		End:   &walletrpc.BlockID{Height: 380642},
	}
	go GetBlockRange(context.Background(), testcache, blockChan, errChan, blockRange)

	// read in block 380640
	select {
	case err := <-errChan:
		// this will also catch context.DeadlineExceeded from the timeout
		t.Fatal("unexpected error:", err)
	case cBlock := <-blockChan:
		if cBlock.Height != 380640 {
			t.Fatal("unexpected Height:", cBlock.Height)
		}
	}

	// read in block 380641
	select {
	case err := <-errChan:
		// this will also catch context.DeadlineExceeded from the timeout
		t.Fatal("unexpected error:", err)
	case cBlock := <-blockChan:
		if cBlock.Height != 380641 {
			t.Fatal("unexpected Height:", cBlock.Height)
		}
	}

	// try to read in block 380642, but this will fail (see case 5 above)
	select {
	case err := <-errChan:
		// this will also catch context.DeadlineExceeded from the timeout
		if !strings.Contains(err.Error(), "newer than the latest block") {
			t.Fatal("unexpected error:", err)
		}
	case <-blockChan:
		t.Fatal("reading height 380642 should have failed")
	}

	if step != 5 {
		t.Fatal("unexpected step:", step)
	}
	os.RemoveAll(unitTestPath)
}

func TestGetBlockRangeCancelsInFlightRPC(t *testing.T) {
	RawRequest = func(ctx context.Context, method string, params []json.RawMessage) (json.RawMessage, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	defer resetGlobals()
	os.RemoveAll(unitTestPath)
	testcache = NewBlockCache(unitTestPath, unitTestChain, 380640, 0)
	ctx, cancel := context.WithCancel(context.Background())
	blockChan := make(chan *walletrpc.CompactBlock)
	errChan := make(chan error, 1)
	blockRange := &walletrpc.BlockRange{
		Start: &walletrpc.BlockID{Height: 380640},
		End:   &walletrpc.BlockID{Height: 380640},
	}
	done := make(chan struct{})
	go func() {
		GetBlockRange(ctx, testcache, blockChan, errChan, blockRange)
		close(done)
	}()
	cancel()
	select {
	case <-time.After(2 * time.Second):
		t.Fatal("GetBlockRange did not exit after context cancellation")
	case <-done:
	}
	os.RemoveAll(unitTestPath)
}

// There are four test blocks, 0..3
func getblockStubReverse(ctx context.Context, method string, params []json.RawMessage) (json.RawMessage, error) {
	var arg string
	err := json.Unmarshal(params[0], &arg)
	if err != nil {
		testT.Fatal("could not unmarshal arg")
	}

	step++
	switch step {
	case 1:
		if arg != "380642" {
			testT.Error("unexpected height")
		}
		// Sunny-day
		return []byte("{\"Tx\": [\"" + testTxid + "\"], \"Hash\": \"" + testBlockid42 + "\"}"), nil
	case 2:
		if arg != testBlockid42 {
			testT.Error("unexpected hash")
		}
		return blocks[2], nil
	case 3:
		if arg != "380641" {
			testT.Error("unexpected height")
		}
		// Sunny-day
		return []byte("{\"Tx\": [\"" + testTxid + "\"], \"Hash\": \"" + testBlockid41 + "\"}"), nil
	case 4:
		if arg != testBlockid41 {
			testT.Error("unexpected hash")
		}
		return blocks[1], nil
	case 5:
		if arg != "380640" {
			testT.Error("unexpected height")
		}
		// Sunny-day
		return []byte("{\"Tx\": [\"" + testTxid + "\"], \"Hash\": \"" + testBlockid40 + "\"}"), nil
	case 6:
		if arg != testBlockid40 {
			testT.Error("unexpected hash")
		}
		return blocks[0], nil
	}
	testT.Error("getblockStub called too many times")
	return nil, nil
}

func TestGetBlockRangeReverse(t *testing.T) {
	testT = t
	RawRequest = getblockStubReverse
	defer resetGlobals()
	os.RemoveAll(unitTestPath)
	testcache = NewBlockCache(unitTestPath, unitTestChain, 380640, 0)
	blockChan := make(chan *walletrpc.CompactBlock)
	errChan := make(chan error)

	// Request the blocks in reverse order by specifying start greater than end
	blockRange := &walletrpc.BlockRange{
		Start: &walletrpc.BlockID{Height: 380642},
		End:   &walletrpc.BlockID{Height: 380640},
	}
	go GetBlockRange(context.Background(), testcache, blockChan, errChan, blockRange)

	// read in block 380642
	select {
	case err := <-errChan:
		// this will also catch context.DeadlineExceeded from the timeout
		t.Fatal("unexpected error:", err)
	case cBlock := <-blockChan:
		if cBlock.Height != 380642 {
			t.Fatal("unexpected Height:", cBlock.Height)
		}
	}

	// read in block 380641
	select {
	case err := <-errChan:
		// this will also catch context.DeadlineExceeded from the timeout
		t.Fatal("unexpected error:", err)
	case cBlock := <-blockChan:
		if cBlock.Height != 380641 {
			t.Fatal("unexpected Height:", cBlock.Height)
		}
	}

	// read in block 380640
	select {
	case err := <-errChan:
		// this will also catch context.DeadlineExceeded from the timeout
		t.Fatal("unexpected error:", err)
	case cBlock := <-blockChan:
		if cBlock.Height != 380640 {
			t.Fatal("unexpected Height:", cBlock.Height)
		}
	}
	if step != 6 {
		t.Fatal("unexpected step:", step)
	}
	os.RemoveAll(unitTestPath)
}

func TestGenerateCerts(t *testing.T) {
	if GenerateCerts() == nil {
		t.Fatal("GenerateCerts returned nil")
	}
}

// ------------------------------------------ GetMempoolStream

// Note that in mocking zcashd's RPC replies here, we don't really need
// actual txids or transactions, or even strings with the correct format
// for those, except that a transaction must be a hex string.
func mempoolStub(ctx context.Context, method string, params []json.RawMessage) (json.RawMessage, error) {
	step++
	switch step {
	case 1:
		// This will be a getblockchaininfo request
		if method != "getblockchaininfo" {
			testT.Fatal("expecting blockchaininfo")
		}
		r, _ := json.Marshal(&ZcashdRpcReplyGetblockchaininfo{
			BestBlockHash: "010203",
			Blocks:        200,
		})
		return r, nil
	case 2:
		// No new block has arrived.
		if method != "getblockchaininfo" {
			testT.Fatal("expecting blockchaininfo")
		}
		r, _ := json.Marshal(&ZcashdRpcReplyGetblockchaininfo{
			BestBlockHash: "010203",
			Blocks:        200,
		})
		return r, nil
	case 3:
		// Expect a getrawmempool next.
		if method != "getrawmempool" {
			testT.Fatal("expecting getrawmempool")
		}
		// In reality, this would be a hex txid
		r, _ := json.Marshal([]string{
			"mempooltxid-1",
		})
		return r, nil
	case 4:
		// Next, it should ask for this transaction (non-verbose).
		if method != "getrawtransaction" {
			testT.Fatal("expecting getrawtransaction")
		}
		var txid string
		json.Unmarshal(params[0], &txid)
		if txid != "mempooltxid-1" {
			testT.Fatal("unexpected txid")
		}
		r, _ := json.Marshal(map[string]string{"hex": "aabb"})
		return r, nil
	case 5:
		// Simulate that still no new block has arrived ...
		if method != "getblockchaininfo" {
			testT.Fatal("expecting blockchaininfo")
		}
		r, _ := json.Marshal(&ZcashdRpcReplyGetblockchaininfo{
			BestBlockHash: "010203",
			Blocks:        200,
		})
		return r, nil
	case 6:
		// ... but there a second tx has arrived in the mempool
		if method != "getrawmempool" {
			testT.Fatal("expecting getrawmempool")
		}
		// In reality, this would be a hex txid
		r, _ := json.Marshal([]string{
			"mempooltxid-2",
			"mempooltxid-1"})
		return r, nil
	case 7:
		// The new mempool tx (and only that one) gets fetched
		if method != "getrawtransaction" {
			testT.Fatal("expecting getrawtransaction")
		}
		var txid string
		json.Unmarshal(params[0], &txid)
		if txid != "mempooltxid-2" {
			testT.Fatal("unexpected txid")
		}
		r, _ := json.Marshal(map[string]string{"hex": "ccdd"})
		return r, nil
	case 8:
		// A new block arrives, this will cause these two tx to be returned
		if method != "getblockchaininfo" {
			testT.Fatal("expecting blockchaininfo")
		}
		r, _ := json.Marshal(&ZcashdRpcReplyGetblockchaininfo{
			BestBlockHash: "d1d2d3",
			Blocks:        201,
		})
		return r, nil
	}
	testT.Fatal("ran out of cases")
	return nil, nil
}

func TestMempoolStream(t *testing.T) {
	testT = t
	RawRequest = mempoolStub
	defer resetGlobals()
	Time.Sleep = sleepStub
	Time.Now = nowStub
	Time.After = afterStub
	// In real life, wall time is not close to zero, simulate that.
	sleepDuration = 1000 * time.Second

	var replies []*walletrpc.RawTransaction
	// The first request after startup immediately returns an empty list.
	err := GetMempool(context.Background(), func(tx *walletrpc.RawTransaction) error {
		t.Fatal("send to client function called on initial GetMempool call")
		return nil
	})
	if err != nil {
		t.Errorf("GetMempool failed: %v", err)
	}

	// This should return two transactions.
	err = GetMempool(context.Background(), func(tx *walletrpc.RawTransaction) error {
		replies = append(replies, tx)
		return nil
	})
	if err != nil {
		t.Errorf("GetMempool failed: %v", err)
	}
	if len(replies) != 2 {
		t.Fatal("unexpected number of tx")
	}
	// The interface guarantees that the transactions will be returned
	// in the order they entered the mempool.
	if !bytes.Equal([]byte(replies[0].GetData()), []byte{0xaa, 0xbb}) {
		t.Fatal("unexpected tx contents")
	}
	if replies[0].GetHeight() != 0 {
		t.Fatal("unexpected tx height")
	}
	if !bytes.Equal([]byte(replies[1].GetData()), []byte{0xcc, 0xdd}) {
		t.Fatal("unexpected tx contents")
	}
	if replies[1].GetHeight() != 0 {
		t.Fatal("unexpected tx height")
	}

	// Time started at 1000 seconds (since 1970), and just over 4 seconds
	// should have elapsed. The units here are nanoseconds.
	if sleepDuration != 1004400000000 {
		t.Fatal("unexpected end time")
	}
	if step != 8 {
		t.Fatal("unexpected number of zebrad RPCs")
	}
}

func TestZcashdRpcReplyUnmarshalling(t *testing.T) {
	var txinfo0 ZcashdRpcReplyGetrawtransaction
	err0 := json.Unmarshal([]byte("{\"hex\": \"deadbeef\", \"height\": 123456}"), &txinfo0)
	if err0 != nil {
		t.Fatal("Failed to unmarshal tx with known height.")
	}
	if txinfo0.Height != 123456 {
		t.Errorf("Unmarshalled incorrect height: got: %d, want: 123456.", txinfo0.Height)
	}

	var txinfo1 ZcashdRpcReplyGetrawtransaction
	err1 := json.Unmarshal([]byte("{\"hex\": \"deadbeef\", \"height\": -1}"), &txinfo1)
	if err1 != nil {
		t.Fatal("failed to unmarshal tx not in main chain")
	}
	if txinfo1.Height != -1 {
		t.Errorf("Unmarshalled incorrect height: got: %d, want: -1.", txinfo1.Height)
	}

	var txinfo2 ZcashdRpcReplyGetrawtransaction
	err2 := json.Unmarshal([]byte("{\"hex\": \"deadbeef\"}"), &txinfo2)
	if err2 != nil {
		t.Fatal("failed to unmarshal reply lacking height data")
	}
	if txinfo2.Height != 0 {
		t.Errorf("Unmarshalled incorrect height: got: %d, want: 0.", txinfo2.Height)
	}
}

func TestParseRawTransaction(t *testing.T) {
	rt0, err0 := ParseRawTransaction([]byte("{\"hex\": \"deadbeef\", \"height\": 123456}"))
	if err0 != nil {
		t.Fatal("Failed to parse raw transaction response with known height.")
	}
	if rt0.Height != 123456 {
		t.Errorf("Unmarshalled incorrect height: got: %d, expected: 123456.", rt0.Height)
	}

	rt1, err1 := ParseRawTransaction([]byte("{\"hex\": \"deadbeef\", \"height\": -1}"))
	if err1 != nil {
		t.Fatal("Failed to parse raw transaction response for a known tx not in the main chain.")
	}
	// We expect the int64 value `-1` to have been reinterpreted as a uint64 value in order
	// to be representable as a uint64 in `RawTransaction`. The conversion from the twos-complement
	// signed representation should map `-1` to `math.MaxUint64`.
	if rt1.Height != math.MaxUint64 {
		t.Errorf("Unmarshalled incorrect height: got: %d, want: 0x%X.", rt1.Height, uint64(math.MaxUint64))
	}

	rt2, err2 := ParseRawTransaction([]byte("{\"hex\": \"deadbeef\"}"))
	if err2 != nil {
		t.Fatal("Failed to parse raw transaction response for a tx in the mempool.")
	}
	if rt2.Height != 0 {
		t.Errorf("Unmarshalled incorrect height: got: %d, expected: 0.", rt2.Height)
	}
}

// TestMempoolStreamCancelOnEmptyMempool is the regression test for the fix in
// this PR. Without the fix, GetMempool on an empty mempool with stable tip
// hash never observes ctx.Done because sendToClient is never invoked and the
// 200ms Time.Sleep is non-cancellable. With the fix, the cancel-aware select
// at the bottom of the loop returns promptly with ctx.Err().
func TestMempoolStreamCancelOnEmptyMempool(t *testing.T) {
	// Stub RawRequest to return a stable empty-mempool / stable-tip world,
	// so the loop parks at the cancel-aware sleep with no work and no tip
	// change to break out.
	RawRequest = func(ctx context.Context, method string, params []json.RawMessage) (json.RawMessage, error) {
		switch method {
		case "getblockchaininfo":
			r, _ := json.Marshal(&ZcashdRpcReplyGetblockchaininfo{
				BestBlockHash: "stable-hash",
				Blocks:        100,
			})
			return r, nil
		case "getrawmempool":
			return json.RawMessage("[]"), nil
		}
		return nil, errors.New("unexpected RPC: " + method)
	}
	defer resetGlobals()
	// Real time for the cancel-aware sleep so ctx.Done has a real race with
	// the 200ms timer. afterStub would fire instantly and the test would not
	// exercise the cancel path deterministically.
	Time.After = time.After
	Time.Sleep = sleepStub
	Time.Now = time.Now

	// Pre-populate the package-global tip cache so the first refresh matches
	// the stubbed tip and does NOT trigger the tip-changed branch (which
	// would break out of the loop immediately).
	g_lastBlockChainInfo = &ZcashdRpcReplyGetblockchaininfo{BestBlockHash: "stable-hash"}
	g_lastTime = time.Time{}
	g_txidSeen = map[txid]struct{}{}
	g_txList = []*walletrpc.RawTransaction{}

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)
	go func() {
		done <- GetMempool(ctx, func(tx *walletrpc.RawTransaction) error {
			t.Error("sendToClient must not be invoked on empty mempool")
			return nil
		})
	}()

	// Let the loop reach the cancel-aware select.
	time.Sleep(100 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		if err != context.Canceled {
			t.Fatalf("expected context.Canceled, got %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("GetMempool did not return within 2s of cancel; the cancel-aware select is not effective")
	}
}

func TestFilterTxPoolIronwood(t *testing.T) {
	tx := &walletrpc.CompactTx{
		Index: 7,
		Txid:  []byte{1, 2, 3},
		Actions: []*walletrpc.CompactOrchardAction{
			{Nullifier: []byte{4}},
		},
		IronwoodActions: []*walletrpc.CompactOrchardAction{
			{Nullifier: []byte{5}},
		},
	}

	orchardOnly := FilterTxPool(tx, []walletrpc.PoolType{walletrpc.PoolType_ORCHARD})
	if orchardOnly == nil || len(orchardOnly.Actions) != 1 || len(orchardOnly.IronwoodActions) != 0 {
		t.Fatal("orchard-only filter returned unexpected actions")
	}

	ironwoodOnly := FilterTxPool(tx, []walletrpc.PoolType{walletrpc.PoolType_IRONWOOD})
	if ironwoodOnly == nil || len(ironwoodOnly.Actions) != 0 || len(ironwoodOnly.IronwoodActions) != 1 {
		t.Fatal("ironwood-only filter returned unexpected actions")
	}

	defaultFiltered := filterBlockPool([]*walletrpc.CompactTx{tx}, nil)
	if len(defaultFiltered) != 1 || len(defaultFiltered[0].IronwoodActions) != 1 {
		t.Fatal("default shielded filter should keep ironwood actions")
	}
}
