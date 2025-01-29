// Copyright (c) 2019-2020 The Zcash developers
// Distributed under the MIT software license, see the accompanying
// file COPYING or https://www.opensource.org/licenses/mit-license.php .
package frontend

import (
	"bufio"
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/sirupsen/logrus"
	"github.com/zcash/lightwalletd/common"
	"github.com/zcash/lightwalletd/walletrpc"
)

var (
	testT  *testing.T
	logger = logrus.New()
	step   int

	blocks    [][]byte // four test blocks
	rawTxData [][]byte
)

const (
	unitTestPath  = "unittestcache"
	unitTestChain = "unittestnet"
	testTxid      = "1234000000000000000000000000000000000000000000000000000000000000"
	testBlockid   = "0000000000000000000000000000000000000000000000000000000000380640"
)

// block 380640 used here is a real block from testnet
func testsetup() (walletrpc.CompactTxStreamerServer, *common.BlockCache) {
	os.RemoveAll(unitTestPath)
	cache := common.NewBlockCache(unitTestPath, unitTestChain, 380640, 0)
	lwd, err := NewLwdStreamer(cache, "main", false /* enablePing */)
	if err != nil {
		os.Stderr.WriteString(fmt.Sprint("NewLwdStreamer failed:", err))
		os.Exit(1)
	}
	return lwd, cache
}

func TestMain(m *testing.M) {
	output, err := os.OpenFile("test-log", os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0644)
	if err != nil {
		os.Stderr.WriteString(fmt.Sprint("Cannot open test-log:", err))
		os.Exit(1)
	}
	logger.SetOutput(output)
	common.Log = logger.WithFields(logrus.Fields{
		"app": "test",
	})

	// Several tests need test blocks; read all 4 into memory just once
	// (for efficiency).
	testBlocks, err := os.Open("../testdata/blocks")
	if err != nil {
		os.Stderr.WriteString(fmt.Sprint("Error:", err))
		os.Exit(1)
	}
	defer testBlocks.Close()
	scan := bufio.NewScanner(testBlocks)
	for scan.Scan() { // each line (block)
		blockJSON, _ := json.Marshal(scan.Text())
		blocks = append(blocks, blockJSON)
	}

	testData, err := os.Open("../testdata/zip243_raw_tx")
	if err != nil {
		os.Stderr.WriteString(fmt.Sprint("Error:", err))
		os.Exit(1)
	}
	defer testData.Close()

	// Parse the raw transactions file
	rawTxData = [][]byte{}
	scan = bufio.NewScanner(testData)
	for scan.Scan() {
		dataLine := scan.Text()
		// Skip the comments
		if strings.HasPrefix(dataLine, "#") {
			continue
		}

		txData, err := hex.DecodeString(dataLine)
		if err != nil {
			os.Stderr.WriteString(fmt.Sprint("Error:", err))
			os.Exit(1)
		}

		rawTxData = append(rawTxData, txData)
	}

	// Setup is done; run all tests.
	exitcode := m.Run()

	// cleanup
	os.Remove("test-log")
	os.RemoveAll(unitTestPath)

	os.Exit(exitcode)
}

func TestGetTransaction(t *testing.T) {
	// GetTransaction() will mostly be tested below via TestGetTaddressTransactions
	lwd, _ := testsetup()

	rawtx, err := lwd.GetTransaction(context.Background(),
		&walletrpc.TxFilter{})
	if err == nil {
		testT.Fatal("GetTransaction unexpectedly succeeded")
	}
	if !strings.Contains(err.Error(), "GetTransaction: specify a txid") {
		testT.Fatal("GetTransaction unexpected error message")
	}
	if rawtx != nil {
		testT.Fatal("GetTransaction non-nil rawtx returned")
	}

	rawtx, err = lwd.GetTransaction(context.Background(),
		&walletrpc.TxFilter{Block: &walletrpc.BlockID{Hash: []byte{}}})
	if err == nil {
		testT.Fatal("GetTransaction unexpectedly succeeded")
	}
	if !strings.Contains(err.Error(), "GetTransaction: specify a txid, not a blockhash+num") {
		testT.Fatal("GetTransaction unexpected error message")
	}
	if rawtx != nil {
		testT.Fatal("GetTransaction non-nil rawtx returned")
	}
}

