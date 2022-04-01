// Copyright (c) 2019-2020 The Zcash developers
// Distributed under the MIT software license, see the accompanying
// file COPYING or https://www.opensource.org/licenses/mit-license.php .
package common

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"github.com/zcash/lightwalletd/walletrpc"
)

// ------------------------------------------ Setup
//
// This section does some setup things that may (even if not currently)
// be useful across multiple tests.

var (
	testT *testing.T

	// The various stub callbacks need to sequence through states
	step int

	getblockchaininfoReply []byte
	logger                 = logrus.New()

	blocks [][]byte // four test blocks

	testcache *BlockCache
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

// ------------------------------------------ GetLightdInfo()

func getLightdInfoStub(method string, params []json.RawMessage) (json.RawMessage, error) {
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
		})
		return r, nil
	}
	return nil, nil
}

func TestGetLightdInfo(t *testing.T) {
	testT = t
	RawRequest = getLightdInfoStub
	Time.Sleep = sleepStub
	// This calls the getblockchaininfo rpc just to establish connectivity with zcashd
	FirstRPC()

	// Ensure the retry happened as expected
	logFile, err := ioutil.ReadFile("test-log")
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

	if sleepCount != 1 || sleepDuration != 15*time.Second {
		t.Error("unexpected sleeps", sleepCount, sleepDuration)
	}
	step = 0
	sleepCount = 0
	sleepDuration = 0
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
func blockIngestorStub(method string, params []json.RawMessage) (json.RawMessage, error) {
	step++
	// request the first two blocks very quickly (syncing),
	// then next block isn't yet available
	switch step {
	case 1:
		checkSleepMethod(0, 0, "getbestblockhash", method)
		// This hash doesn't matter, won't match anything
		r, _ := json.Marshal("010101")
		return r, nil
	case 2:
		checkSleepMethod(0, 0, "getblock", method)
		var height string
		err := json.Unmarshal(params[0], &height)
		if err != nil {
			testT.Fatal("could not unmarshal height")
		}
		if height != "380640" {
			testT.Fatal("incorrect height requested")
		}
		// height 380640
		return blocks[0], nil
	case 3:
		checkSleepMethod(0, 0, "getbestblockhash", method)
		// This hash doesn't matter, won't match anything
		r, _ := json.Marshal("010101")
		return r, nil
	case 4:
		checkSleepMethod(0, 0, "getblock", method)
		var height string
		err := json.Unmarshal(params[0], &height)
		if err != nil {
			testT.Fatal("could not unmarshal height")
		}
		if height != "380641" {
			testT.Fatal("incorrect height requested")
		}
		// height 380641
		return blocks[1], nil
	case 5:
		// Return the expected block hash, so we're synced, should
		// then sleep for 2 seconds, then another getbestblockhash
		checkSleepMethod(0, 0, "getbestblockhash", method)
		r, _ := json.Marshal(displayHash(testcache.GetLatestHash()))
		return r, nil
	case 6:
		// Simulate still no new block, still synced, should
		// sleep for 2 seconds, then another getbestblockhash
		checkSleepMethod(1, 2, "getbestblockhash", method)
		r, _ := json.Marshal(displayHash(testcache.GetLatestHash()))
		return r, nil
	case 7:
		// Simulate new block (any non-matching hash will do)
		checkSleepMethod(2, 4, "getbestblockhash", method)
		r, _ := json.Marshal("aabb")
		return r, nil
	case 8:
		checkSleepMethod(2, 4, "getblock", method)
		var height string
		err := json.Unmarshal(params[0], &height)
		if err != nil {
			testT.Fatal("could not unmarshal height")
		}
		if height != "380642" {
			testT.Fatal("incorrect height requested")
		}
		// height 380642
		return blocks[2], nil
	case 9:
		// Simulate still no new block, still synced, should
		// sleep for 2 seconds, then another getbestblockhash
		checkSleepMethod(2, 4, "getbestblockhash", method)
		r, _ := json.Marshal(displayHash(testcache.GetLatestHash()))
		return r, nil
	case 10:
		// There are 3 blocks in the cache (380640-642), so let's
		// simulate a 1-block reorg, new version (replacement) of 380642
		checkSleepMethod(3, 6, "getbestblockhash", method)
		// hash doesn't matter, just something that doesn't match
		r, _ := json.Marshal("4545")
		return r, nil
	case 11:
		// It thinks there may simply be a new block, but we'll say
		// there is no block at this height (380642 was replaced).
		checkSleepMethod(3, 6, "getblock", method)
		var height string
		err := json.Unmarshal(params[0], &height)
		if err != nil {
			testT.Fatal("could not unmarshal height")
		}
		if height != "380643" {
			testT.Fatal("incorrect height requested")
		}
		return nil, errors.New("-8: Block height out of range")
	case 12:
		// It will re-ask the best hash (let's make no change)
		checkSleepMethod(3, 6, "getbestblockhash", method)
		// hash doesn't matter, just something that doesn't match
		r, _ := json.Marshal("4545")
		return r, nil
	case 13:
		// It should have backed up one block
		checkSleepMethod(3, 6, "getblock", method)
		var height string
		err := json.Unmarshal(params[0], &height)
		if err != nil {
			testT.Fatal("could not unmarshal height")
		}
		if height != "380642" {
			testT.Fatal("incorrect height requested")
		}
		// height 380642
		return blocks[2], nil
	case 14:
		// We're back to the same state as case 9, and this time
		// we'll make it back up 2 blocks (rather than one)
		checkSleepMethod(3, 6, "getbestblockhash", method) // XXXXXXXXXXXXXXXXXXXXXXXXXXXXX XXX
		// hash doesn't matter, just something that doesn't match
		r, _ := json.Marshal("5656")
		return r, nil
	case 15:
		// It thinks there may simply be a new block, but we'll say
		// there is no block at this height (380642 was replaced).
		checkSleepMethod(3, 6, "getblock", method)
		var height string
		err := json.Unmarshal(params[0], &height)
		if err != nil {
			testT.Fatal("could not unmarshal height")
		}
		if height != "380643" {
			testT.Fatal("incorrect height requested")
		}
		return nil, errors.New("-8: Block height out of range")
	case 16:
		checkSleepMethod(3, 6, "getbestblockhash", method)
		// hash doesn't matter, just something that doesn't match
		r, _ := json.Marshal("5656")
		return r, nil
	case 17:
		// Like case 13, it should have backed up one block, but
		// this time we'll make it back up one more
		checkSleepMethod(3, 6, "getblock", method)
		var height string
		err := json.Unmarshal(params[0], &height)
		if err != nil {
			testT.Fatal("could not unmarshal height")
		}
		if height != "380642" {
			testT.Fatal("incorrect height requested")
		}
		return nil, errors.New("-8: Block height out of range")
	case 18:
		checkSleepMethod(3, 6, "getbestblockhash", method)
		// hash doesn't matter, just something that doesn't match
		r, _ := json.Marshal("5656")
		return r, nil
	case 19:
		// It should have backed up one more
		checkSleepMethod(3, 6, "getblock", method)
		var height string
		err := json.Unmarshal(params[0], &height)
		if err != nil {
			testT.Fatal("could not unmarshal height")
		}
		if height != "380641" {
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
	Time.Sleep = sleepStub
	Time.Now = nowStub
	os.RemoveAll(unitTestPath)
	testcache = NewBlockCache(unitTestPath, unitTestChain, 380640, -1)
	BlockIngestor(testcache, 11)
	if step != 19 {
		t.Error("unexpected final step", step)
	}
	step = 0
	sleepCount = 0
	sleepDuration = 0
	os.RemoveAll(unitTestPath)
}

// ------------------------------------------ GetBlockRange()

// There are four test blocks, 0..3
// (probably don't need all these cases)
func getblockStub(method string, params []json.RawMessage) (json.RawMessage, error) {
	if method != "getblock" {
		testT.Error("unexpected method")
	}
	var height string
	err := json.Unmarshal(params[0], &height)
	if err != nil {
		testT.Fatal("could not unmarshal height")
	}

	step++
	switch step {
	case 1:
		if height != "380640" {
			testT.Error("unexpected height")
		}
		// Sunny-day
		return blocks[0], nil
	case 2:
		if height != "380641" {
			testT.Error("unexpected height")
		}
		// Sunny-day
		return blocks[1], nil
	case 3:
		if height != "380642" {
			testT.Error("unexpected height", height)
		}
		// Simulate that we're synced (caught up);
		// this should cause one 10s sleep (then retry).
		return nil, errors.New("-8: Block height out of range")
	case 4:
		if sleepCount != 1 || sleepDuration != 2*time.Second {
			testT.Error("unexpected sleeps", sleepCount, sleepDuration)
		}
		if height != "380642" {
			testT.Error("unexpected height", height)
		}
		// Simulate that we're still caught up; this should cause a 1s
		// wait then a check for reorg to shorter chain (back up one).
		return nil, errors.New("-8: Block height out of range")
	case 5:
		if sleepCount != 1 || sleepDuration != 2*time.Second {
			testT.Error("unexpected sleeps", sleepCount, sleepDuration)
		}
		// Back up to 41.
		if height != "380641" {
			testT.Error("unexpected height", height)
		}
		// Return the expected block (as normally happens, no actual reorg),
		// ingestor will immediately re-request the next block (42).
		return blocks[1], nil
	case 6:
		if sleepCount != 1 || sleepDuration != 2*time.Second {
			testT.Error("unexpected sleeps", sleepCount, sleepDuration)
		}
		if height != "380642" {
			testT.Error("unexpected height", height)
		}
		// Block 42 has now finally appeared, it will immediately ask for 43.
		return blocks[2], nil
	case 7:
		if sleepCount != 1 || sleepDuration != 2*time.Second {
			testT.Error("unexpected sleeps", sleepCount, sleepDuration)
		}
		if height != "380643" {
			testT.Error("unexpected height", height)
		}
		// Simulate a reorg by modifying the block's hash temporarily,
		// this causes a 1s sleep and then back up one block (to 42).
		blocks[3][9]++ // first byte of the prevhash
		return blocks[3], nil
	case 8:
		blocks[3][9]-- // repair first byte of the prevhash
		if sleepCount != 1 || sleepDuration != 2*time.Second {
			testT.Error("unexpected sleeps", sleepCount, sleepDuration)
		}
		if height != "380642" {
			testT.Error("unexpected height ", height)
		}
		return blocks[2], nil
	case 9:
		if sleepCount != 1 || sleepDuration != 2*time.Second {
			testT.Error("unexpected sleeps", sleepCount, sleepDuration)
		}
		if height != "380643" {
			testT.Error("unexpected height ", height)
		}
		// Instead of returning expected (43), simulate block unmarshal
		// failure, should cause 10s sleep, retry
		return nil, nil
	case 10:
		if sleepCount != 2 || sleepDuration != 12*time.Second {
			testT.Error("unexpected sleeps", sleepCount, sleepDuration)
		}
		if height != "380643" {
			testT.Error("unexpected height ", height)
		}
		// Back to sunny-day
		return blocks[3], nil
	case 11:
		if sleepCount != 2 || sleepDuration != 12*time.Second {
			testT.Error("unexpected sleeps", sleepCount, sleepDuration)
		}
		if height != "380644" {
			testT.Error("unexpected height ", height)
		}
		// next block not ready
		return nil, nil
	}
	testT.Error("getblockStub called too many times")
	return nil, nil
}

func TestGetBlockRange(t *testing.T) {
	testT = t
	RawRequest = getblockStub
	os.RemoveAll(unitTestPath)
	testcache = NewBlockCache(unitTestPath, unitTestChain, 380640, 0)
	blockChan := make(chan *walletrpc.CompactBlock)
	errChan := make(chan error)
	go GetBlockRange(testcache, blockChan, errChan, 380640, 380642)

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

	// try to read in block 380642, but this will fail (see case 3 above)
	select {
	case err := <-errChan:
		// this will also catch context.DeadlineExceeded from the timeout
		if err.Error() != "block requested is newer than latest block" {
			t.Fatal("unexpected error:", err)
		}
	case _ = <-blockChan:
		t.Fatal("reading height 22 should have failed")
	}

	step = 0
	os.RemoveAll(unitTestPath)
}

// There are four test blocks, 0..3
func getblockStubReverse(method string, params []json.RawMessage) (json.RawMessage, error) {
	var height string
	err := json.Unmarshal(params[0], &height)
	if err != nil {
		testT.Fatal("could not unmarshal height")
	}

	step++
	switch step {
	case 1:
		if height != "380642" {
			testT.Error("unexpected height")
		}
		// Sunny-day
		return blocks[2], nil
	case 2:
		if height != "380641" {
			testT.Error("unexpected height")
		}
		// Sunny-day
		return blocks[1], nil
	case 3:
		if height != "380640" {
			testT.Error("unexpected height")
		}
		// Sunny-day
		return blocks[0], nil
	}
	testT.Error("getblockStub called too many times")
	return nil, nil
}

func TestGetBlockRangeReverse(t *testing.T) {
	testT = t
	RawRequest = getblockStubReverse
	os.RemoveAll(unitTestPath)
	testcache = NewBlockCache(unitTestPath, unitTestChain, 380640, 0)
	blockChan := make(chan *walletrpc.CompactBlock)
	errChan := make(chan error)

	// Request the blocks in reverse order by specifying start greater than end
	go GetBlockRange(testcache, blockChan, errChan, 380642, 380640)

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
	step = 0
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
func mempoolStub(method string, params []json.RawMessage) (json.RawMessage, error) {
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
		r, _ := json.Marshal("aabb")
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
		r, _ := json.Marshal("ccdd")
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
	Time.Sleep = sleepStub
	Time.Now = nowStub
	// In real life, wall time is not close to zero, simulate that.
	sleepDuration = 1000 * time.Second

	var replies []*walletrpc.RawTransaction
	// The first request after startup immediately returns an empty list.
	err := GetMempool(func(tx *walletrpc.RawTransaction) error {
		t.Fatal("send to client function called on initial GetMempool call")
		return nil
	})
	if err != nil {
		t.Fatal("GetMempool failed")
	}

	// This should return two transactions.
	err = GetMempool(func(tx *walletrpc.RawTransaction) error {
		replies = append(replies, tx)
		return nil
	})
	if err != nil {
		t.Fatal("GetMempool failed")
	}
	if len(replies) != 2 {
		t.Fatal("unexpected number of tx")
	}
	// The interface guarantees that the transactions will be returned
	// in the order they entered the mempool.
	if !bytes.Equal([]byte(replies[0].GetData()), []byte{0xaa, 0xbb}) {
		t.Fatal("unexpected tx contents")
	}
	if replies[0].GetHeight() != 200 {
		t.Fatal("unexpected tx height")
	}
	if !bytes.Equal([]byte(replies[1].GetData()), []byte{0xcc, 0xdd}) {
		t.Fatal("unexpected tx contents")
	}
	if replies[1].GetHeight() != 200 {
		t.Fatal("unexpected tx height")
	}

	// Time started at 1000 seconds (since 1970), and just over 4 seconds
	// should have elapsed. The units here are nanoseconds.
	if sleepDuration != 1004400000000 {
		t.Fatal("unexpected end time")
	}
	if step != 8 {
		t.Fatal("unexpected number of zcashd RPCs")
	}

	step = 0
	sleepCount = 0
	sleepDuration = 0
}
