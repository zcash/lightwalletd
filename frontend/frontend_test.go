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
	"io"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/zcash/lightwalletd/common"
	"github.com/zcash/lightwalletd/walletrpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
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

// resetGlobals restores the package globals that tests replace -- the zcashd
// RPC stub and the step counter those stubs sequence through -- to the values
// they have at package initialization. Any test that installs a stub should
// "defer resetGlobals()" so it doesn't leak into later tests, even if the test
// exits early via t.Fatal. Restoring common.RawRequest to nil (rather than to
// a working implementation) means that a test that forgets to install a stub
// panics rather than silently running against a leftover one.
func resetGlobals() {
	common.RawRequest = nil
	step = 0
	// The GetMempoolTx cache is package state too; a test that refreshes it
	// would otherwise leave the next test looking at a stale mempool.
	mempoolMap = nil
	mempoolList = nil
	lastMempool = time.Time{}
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

func getLatestBlockStub(ctx context.Context, method string, params []json.RawMessage) (json.RawMessage, error) {
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
	defer resetGlobals()
	lwd, cache := testsetup()

	// This argument is not used (it may be in the future)
	req := &walletrpc.ChainSpec{}

	// This does zcashd rpc "getblock", calls getLatestBlockStub() above
	block, err := common.GetBlock(context.Background(), cache, 380640)
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

func zcashdrpcStub(ctx context.Context, method string, params []json.RawMessage) (json.RawMessage, error) {
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

// testtaddrbalance is a mock client-streaming server for
// GetTaddressBalanceStream. It feeds the handler a fixed list of addresses,
// then EOF, and records the balance returned via SendAndClose.
type testtaddrbalance struct {
	walletrpc.CompactTxStreamer_GetTaddressBalanceStreamServer
	addrs   []string
	idx     int
	balance *walletrpc.Balance
}

func (t *testtaddrbalance) Context() context.Context {
	return context.Background()
}

func (t *testtaddrbalance) Recv() (*walletrpc.Address, error) {
	if t.idx >= len(t.addrs) {
		return nil, io.EOF
	}
	a := &walletrpc.Address{Address: t.addrs[t.idx]}
	t.idx++
	return a, nil
}

func (t *testtaddrbalance) SendAndClose(b *walletrpc.Balance) error {
	t.balance = b
	return nil
}

// getaddressbalanceStub returns a fixed balance and asserts that the request
// only reaches zcashd with the expected (valid, bounded) address list.
func getaddressbalanceStub(ctx context.Context, method string, params []json.RawMessage) (json.RawMessage, error) {
	if method != "getaddressbalance" {
		testT.Fatal("unexpected method", method)
	}
	var req common.ZcashdRpcRequestGetaddressbalance
	if err := json.Unmarshal(params[0], &req); err != nil {
		testT.Fatal("could not unmarshal getaddressbalance request")
	}
	return json.Marshal(common.ZcashdRpcReplyGetaddressbalance{Balance: 1234})
}

func TestGetTaddressBalanceStream(t *testing.T) {
	testT = t
	defer resetGlobals()
	lwd, _ := testsetup()

	validAddr := "t1234567890123456789012345678901234"

	// An invalid address must be rejected immediately, before any zcashd
	// call, and before the whole (potentially unbounded) stream is buffered.
	common.RawRequest = func(ctx context.Context, method string, params []json.RawMessage) (json.RawMessage, error) {
		testT.Fatal("zcashd must not be called for an invalid address")
		return nil, nil
	}
	{
		mock := &testtaddrbalance{addrs: []string{validAddr, "not-a-valid-address"}}
		err := lwd.GetTaddressBalanceStream(mock)
		if err == nil {
			t.Fatal("GetTaddressBalanceStream should have failed on bad address")
		}
		if status.Code(err) != codes.InvalidArgument {
			t.Fatal("expected InvalidArgument on bad address, got:", err)
		}
	}

	// Too many addresses must be rejected (GHSA-x4m7-3gpp-xc36), before the
	// server accumulates or forwards them.
	{
		addrs := make([]string, maxTaddrsPerRequest+1)
		for i := range addrs {
			addrs[i] = validAddr
		}
		mock := &testtaddrbalance{addrs: addrs}
		err := lwd.GetTaddressBalanceStream(mock)
		if err == nil {
			t.Fatal("GetTaddressBalanceStream should have failed on too many addresses")
		}
		if status.Code(err) != codes.ResourceExhausted {
			t.Fatal("expected ResourceExhausted on too many addresses, got:", err)
		}
	}

	// A valid, bounded request succeeds and returns the balance from zcashd.
	common.RawRequest = getaddressbalanceStub
	{
		mock := &testtaddrbalance{addrs: []string{validAddr, validAddr}}
		err := lwd.GetTaddressBalanceStream(mock)
		if err != nil {
			t.Fatal("GetTaddressBalanceStream failed:", err)
		}
		if mock.balance == nil || mock.balance.ValueZat != 1234 {
			t.Fatal("unexpected balance:", mock.balance)
		}
	}
}

func TestGetTaddressBalanceTooManyAddresses(t *testing.T) {
	testT = t
	defer resetGlobals()
	lwd, _ := testsetup()

	// The unary method takes the whole list in one protobuf message, so its
	// only bound was gRPC's 4MB MaxRecvMsgSize -- about 113,000 addresses at
	// 37 wire bytes each, an order of magnitude above the cap the streaming
	// and utxo methods enforce (GHSA-x4m7-3gpp-xc36).
	//
	// Distinct addresses, so that the counts here are unaffected by any later
	// deduplication of the list.
	addrs := make([]string, maxTaddrsPerRequest+1)
	for i := range addrs {
		addrs[i] = fmt.Sprintf("t1%033d", i)
	}

	// Exactly at the limit: accepted, and the whole list reaches zcashd.
	common.RawRequest = func(ctx context.Context, method string, params []json.RawMessage) (json.RawMessage, error) {
		if method != "getaddressbalance" {
			testT.Fatal("unexpected method", method)
		}
		var req common.ZcashdRpcRequestGetaddressbalance
		if err := json.Unmarshal(params[0], &req); err != nil {
			testT.Fatal("could not unmarshal getaddressbalance request")
		}
		if len(req.Addresses) != maxTaddrsPerRequest {
			testT.Fatal("expected the whole list to reach zcashd, got:", len(req.Addresses))
		}
		return json.Marshal(common.ZcashdRpcReplyGetaddressbalance{Balance: 1234})
	}
	balance, err := lwd.GetTaddressBalance(context.Background(),
		&walletrpc.AddressList{Addresses: addrs[:maxTaddrsPerRequest]})
	if err != nil {
		t.Fatal("GetTaddressBalance failed at exactly the limit:", err)
	}
	if balance.ValueZat != 1234 {
		t.Fatal("unexpected balance:", balance.ValueZat)
	}

	// One over: rejected before zcashd is contacted.
	common.RawRequest = func(ctx context.Context, method string, params []json.RawMessage) (json.RawMessage, error) {
		testT.Fatal("zcashd must not be called when the address list is over the limit")
		return nil, nil
	}
	_, err = lwd.GetTaddressBalance(context.Background(), &walletrpc.AddressList{Addresses: addrs})
	if err == nil {
		t.Fatal("GetTaddressBalance should have failed on too many addresses")
	}
	if status.Code(err) != codes.ResourceExhausted {
		t.Fatal("expected ResourceExhausted on too many addresses, got:", err)
	}
}

func TestGetAddressUtxosTooManyAddresses(t *testing.T) {
	testT = t
	defer resetGlobals()
	lwd, _ := testsetup()

	// A request naming too many addresses must be rejected before zcashd is
	// contacted, so one request can't force unbounded backend work
	// (GHSA-x4m7-3gpp-xc36).
	common.RawRequest = func(ctx context.Context, method string, params []json.RawMessage) (json.RawMessage, error) {
		testT.Fatal("zcashd must not be called when the address list is over the limit")
		return nil, nil
	}
	addrs := make([]string, maxTaddrsPerRequest+1)
	for i := range addrs {
		addrs[i] = "t1234567890123456789012345678901234"
	}
	_, err := lwd.GetAddressUtxos(context.Background(), &walletrpc.GetAddressUtxosArg{Addresses: addrs})
	if err == nil {
		t.Fatal("GetAddressUtxos should have failed on too many addresses")
	}
	if status.Code(err) != codes.ResourceExhausted {
		t.Fatal("expected ResourceExhausted on too many addresses, got:", err)
	}
}

func TestGetTaddressBalanceDeduplicates(t *testing.T) {
	testT = t
	defer resetGlobals()
	lwd, _ := testsetup()

	taddr := func(c string) string { return "t1" + strings.Repeat(c, 33) }
	addrA, addrB := taddr("a"), taddr("b")

	// zcashd sums getaddressbalance over the list entries, so a repeated
	// address inflates the balance it reports; zebrad collapses duplicates
	// before querying. Model zcashd here: 1000 zats per entry received.
	var forwarded []string
	common.RawRequest = func(ctx context.Context, method string, params []json.RawMessage) (json.RawMessage, error) {
		if method != "getaddressbalance" {
			testT.Fatal("unexpected method", method)
		}
		var req common.ZcashdRpcRequestGetaddressbalance
		if err := json.Unmarshal(params[0], &req); err != nil {
			testT.Fatal("could not unmarshal getaddressbalance request")
		}
		forwarded = req.Addresses
		return json.Marshal(common.ZcashdRpcReplyGetaddressbalance{
			Balance: int64(1000 * len(req.Addresses)),
		})
	}

	balance, err := lwd.GetTaddressBalance(context.Background(),
		&walletrpc.AddressList{Addresses: []string{addrA, addrB, addrA, addrA}})
	if err != nil {
		t.Fatal("GetTaddressBalance failed:", err)
	}
	if !reflect.DeepEqual(forwarded, []string{addrA, addrB}) {
		t.Fatal("expected the distinct addresses in first-occurrence order, got:", forwarded)
	}
	// Two distinct addresses, so two entries' worth -- not four.
	if balance.ValueZat != 2000 {
		t.Fatal("expected balance 2000, got:", balance.ValueZat)
	}
}

func TestGetTaddressBalanceEmptyList(t *testing.T) {
	testT = t
	defer resetGlobals()
	lwd, _ := testsetup()

	// An empty address list is not an error; the balance of nothing is zero.
	common.RawRequest = func(ctx context.Context, method string, params []json.RawMessage) (json.RawMessage, error) {
		if method != "getaddressbalance" {
			testT.Fatal("unexpected method", method)
		}
		return json.Marshal(common.ZcashdRpcReplyGetaddressbalance{Balance: 0})
	}

	balance, err := lwd.GetTaddressBalance(context.Background(), &walletrpc.AddressList{})
	if err != nil {
		t.Fatal("GetTaddressBalance failed on an empty address list:", err)
	}
	if balance.ValueZat != 0 {
		t.Fatal("expected balance 0, got:", balance.ValueZat)
	}
}

func TestGetAddressUtxosEmptyList(t *testing.T) {
	testT = t
	defer resetGlobals()
	lwd, _ := testsetup()

	// An empty address list is not an error; it selects no utxos.
	common.RawRequest = func(ctx context.Context, method string, params []json.RawMessage) (json.RawMessage, error) {
		if method != "getaddressutxos" {
			testT.Fatal("unexpected method", method)
		}
		return json.Marshal([]common.ZcashdRpcReplyGetaddressutxos{})
	}

	reply, err := lwd.GetAddressUtxos(context.Background(), &walletrpc.GetAddressUtxosArg{})
	if err != nil {
		t.Fatal("GetAddressUtxos failed on an empty address list:", err)
	}
	if len(reply.AddressUtxos) != 0 {
		t.Fatal("expected no utxos, got:", len(reply.AddressUtxos))
	}
}

func TestGetAddressUtxosDeduplicates(t *testing.T) {
	testT = t
	defer resetGlobals()
	lwd, _ := testsetup()

	// A distinct valid taddr per letter: "t1" followed by 33 more characters.
	taddr := func(c string) string { return "t1" + strings.Repeat(c, 33) }
	addrA, addrB, addrC := taddr("a"), taddr("b"), taddr("c")
	addrD, addrE, addrF := taddr("d"), taddr("e"), taddr("f")

	// zcashd looks up each entry of the address list independently, so a
	// repeated address multiplies its backend cost and its UTXOs in the reply.
	// Repeating an address costs the caller nothing, so lightwalletd must send
	// each distinct address once, in first-occurrence order. Six addresses so
	// that an implementation collecting them out of order (from a map, say)
	// reliably fails this rather than matching by chance.
	var forwarded []string
	common.RawRequest = func(ctx context.Context, method string, params []json.RawMessage) (json.RawMessage, error) {
		if method != "getaddressutxos" {
			testT.Fatal("unexpected method", method)
		}
		var req common.ZcashdRpcRequestGetaddressutxos
		if err := json.Unmarshal(params[0], &req); err != nil {
			testT.Fatal("could not unmarshal getaddressutxos request")
		}
		forwarded = req.Addresses
		return json.Marshal([]common.ZcashdRpcReplyGetaddressutxos{{
			Address:     addrA,
			Txid:        "0788e4dc9973cd9a54e0f4d51ec96f4b8e6a8e0f8a1e1e9e4b2c2a1d0e0f0a0b",
			OutputIndex: 0,
			Script:      "76a914000000000000000000000000000000000000000088ac",
			Satoshis:    1000,
			Height:      1234,
		}})
	}

	// Duplicates interleaved with the distinct addresses, and one address
	// (addrA) repeated far more often than the rest.
	reply, err := lwd.GetAddressUtxos(context.Background(), &walletrpc.GetAddressUtxosArg{
		Addresses: []string{
			addrA, addrB, addrA, addrC, addrB, addrA,
			addrD, addrA, addrE, addrD, addrF, addrA,
		},
	})
	if err != nil {
		t.Fatal("GetAddressUtxos failed:", err)
	}
	want := []string{addrA, addrB, addrC, addrD, addrE, addrF}
	if !reflect.DeepEqual(forwarded, want) {
		t.Fatal("expected the distinct addresses in first-occurrence order, got:", forwarded)
	}
	if len(reply.AddressUtxos) != 1 {
		t.Fatal("expected one utxo, got:", len(reply.AddressUtxos))
	}

	// An invalid address is still rejected, even if a valid one precedes it,
	// and even though the dedupe check runs first.
	common.RawRequest = func(ctx context.Context, method string, params []json.RawMessage) (json.RawMessage, error) {
		testT.Fatal("zcashd must not be called for an invalid address")
		return nil, nil
	}
	_, err = lwd.GetAddressUtxos(context.Background(), &walletrpc.GetAddressUtxosArg{
		Addresses: []string{addrA, addrA, "not-a-valid-address"},
	})
	if err == nil {
		t.Fatal("GetAddressUtxos should have failed on bad address")
	}
	if status.Code(err) != codes.InvalidArgument {
		t.Fatal("expected InvalidArgument on bad address, got:", err)
	}
}

type testgettx struct {
	walletrpc.CompactTxStreamer_GetTaddressTransactionsServer
}

func TestGetAddressUtxosPushesDownLimits(t *testing.T) {
	testT = t
	defer resetGlobals()
	lwd, _ := testsetup()

	taddr := "t1" + strings.Repeat("a", 33)
	utxo := func(height int) common.ZcashdRpcReplyGetaddressutxos {
		return common.ZcashdRpcReplyGetaddressutxos{
			Address:     taddr,
			Txid:        "0788e4dc9973cd9a54e0f4d51ec96f4b8e6a8e0f8a1e1e9e4b2c2a1d0e0f0a0b",
			OutputIndex: 0,
			Script:      "76a914000000000000000000000000000000000000000088ac",
			Satoshis:    1000,
			Height:      height,
		}
	}

	// Model a backend that doesn't implement the new arguments: it returns
	// every utxo, in chain order, whatever the request asked for.
	var sent json.RawMessage
	common.RawRequest = func(ctx context.Context, method string, params []json.RawMessage) (json.RawMessage, error) {
		if method != "getaddressutxos" {
			testT.Fatal("unexpected method", method)
		}
		sent = params[0]
		return json.Marshal([]common.ZcashdRpcReplyGetaddressutxos{
			utxo(100), utxo(200), utxo(300), utxo(400),
		})
	}

	reply, err := lwd.GetAddressUtxos(context.Background(), &walletrpc.GetAddressUtxosArg{
		Addresses:   []string{taddr},
		StartHeight: 200,
		MaxEntries:  2,
	})
	if err != nil {
		t.Fatal("GetAddressUtxos failed:", err)
	}
	var req common.ZcashdRpcRequestGetaddressutxos
	if err := json.Unmarshal(sent, &req); err != nil {
		t.Fatal("could not unmarshal getaddressutxos request")
	}
	if req.StartHeight != 200 || req.MaxEntries != 2 {
		t.Fatal("expected the limits to reach the backend, got:", string(sent))
	}
	// The backend ignored them, so the client-side filter must still hold.
	heights := make([]uint64, 0)
	for _, u := range reply.AddressUtxos {
		heights = append(heights, u.Height)
	}
	if !reflect.DeepEqual(heights, []uint64{200, 300}) {
		t.Fatal("expected utxos at heights 200 and 300, got:", heights)
	}

	// A request that sets neither limit is byte-for-byte what it was before
	// the arguments existed, so an unpatched backend sees no change.
	_, err = lwd.GetAddressUtxos(context.Background(), &walletrpc.GetAddressUtxosArg{
		Addresses: []string{taddr},
	})
	if err != nil {
		t.Fatal("GetAddressUtxos failed:", err)
	}
	if want := `{"addresses":["` + taddr + `"]}`; string(sent) != want {
		t.Fatal("expected unset limits to be omitted, got:", string(sent))
	}
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
	defer resetGlobals()
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

// A request that omits the range End must not become an open-ended index scan;
// the handler defaults End to the current chain tip (GHSA-x4m7-3gpp-xc36 F2).
func TestGetTaddressTransactionsDefaultsEndToTip(t *testing.T) {
	testT = t
	defer resetGlobals()
	lwd, _ := testsetup()

	const tip = 987654
	common.RawRequest = func(ctx context.Context, method string, params []json.RawMessage) (json.RawMessage, error) {
		switch method {
		case "getblockchaininfo":
			return json.Marshal(common.ZcashdRpcReplyGetblockchaininfo{Blocks: tip})
		case "getaddresstxids":
			var filter common.ZcashdRpcRequestGetaddresstxids
			if err := json.Unmarshal(params[0], &filter); err != nil {
				testT.Fatal("could not unmarshal getaddresstxids request")
			}
			if filter.Start != 100 {
				testT.Fatal("unexpected start", filter.Start)
			}
			if filter.End != tip {
				testT.Fatal("End should default to the chain tip, got", filter.End)
			}
			return []byte("[]"), nil // no txids -> no fan-out
		}
		testT.Fatal("unexpected method", method)
		return nil, nil
	}

	filter := &walletrpc.TransparentAddressBlockFilter{
		Address: "t1234567890123456789012345678901234",
		Range:   &walletrpc.BlockRange{Start: &walletrpc.BlockID{Height: 100}}, // End omitted
	}
	if err := lwd.GetTaddressTransactions(filter, &testgettx{}); err != nil {
		t.Fatal("GetTaddressTransactions failed:", err)
	}
}

// A range wider than maxTaddrTxBlockSpan must be rejected before zcashd is
// contacted (GHSA-x4m7-3gpp-xc36 F2).
func TestGetTaddressTransactionsRangeTooWide(t *testing.T) {
	testT = t
	defer resetGlobals()
	lwd, _ := testsetup()

	common.RawRequest = func(ctx context.Context, method string, params []json.RawMessage) (json.RawMessage, error) {
		testT.Fatal("zcashd must not be called when the range is too wide")
		return nil, nil
	}
	filter := &walletrpc.TransparentAddressBlockFilter{
		Address: "t1234567890123456789012345678901234",
		Range: &walletrpc.BlockRange{
			Start: &walletrpc.BlockID{Height: 0},
			End:   &walletrpc.BlockID{Height: maxTaddrTxBlockSpan + 1},
		},
	}
	err := lwd.GetTaddressTransactions(filter, &testgettx{})
	if err == nil {
		t.Fatal("GetTaddressTransactions should have failed on too-wide range")
	}
	if status.Code(err) != codes.InvalidArgument {
		t.Fatal("expected InvalidArgument on too-wide range, got:", err)
	}
}

func getblockStub(ctx context.Context, method string, params []json.RawMessage) (json.RawMessage, error) {
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
	defer resetGlobals()
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
	defer resetGlobals()
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

func sendrawtransactionStub(ctx context.Context, method string, params []json.RawMessage) (json.RawMessage, error) {
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
	defer resetGlobals()
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

func TestPruneCompactBlockToNullifiers(t *testing.T) {
	cb := &walletrpc.CompactBlock{
		Vtx: []*walletrpc.CompactTx{
			{
				Actions: []*walletrpc.CompactOrchardAction{
					{Nullifier: []byte{1}, Cmx: []byte{2}},
				},
				IronwoodActions: []*walletrpc.CompactOrchardAction{
					{Nullifier: []byte{3}, Cmx: []byte{4}},
				},
				Outputs: []*walletrpc.CompactSaplingOutput{{Cmu: []byte{5}}},
				Vin:     []*walletrpc.CompactTxIn{{PrevoutTxid: []byte{6}}},
				Vout:    []*walletrpc.TxOut{{Value: 7}},
			},
		},
		ChainMetadata: &walletrpc.ChainMetadata{
			SaplingCommitmentTreeSize:  10,
			OrchardCommitmentTreeSize:  20,
			IronwoodCommitmentTreeSize: 30,
		},
	}
	pruneCompactBlockToNullifiers(cb)
	tx := cb.Vtx[0]
	if len(tx.Actions) != 1 || len(tx.Actions[0].Cmx) != 0 || !bytes.Equal(tx.Actions[0].Nullifier, []byte{1}) {
		t.Fatalf("orchard action not pruned to nullifier only: %+v", tx.Actions[0])
	}
	if len(tx.IronwoodActions) != 1 || len(tx.IronwoodActions[0].Cmx) != 0 || !bytes.Equal(tx.IronwoodActions[0].Nullifier, []byte{3}) {
		t.Fatalf("ironwood action not pruned to nullifier only: %+v", tx.IronwoodActions[0])
	}
	if len(tx.Outputs) != 0 || len(tx.Vin) != 0 || len(tx.Vout) != 0 {
		t.Fatalf("non-nullifier components not nil: outputs=%d vin=%d vout=%d", len(tx.Outputs), len(tx.Vin), len(tx.Vout))
	}
	if cb.ChainMetadata.SaplingCommitmentTreeSize != 0 ||
		cb.ChainMetadata.OrchardCommitmentTreeSize != 0 ||
		cb.ChainMetadata.IronwoodCommitmentTreeSize != 0 {
		t.Fatalf("chain metadata tree sizes not zeroed: %+v", cb.ChainMetadata)
	}
}

// An explicit End of height 0 must not be treated as "no bound": `End` carries
// `json:",omitempty"`, so a zero value is dropped from the getaddresstxids
// request and zcashd falls back to an open-ended scan — the exact behaviour
// GHSA-x4m7-3gpp-xc36 F2 is meant to prevent.
func TestGetTaddressTransactionsZeroEndIsBounded(t *testing.T) {
	testT = t
	defer resetGlobals()
	lwd, _ := testsetup()

	const tip = 987654
	common.RawRequest = func(ctx context.Context, method string, params []json.RawMessage) (json.RawMessage, error) {
		switch method {
		case "getblockchaininfo":
			return json.Marshal(common.ZcashdRpcReplyGetblockchaininfo{Blocks: tip})
		case "getaddresstxids":
			var filter common.ZcashdRpcRequestGetaddresstxids
			if err := json.Unmarshal(params[0], &filter); err != nil {
				testT.Fatal("could not unmarshal getaddresstxids request")
			}
			if filter.End == 0 {
				testT.Fatal("End=0 reached the backend as an unbounded scan")
			}
			return []byte("[]"), nil
		}
		testT.Fatal("unexpected method", method)
		return nil, nil
	}

	filter := &walletrpc.TransparentAddressBlockFilter{
		Address: "t1234567890123456789012345678901234",
		Range: &walletrpc.BlockRange{
			Start: &walletrpc.BlockID{Height: 100},
			End:   &walletrpc.BlockID{Height: 0}, // non-nil, zero
		},
	}
	if err := lwd.GetTaddressTransactions(filter, &testgettx{}); err != nil {
		t.Fatal("GetTaddressTransactions failed:", err)
	}
}

// An invalid-length block hash must be rejected before it is hex-expanded and
// forwarded, so a client can't force large allocations here or parsing work in
// zcashd with input that can only ever be rejected (GHSA-q2c2-hpp9-58hm).
func TestGetTreeStateInvalidHashLength(t *testing.T) {
	testT = t
	defer resetGlobals()
	lwd, _ := testsetup()

	common.RawRequest = func(ctx context.Context, method string, params []json.RawMessage) (json.RawMessage, error) {
		testT.Fatal("zcashd must not be called for an invalid-length block hash")
		return nil, nil
	}
	for _, n := range []int{1, 31, 33, 64, 4 << 20} {
		_, err := lwd.GetTreeState(context.Background(),
			&walletrpc.BlockID{Hash: make([]byte, n)})
		if err == nil {
			t.Fatal("GetTreeState should have failed on hash length", n)
		}
		if status.Code(err) != codes.InvalidArgument {
			t.Fatal("expected InvalidArgument for hash length", n, "got:", err)
		}
	}

	// A correctly-sized hash must still reach zcashd -- the guard must not
	// reject valid input.
	forwarded := false
	common.RawRequest = func(ctx context.Context, method string, params []json.RawMessage) (json.RawMessage, error) {
		forwarded = true
		if method != "z_gettreestate" {
			testT.Fatal("unexpected method", method)
		}
		return nil, errors.New("-8: block not found")
	}
	_, err := lwd.GetTreeState(context.Background(),
		&walletrpc.BlockID{Hash: make([]byte, blockHashLen)})
	if err == nil {
		t.Fatal("expected the stubbed backend error")
	}
	if !forwarded {
		t.Fatal("a 32-byte hash should have been forwarded to zcashd")
	}
}

// An oversized raw transaction must be rejected before it is hex-expanded and
// forwarded (GHSA-6ppp-r2gc-9q6v). A transaction at exactly the limit is still
// accepted, so the bound is not off by one.
func TestSendTransactionOversized(t *testing.T) {
	testT = t
	defer resetGlobals()
	lwd, _ := testsetup()

	// Over the limit: rejected locally, zcashd never contacted.
	common.RawRequest = func(ctx context.Context, method string, params []json.RawMessage) (json.RawMessage, error) {
		testT.Fatal("zcashd must not be called for an oversized transaction")
		return nil, nil
	}
	_, err := lwd.SendTransaction(context.Background(),
		&walletrpc.RawTransaction{Data: make([]byte, maxRawTxSize+1)})
	if err == nil {
		t.Fatal("SendTransaction should have failed on an oversized transaction")
	}
	if status.Code(err) != codes.InvalidArgument {
		t.Fatal("expected InvalidArgument on oversized transaction, got:", err)
	}

	// Exactly at the limit: forwarded to zcashd.
	forwarded := false
	common.RawRequest = func(ctx context.Context, method string, params []json.RawMessage) (json.RawMessage, error) {
		forwarded = true
		return json.Marshal("sometxid")
	}
	if _, err := lwd.SendTransaction(context.Background(),
		&walletrpc.RawTransaction{Data: make([]byte, maxRawTxSize)}); err != nil {
		t.Fatal("SendTransaction at the size limit failed:", err)
	}
	if !forwarded {
		t.Fatal("a transaction at the size limit should have been forwarded")
	}
}

type testmempooltx struct {
	walletrpc.CompactTxStreamer_GetMempoolTxServer
	sent int
}

func (t *testmempooltx) Context() context.Context { return context.Background() }

func (t *testmempooltx) Send(tx *walletrpc.CompactTx) error {
	t.sent++
	return nil
}

// An oversized exclude list must be rejected before the server allocates,
// hex-encodes and sorts it -- work that used to happen while holding the
// method mutex, stalling other callers (GHSA-4hp3-3494-3f2m). A list at the
// cap is still accepted, so the bound is not off by one.
func TestGetMempoolTxExcludeListCap(t *testing.T) {
	testT = t
	defer resetGlobals()
	lwd, _ := testsetup()

	suffixes := make([][]byte, maxExcludeTxidSuffixes+1)
	for i := range suffixes {
		suffixes[i] = []byte{1, 2, 3, 4}
	}

	// Over the cap: rejected locally, zcashd never contacted.
	common.RawRequest = func(ctx context.Context, method string, params []json.RawMessage) (json.RawMessage, error) {
		testT.Fatal("zcashd must not be called for an oversized exclude list")
		return nil, nil
	}
	err := lwd.GetMempoolTx(
		&walletrpc.GetMempoolTxRequest{ExcludeTxidSuffixes: suffixes}, &testmempooltx{})
	if err == nil {
		t.Fatal("GetMempoolTx should have failed on an oversized exclude list")
	}
	if status.Code(err) != codes.InvalidArgument {
		t.Fatal("expected InvalidArgument on oversized exclude list, got:", err)
	}

	// Exactly at the cap: accepted, and the mempool refresh happens.
	refreshed := false
	common.RawRequest = func(ctx context.Context, method string, params []json.RawMessage) (json.RawMessage, error) {
		if method != "getrawmempool" {
			testT.Fatal("unexpected method", method)
		}
		refreshed = true
		return []byte("[]"), nil
	}
	if err := lwd.GetMempoolTx(
		&walletrpc.GetMempoolTxRequest{ExcludeTxidSuffixes: suffixes[:maxExcludeTxidSuffixes]},
		&testmempooltx{}); err != nil {
		t.Fatal("GetMempoolTx at the exclude-list cap failed:", err)
	}
	if !refreshed {
		t.Fatal("a request at the cap should have reached the backend")
	}
}

// testmempoolslow is a GetMempoolTx stream whose Send blocks until released,
// standing in for a client that has stopped reading its stream.
type testmempoolslow struct {
	walletrpc.CompactTxStreamer_GetMempoolTxServer
	sending chan struct{}
	release chan struct{}
	once    bool
}

func (t *testmempoolslow) Context() context.Context { return context.Background() }

func (t *testmempoolslow) Send(tx *walletrpc.CompactTx) error {
	if !t.once {
		t.once = true
		close(t.sending)
		<-t.release
	}
	return nil
}

// A client that stops reading its stream must not stall other GetMempoolTx
// callers. resp.Send blocks on the client's flow control, so holding s.mutex
// across it serialized every caller behind the slowest one
// (GHSA-9p9r-mggr-8q9g). The same applies to the refresh RPCs
// (GHSA-f9pw-q493-7qvh) -- neither may run inside the critical section.
func TestGetMempoolTxSlowClientDoesNotBlockOthers(t *testing.T) {
	testT = t
	defer resetGlobals()
	lwd, _ := testsetup()

	// Seed a fresh cache holding one Orchard tx, so neither caller triggers a
	// refresh and both reach the send loop.
	txid := strings.Repeat("ab", 32)
	tx := &walletrpc.CompactTx{
		Actions: []*walletrpc.CompactOrchardAction{{Nullifier: make([]byte, 32)}},
	}
	m := map[string]*walletrpc.CompactTx{txid: tx}
	mempoolList = []string{txid}
	mempoolMap = &m
	lastMempool = time.Now()

	common.RawRequest = func(ctx context.Context, method string, params []json.RawMessage) (json.RawMessage, error) {
		testT.Fatal("no mempool refresh expected; the cache was seeded fresh")
		return nil, nil
	}

	req := &walletrpc.GetMempoolTxRequest{}

	// Caller A parks inside Send, as a stalled client would.
	slow := &testmempoolslow{sending: make(chan struct{}), release: make(chan struct{})}
	slowDone := make(chan error, 1)
	go func() { slowDone <- lwd.GetMempoolTx(req, slow) }()
	select {
	case <-slow.sending:
	case <-time.After(10 * time.Second):
		t.Fatal("the slow caller never reached Send")
	}

	// Caller B must finish while A is still parked. If the mutex were still
	// held across Send, this would block until the test timed out.
	fastDone := make(chan error, 1)
	go func() { fastDone <- lwd.GetMempoolTx(req, &testmempooltx{}) }()
	select {
	case err := <-fastDone:
		if err != nil {
			t.Fatal("second caller failed:", err)
		}
	case <-time.After(10 * time.Second):
		close(slow.release)
		t.Fatal("second caller blocked behind the stalled client -- the mutex is still held across resp.Send")
	}

	close(slow.release)
	if err := <-slowDone; err != nil {
		t.Fatal("slow caller failed:", err)
	}
}

// testmempoolcancel is a GetMempoolTx stream whose context is cancelled after
// the refresh has started, standing in for a client that hangs up mid-refresh.
type testmempoolcancel struct {
	walletrpc.CompactTxStreamer_GetMempoolTxServer
	ctx context.Context
}

func (t *testmempoolcancel) Context() context.Context        { return t.ctx }
func (t *testmempoolcancel) Send(*walletrpc.CompactTx) error { return nil }

// If the client that triggered a refresh disconnects, the refresh must abort
// rather than run to completion. RawRequest observes the context, so every
// remaining per-txid fetch would fail and be swallowed by the "mempool
// transactions can disappear" path, publishing a cache missing nearly every
// transaction and degrading it for other callers (GHSA-f9pw-q493-7qvh).
func TestGetMempoolTxAbortsRefreshOnClientCancel(t *testing.T) {
	testT = t
	defer resetGlobals()
	lwd, _ := testsetup()

	// A mempool of 50 txids, none of them already cached, so the refresh has
	// plenty of per-txid fetches left to do after the cancel.
	var txids []string
	for i := 0; i < 50; i++ {
		txids = append(txids, fmt.Sprintf("%064x", i))
	}
	listJSON, _ := json.Marshal(txids)

	ctx, cancel := context.WithCancel(context.Background())
	fetches := 0
	common.RawRequest = func(_ context.Context, method string, params []json.RawMessage) (json.RawMessage, error) {
		switch method {
		case "getrawmempool":
			return listJSON, nil
		case "getrawtransaction":
			fetches++
			cancel() // the client hangs up after the first fetch
			return nil, errors.New("context canceled")
		}
		testT.Fatal("unexpected method", method)
		return nil, nil
	}

	err := lwd.GetMempoolTx(&walletrpc.GetMempoolTxRequest{}, &testmempoolcancel{ctx: ctx})
	if err == nil {
		t.Fatal("expected a context error once the client cancelled")
	}
	if status.Code(err) != codes.Canceled {
		t.Fatal("expected Canceled, got:", status.Code(err), err)
	}
	// The refresh must have stopped early rather than grinding through all 50.
	if fetches > 2 {
		t.Fatalf("refresh kept fetching after cancel: %d fetches for 50 txids", fetches)
	}
	// And it must not have published a degraded cache.
	if mempoolMap != nil && len(*mempoolMap) > 0 {
		t.Fatalf("a partial cache was published: %d entries", len(*mempoolMap))
	}
}

type testgetsubtreeroots struct {
	walletrpc.CompactTxStreamer_GetSubtreeRootsServer
}

func (tg *testgetsubtreeroots) Context() context.Context {
	return context.Background()
}

func (tg *testgetsubtreeroots) Send(sr *walletrpc.SubtreeRoot) error {
	return nil
}

// An unrecognized ShieldedProtocol must arrive as InvalidArgument, not Unknown.
func TestGetSubtreeRootsUnknownProtocolIsInvalidArgument(t *testing.T) {
	testT = t
	defer resetGlobals()
	lwd, _ := testsetup()

	common.RawRequest = func(ctx context.Context, method string, params []json.RawMessage) (json.RawMessage, error) {
		testT.Fatal("the backend must not be called for an unrecognized shielded protocol")
		return nil, nil
	}
	err := lwd.GetSubtreeRoots(&walletrpc.GetSubtreeRootsArg{
		ShieldedProtocol: walletrpc.ShieldedProtocol(9999),
	}, &testgetsubtreeroots{})
	if err == nil {
		t.Fatal("GetSubtreeRoots should have failed on an unrecognized shielded protocol")
	}
	if status.Code(err) != codes.InvalidArgument {
		t.Fatal("expected InvalidArgument for an unrecognized shielded protocol, got:",
			status.Code(err), err)
	}

	// The recognized protocols still reach the backend, named as the RPC expects.
	for _, protocol := range []walletrpc.ShieldedProtocol{
		walletrpc.ShieldedProtocol_sapling,
		walletrpc.ShieldedProtocol_orchard,
		walletrpc.ShieldedProtocol_ironwood,
	} {
		called := false
		common.RawRequest = func(ctx context.Context, method string, params []json.RawMessage) (json.RawMessage, error) {
			called = true
			if method != "z_getsubtreesbyindex" {
				testT.Fatal("unexpected method", method)
			}
			var name string
			if err := json.Unmarshal(params[0], &name); err != nil {
				testT.Fatal("could not unmarshal the protocol parameter")
			}
			if name != protocol.String() {
				testT.Fatal("expected protocol", protocol.String(), "got", name)
			}
			return json.Marshal(common.ZcashdRpcReplyGetsubtreebyindex{})
		}
		if err := lwd.GetSubtreeRoots(&walletrpc.GetSubtreeRootsArg{
			ShieldedProtocol: protocol,
		}, &testgetsubtreeroots{}); err != nil {
			t.Fatal("GetSubtreeRoots failed for", protocol.String(), err)
		}
		if !called {
			t.Fatal("the backend was not called for", protocol.String())
		}
	}
}