func getLatestBlockStub(method string, params []json.RawMessage) (json.RawMessage, error) {
	step++

	// Test retry logic (for the moment, it's very simple, just one retry).
	switch step {
	case 1:
		if method != "getblock" {
			testT.Fatal("unexpected method:", method)
		}
		var arg string
		err := json.Unmarshal(params[0], &arg)
		if err != nil {
			testT.Fatal("could not unmarshal height")
		}
		if arg != "380640" {
			testT.Fatal("unexpected getblock height", arg)
		}
		// verbose mode (getblock height 1), return transaction list
		return []byte("{\"Tx\": [\"" + testTxid + "\"], \"Hash\": \"" + testBlockid + "\"}"), nil
	case 2:
		if method != "getblock" {
			testT.Fatal("unexpected method:", method)
		}
		var arg string
		err := json.Unmarshal(params[0], &arg)
		if err != nil {
			testT.Fatal("could not unmarshal height")
		}
		if arg != testBlockid {
			testT.Fatal("unexpected getblock hash", arg)
		}
		return blocks[0], nil
	case 3:
		if method != "getblockchaininfo" {
			testT.Fatal("unexpected method:", method)
		}
		return []byte("{\"Blocks\": 380640, " +
			"\"BestBlockHash\": " +
			"\"000a5e44b3b238d0cc36de7c0cb1ae5ac6e16f8727173abd295a83ebfa073b91\"}"), nil

	case 4:
		return nil, errors.New("getblock test error, too many requests")
	}
	testT.Fatal("unexpected call to getLatestBlockStub")
	return nil, nil
}

func TestGetLatestBlock(t *testing.T) {
	testT = t
	common.RawRequest = getLatestBlockStub
	lwd, cache := testsetup()

	// This argument is not used (it may be in the future)
	req := &walletrpc.ChainSpec{}

	// This does zcashd rpc "getblock", calls getLatestBlockStub() above
	block, err := common.GetBlock(cache, 380640)
	if err != nil {
		t.Fatal("getBlockFromRPC failed", err)
	}
	if err = cache.Add(380640, block); err != nil {
		t.Fatal("cache.Add failed:", err)
	}
	blockID, err := lwd.GetLatestBlock(context.Background(), req)
	if err != nil {
		t.Fatal("lwd.GetLatestBlock failed", err)
	}
	if blockID.Height != 380640 {
		t.Fatal("unexpected blockID.height")
	}
	if string(blockID.Hash) != string(block.Hash) {
		t.Fatal("unexpected blockID.hash")
	}
	step = 0
}

// A valid address starts with "t", followed by 34 alpha characters;
// these should all be detected as invalid.
var addressTests = []string{
	"",                                      // too short
	"a",                                     // too short
	"t123456789012345678901234567890123",    // one byte too short
	"t12345678901234567890123456789012345",  // one byte too long
	"t123456789012345678901234567890123*",   // invalid "*"
	"s1234567890123456789012345678901234",   // doesn't start with "t"
	" t1234567890123456789012345678901234",  // extra stuff before
	"t1234567890123456789012345678901234 ",  // extra stuff after
	"\nt1234567890123456789012345678901234", // newline before
	"t1234567890123456789012345678901234\n", // newline after
}

func zcashdrpcStub(method string, params []json.RawMessage) (json.RawMessage, error) {
	step++
	switch method {
	case "getaddresstxids":
		var filter common.ZcashdRpcRequestGetaddresstxids
		err := json.Unmarshal(params[0], &filter)
		if err != nil {
			testT.Fatal("could not unmarshal block filter")
		}
		if len(filter.Addresses) != 1 {
			testT.Fatal("wrong number of addresses")
		}
		if filter.Addresses[0] != "t1234567890123456789012345678901234" {
			testT.Fatal("wrong address")
		}
		if filter.Start != 20 {
			testT.Fatal("wrong start")
		}
		if filter.End != 30 {
			testT.Fatal("wrong end")
		}
		return []byte("[\"6732cf8d67aac5b82a2a0f0217a7d4aa245b2adb0b97fd2d923dfc674415e221\"]"), nil
	case "getrawtransaction":
		switch step {
		case 2:
			tx := &common.ZcashdRpcReplyGetrawtransaction{
				Hex:    hex.EncodeToString(rawTxData[0]),
				Height: 1234567,
			}
			return json.Marshal(tx)
		case 4:
			// empty return value, should be okay
			return []byte(""), errors.New("-5: test getrawtransaction error")
		}
	}
	testT.Fatal("unexpected call to zcashdrpcStub")
	return nil, nil
}

type testgettx struct {
	walletrpc.CompactTxStreamer_GetTaddressTransactionsServer
}

func (tg *testgettx) Context() context.Context {
	return context.Background()
}

func (tg *testgettx) Send(tx *walletrpc.RawTransaction) error {
	if !bytes.Equal(tx.Data, rawTxData[0]) {
		testT.Fatal("mismatch transaction data")
	}
	if tx.Height != 1234567 {
		testT.Fatal("unexpected transaction height", tx.Height)
	}
	return nil
}

func TestGetTaddressTransactions(t *testing.T) {
	testT = t
	common.RawRequest = zcashdrpcStub
	lwd, _ := testsetup()

	addressBlockFilter := &walletrpc.TransparentAddressBlockFilter{
		Range: &walletrpc.BlockRange{
			Start: &walletrpc.BlockID{Height: 20},
			End:   &walletrpc.BlockID{Height: 30},
		},
	}

	// Ensure that a bad address is detected
	for i, addressTest := range addressTests {
		addressBlockFilter.Address = addressTest
		err := lwd.GetTaddressTransactions(addressBlockFilter, &testgettx{})
		if err == nil {
			t.Fatal("GetTaddressTransactions should have failed on bad address, case", i)
		}
		if !strings.Contains(err.Error(), "invalid characters") {
			t.Fatal("GetTaddressTransactions incorrect error on bad address, case", i)
		}
	}

	// valid address
	addressBlockFilter.Address = "t1234567890123456789012345678901234"
	err := lwd.GetTaddressTransactions(addressBlockFilter, &testgettx{})
	if err != nil {
		t.Fatal("GetTaddressTransactions failed", err)
	}

	// this time GetTransaction() will return an error
	err = lwd.GetTaddressTransactions(addressBlockFilter, &testgettx{})
	if err == nil {
		t.Fatal("GetTaddressTransactions succeeded")
	}
	step = 0
}

func TestGetTaddressTransactionsNilArgs(t *testing.T) {
	lwd, _ := testsetup()

	{
		noRange := &walletrpc.TransparentAddressBlockFilter{
			Range: nil,
		}
		err := lwd.GetTaddressTransactions(noRange, &testgettx{})
		if err == nil {
			t.Fatal("GetBlockRange nil range argument should fail")
		}
	}
	{
		noStart := &walletrpc.TransparentAddressBlockFilter{
			Range: &walletrpc.BlockRange{
				Start: nil,
				End:   &walletrpc.BlockID{Height: 20},
			},
		}
		err := lwd.GetTaddressTransactions(noStart, &testgettx{})
		if err == nil {
			t.Fatal("GetBlockRange nil range argument should fail")
		}
	}
	{
		noEnd := &walletrpc.TransparentAddressBlockFilter{
			Range: &walletrpc.BlockRange{
				Start: &walletrpc.BlockID{Height: 30},
				End:   nil,
			},
		}
		err := lwd.GetTaddressTransactions(noEnd, &testgettx{})
		if err == nil {
			t.Fatal("GetBlockRange nil range argument should fail")
		}
	}
}

func getblockStub(method string, params []json.RawMessage) (json.RawMessage, error) {
	if method != "getblock" {
		testT.Fatal("unexpected method:", method)
	}
	step++
	var arg string
	err := json.Unmarshal(params[0], &arg)
	if err != nil {
		testT.Fatal("could not unmarshal height")
	}

	// Test retry logic (for the moment, it's very simple, just one retry).
	switch step {
	case 1:
		if arg != "380640" {
			testT.Fatal("unexpected getblock height", arg)
		}
		// verbose mode (getblock height 1), return transaction list
		return []byte("{\"Tx\": [\"" + testTxid + "\"], \"Hash\": \"" + testBlockid + "\"}"), nil
	case 2:
		if arg != testBlockid {
			testT.Fatal("unexpected getblock height", arg)
		}
		return blocks[0], nil
	case 3:
		return nil, errors.New("getblock test error, too many requests")
	}
	testT.Fatal("unexpected call to getblockStub")
	return nil, nil
}

func TestGetBlock(t *testing.T) {
	testT = t
	common.RawRequest = getblockStub
	lwd, _ := testsetup()

	_, err := lwd.GetBlock(context.Background(), &walletrpc.BlockID{})
	if err == nil {
		t.Fatal("GetBlock should have failed")
	}
	_, err = lwd.GetBlock(context.Background(), &walletrpc.BlockID{Height: 0})
	if err == nil {
		t.Fatal("GetBlock should have failed")
	}
	_, err = lwd.GetBlock(context.Background(), &walletrpc.BlockID{Hash: []byte{0}})
	if err == nil {
		t.Fatal("GetBlock should have failed")
	}
	if !strings.Contains(err.Error(), "GetBlock: Block hash specifier is not yet implemented") {
		t.Fatal("GetBlock hash unimplemented error message failed")
	}

	// getblockStub() case 1: return error
	block, err := lwd.GetBlock(context.Background(), &walletrpc.BlockID{Height: 380640})
	if err != nil {
		t.Fatal("GetBlock failed:", err)
	}
	if block.Height != 380640 {
		t.Fatal("GetBlock returned unexpected block:", err)
	}
	// getblockStub() case 2: return error
	block, err = lwd.GetBlock(context.Background(), &walletrpc.BlockID{Height: 380640})
	if err == nil {
		t.Fatal("GetBlock should have failed")
	}
	if block != nil {
		t.Fatal("GetBlock returned unexpected non-nil block")
	}
	step = 0
}

type testgetbrange struct {
	walletrpc.CompactTxStreamer_GetBlockRangeServer
}

func (tg *testgetbrange) Context() context.Context {
	return context.Background()
}

func (tg *testgetbrange) Send(cb *walletrpc.CompactBlock) error {
	return nil
}

func TestGetBlockRange(t *testing.T) {
	testT = t
	common.RawRequest = getblockStub
	common.RawRequest = getblockStub
	lwd, _ := testsetup()

	blockrange := &walletrpc.BlockRange{
		Start: &walletrpc.BlockID{Height: 380640},
		End:   &walletrpc.BlockID{Height: 380640},
	}
	// getblockStub() case 1 (success)
	err := lwd.GetBlockRange(blockrange, &testgetbrange{})
	if err != nil {
		t.Fatal("GetBlockRange failed", err)
	}
	// getblockStub() case 2 (failure)
	err = lwd.GetBlockRange(blockrange, &testgetbrange{})
	if err == nil {
		t.Fatal("GetBlockRange should have failed")
	}
	step = 0
}

func TestGetBlockRangeNilArgs(t *testing.T) {
	lwd, _ := testsetup()

	{
		noEnd := &walletrpc.BlockRange{
			Start: &walletrpc.BlockID{Height: 380640},
			End:   nil,
		}
		err := lwd.GetBlockRange(noEnd, &testgetbrange{})
		if err == nil {
			t.Fatal("GetBlockRange nil argument should fail")
		}
	}
	{
		noStart := &walletrpc.BlockRange{
			Start: nil,
			End:   &walletrpc.BlockID{Height: 380640},
		}
		err := lwd.GetBlockRange(noStart, &testgetbrange{})
		if err == nil {
			t.Fatal("GetBlockRange nil argument should fail")
		}
	}
}

func sendrawtransactionStub(method string, params []json.RawMessage) (json.RawMessage, error) {
	step++
	if method != "sendrawtransaction" {
		testT.Fatal("unexpected method")
	}
	if string(params[0]) != "\"07\"" {
		testT.Fatal("unexpected tx data")
	}
	switch step {
	case 1:
		return []byte("sendtxresult"), nil
	case 2:
		return nil, errors.New("-17: some error")
	}
	testT.Fatal("unexpected call to sendrawtransactionStub")
	return nil, nil
}

func TestSendTransaction(t *testing.T) {
	testT = t
	lwd, _ := testsetup()
	common.RawRequest = sendrawtransactionStub
	rawtx := walletrpc.RawTransaction{Data: []byte{7}}
	sendresult, err := lwd.SendTransaction(context.Background(), &rawtx)
	if err != nil {
		t.Fatal("SendTransaction failed", err)
	}
	if sendresult.ErrorCode != 0 {
		t.Fatal("SendTransaction unexpected ErrorCode return")
	}
	if sendresult.ErrorMessage != "sendtxresult" {
		t.Fatal("SendTransaction unexpected ErrorMessage return")
	}

	// sendrawtransactionStub case 2 (error)
	// but note that the error is send within the response
	sendresult, err = lwd.SendTransaction(context.Background(), &rawtx)
	if err != nil {
		t.Fatal("SendTransaction failed:", err)
	}
	if sendresult.ErrorCode != -17 {
		t.Fatal("SendTransaction unexpected ErrorCode return")
	}
	if sendresult.ErrorMessage != "some error" {
		t.Fatal("SendTransaction unexpected ErrorMessage return")
	}
	step = 0
}

func TestMempoolFilter(t *testing.T) {
	txidlist := []string{
		"2e819d0bab5c819dc7d5f92d1bfb4127ce321daf847f6602",
		"29e594c312eee49bc2c9ad37367ba58f857c4a7387ec9715",
		"d4d090e60bf9141c6573f0598b84cc1f9817543e55a4d84d",
		"d4714779c6dd32a72077bd79d4a70cb2153b552d7addec15",
		"9839c1d4deca000656caff57c1f720f4fbd114b52239edde",
		"ce5a28854a509ab309faa433542e73414fef6e903a3d52f5",
	}
	exclude := []string{
		"98aa", // common prefix (98) but no match
		"19",   // no match
		"29",   // one match (should not appear)
		"d4",   // 2 matches (both should appear in result)
		"ce5a28854a509ab309faa433542e73414fef6e903a3d52f5",   // exact match
		"ce5a28854a509ab309faa433542e73414fef6e903a3d52f500", // extra stuff ignored
	}
	expected := []string{
		"2e819d0bab5c819dc7d5f92d1bfb4127ce321daf847f6602",
		"9839c1d4deca000656caff57c1f720f4fbd114b52239edde",
		"d4714779c6dd32a72077bd79d4a70cb2153b552d7addec15",
		"d4d090e60bf9141c6573f0598b84cc1f9817543e55a4d84d",
	}
	actual := MempoolFilter(txidlist, exclude)
	if len(actual) != len(expected) {
		t.Fatal("mempool: wrong number of filter results")
	}
	for i := 0; i < len(actual); i++ {
		if actual[i] != expected[i] {
			t.Fatalf("mempool: expected: %s actual: %s",
				expected[i], actual[i])
		}
	}
	// If the exclude list is empty, return the entire mempool.
	actual = MempoolFilter(txidlist, []string{})
	expected = []string{
		"29e594c312eee49bc2c9ad37367ba58f857c4a7387ec9715",
		"2e819d0bab5c819dc7d5f92d1bfb4127ce321daf847f6602",
		"9839c1d4deca000656caff57c1f720f4fbd114b52239edde",
		"ce5a28854a509ab309faa433542e73414fef6e903a3d52f5",
		"d4714779c6dd32a72077bd79d4a70cb2153b552d7addec15",
		"d4d090e60bf9141c6573f0598b84cc1f9817543e55a4d84d",
	}
	if len(actual) != len(expected) {
		t.Fatal("mempool: wrong number of filter results")
	}
	for i := 0; i < len(actual); i++ {
		if actual[i] != expected[i] {
			t.Fatalf("mempool: expected: %s actual: %s",
				expected[i], actual[i])
		}
	}

}
