// Copyright (c) 2019-2020 The Zcash developers
// Distributed under the MIT software license, see the accompanying
// file COPYING or https://www.opensource.org/licenses/mit-license.php .

package common

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/zcash/lightwalletd/parser"
	"github.com/zcash/lightwalletd/walletrpc"
)

// 'make build' will overwrite this string with the output of git-describe (tag)
var (
	Version   = "v0.0.0.0-dev"
	GitCommit = ""
	Branch    = ""
	BuildDate = ""
	BuildUser = ""
)

type Options struct {
	GRPCBindAddr        string `json:"grpc_bind_address,omitempty"`
	GRPCLogging         bool   `json:"grpc_logging_insecure,omitempty"`
	HTTPBindAddr        string `json:"http_bind_address,omitempty"`
	TLSCertPath         string `json:"tls_cert_path,omitempty"`
	TLSKeyPath          string `json:"tls_cert_key,omitempty"`
	LogLevel            uint64 `json:"log_level,omitempty"`
	LogFile             string `json:"log_file,omitempty"`
	ZcashConfPath       string `json:"zcash_conf,omitempty"`
	RPCUser             string `json:"rpcuser"`
	RPCPassword         string `json:"rpcpassword"`
	RPCHost             string `json:"rpchost"`
	RPCPort             string `json:"rpcport"`
	NoTLSVeryInsecure   bool   `json:"no_tls_very_insecure,omitempty"`
	GenCertVeryInsecure bool   `json:"gen_cert_very_insecure,omitempty"`
	Redownload          bool   `json:"redownload"`
	NoCache             bool   `json:"nocache"`
	SyncFromHeight      int    `json:"sync_from_height"`
	DataDir             string `json:"data_dir"`
	PingEnable          bool   `json:"ping_enable"`
	Darkside            bool   `json:"darkside"`
	DarksideTimeout     uint64 `json:"darkside_timeout"`
}

// RawRequest points to the function to send a an RPC request to zcashd;
// in production, it points to btcsuite/btcd/rpcclient/rawrequest.go:RawRequest();
// in unit tests it points to a function to mock RPCs to zcashd.
var RawRequest func(method string, params []json.RawMessage) (json.RawMessage, error)

// Time allows time-related functions to be mocked for testing,
// so that tests can be deterministic and so they don't require
// real time to elapse. In production, these point to the standard
// library `time` functions; in unit tests they point to mock
// functions (set by the specific test as required).
// More functions can be added later.
var Time struct {
	Sleep func(d time.Duration)
	Now   func() time.Time
}

// Log as a global variable simplifies logging
var Log *logrus.Entry

// The following are JSON zcashd rpc requests and replies.
type (
	// zcashd rpc "getblockchaininfo"
	Upgradeinfo struct {
		// unneeded fields can be omitted
		ActivationHeight int
		Status           string // "active"
	}
	ConsensusInfo struct { // consensus branch IDs
		Nextblock string // example: "e9ff75a6" (canopy)
		Chaintip  string // example: "e9ff75a6" (canopy)
	}
	ZcashdRpcReplyGetblockchaininfo struct {
		Chain           string
		Upgrades        map[string]Upgradeinfo
		Blocks          int
		BestBlockHash   string
		Consensus       ConsensusInfo
		EstimatedHeight int
	}

	// zcashd rpc "getinfo"
	ZcashdRpcReplyGetinfo struct {
		Build      string
		Subversion string
	}

	// zcashd rpc "getaddresstxids"
	ZcashdRpcRequestGetaddresstxids struct {
		Addresses []string `json:"addresses"`
		Start     uint64   `json:"start"`
		End       uint64   `json:"end"`
	}

	// zcashd rpc "z_gettreestate"
	ZcashdRpcReplyGettreestate struct {
		Height  int
		Hash    string
		Time    uint32
		Sapling struct {
			Commitments struct {
				FinalState string
			}
			SkipHash string
		}
		Orchard struct {
			Commitments struct {
				FinalState string
			}
			SkipHash string
		}
	}

	// zcashd rpc "getrawtransaction txid 1" (1 means verbose), there are
	// many more fields but these are the only ones we current need.
	ZcashdRpcReplyGetrawtransaction struct {
		Hex    string
		Height int
	}

	// zcashd rpc "getaddressbalance"
	ZcashdRpcRequestGetaddressbalance struct {
		Addresses []string `json:"addresses"`
	}
	ZcashdRpcReplyGetaddressbalance struct {
		Balance int64
	}

	// zcashd rpc "getaddressutxos"
	ZcashdRpcRequestGetaddressutxos struct {
		Addresses []string `json:"addresses"`
	}
	ZcashdRpcReplyGetaddressutxos struct {
		Address     string
		Txid        string
		OutputIndex int64
		Script      string
		Satoshis    uint64
		Height      int
	}

	// reply to getblock verbose=1 (json includes txid list)
	ZcashRpcReplyGetblock1 struct {
		Hash  string
		Tx    []string
		Trees struct {
			Sapling struct {
				Size uint32
			}
			Orchard struct {
				Size uint32
			}
		}
	}

	// reply to z_getsubtreesbyindex
	//
	// Each shielded transaction output of a particular shielded pool
	// type (Saping or Orchard) can be considered to have an index,
	// beginning with zero at the start of the chain (genesis block,
	// although there were no Sapling or Orchard transactions until
	// later). Each group of 2^16 (65536) of these is called a subtree.
	//
	// This data structure indicates the merkle root hash, and the
	// block height that the last output of this group falls on.
	// For example, Sapling output number 65535, which is the last
	// output in the first subtree, occurred somewhere within block
	// 558822. This height is returned by z_getsubtreesbyindex 0 1
	// (a request to return the subtree of the first group, and only
	// return one entry rather than all entries to the tip of the chain).
	//
	// Here is that example, except return (up to) 2 entries:
	//
	// $ zcash-cli z_getsubtreesbyindex sapling 0 2
	// {
	//  "pool": "sapling",
	//  "start_index": 0,
	//  "subtrees": [
	//    {
	//      "root": "754bb593ea42d231a7ddf367640f09bbf59dc00f2c1d2003cc340e0c016b5b13",
	//      "end_height": 558822
	//    },
	//    {
	//      "root": "03654c3eacbb9b93e122cf6d77b606eae29610f4f38a477985368197fd68e02d",
	//      "end_height": 670209
	//    }
	//   ]
	// }
	//
	Subtree struct {
		Root       string
		End_height int
	}

	ZcashdRpcReplyGetsubtreebyindex struct {
		Subtrees []Subtree
	}
)

// FirstRPC tests that we can successfully reach zcashd through the RPC
// interface. The specific RPC used here is not important.
func FirstRPC() {
	retryCount := 0
	for {
		_, err := GetBlockChainInfo()
		if err == nil {
			if retryCount > 0 {
				Log.Warn("getblockchaininfo RPC successful")
			}
			break
		}
		retryCount++
		if retryCount > 10 {
			Log.WithFields(logrus.Fields{
				"timeouts": retryCount,
			}).Fatal("unable to issue getblockchaininfo RPC call to zcashd node")
		}
		Log.WithFields(logrus.Fields{
			"error": err.Error(),
			"retry": retryCount,
		}).Warn("error with getblockchaininfo rpc, retrying...")
		Time.Sleep(time.Duration(10+retryCount*5) * time.Second) // backoff
	}
}

func GetBlockChainInfo() (*ZcashdRpcReplyGetblockchaininfo, error) {
	result, rpcErr := RawRequest("getblockchaininfo", []json.RawMessage{})
	if rpcErr != nil {
		return nil, rpcErr
	}
	var getblockchaininfoReply ZcashdRpcReplyGetblockchaininfo
	err := json.Unmarshal(result, &getblockchaininfoReply)
	if err != nil {
		return nil, err
	}
	return &getblockchaininfoReply, nil
}

func GetLightdInfo() (*walletrpc.LightdInfo, error) {
	result, rpcErr := RawRequest("getinfo", []json.RawMessage{})
	if rpcErr != nil {
		return nil, rpcErr
	}
	var getinfoReply ZcashdRpcReplyGetinfo
	err := json.Unmarshal(result, &getinfoReply)
	if err != nil {
		return nil, rpcErr
	}

	result, rpcErr = RawRequest("getblockchaininfo", []json.RawMessage{})
	if rpcErr != nil {
		return nil, rpcErr
	}
	var getblockchaininfoReply ZcashdRpcReplyGetblockchaininfo
	err = json.Unmarshal(result, &getblockchaininfoReply)
	if err != nil {
		return nil, rpcErr
	}
	// If the sapling consensus branch doesn't exist, it must be regtest
	var saplingHeight int
	if saplingJSON, ok := getblockchaininfoReply.Upgrades["76b809bb"]; ok { // Sapling ID
		saplingHeight = saplingJSON.ActivationHeight
	}

	vendor := "ECC LightWalletD"
	if DarksideEnabled {
		vendor = "ECC DarksideWalletD"
	}
	return &walletrpc.LightdInfo{
		Version:                 Version,
		Vendor:                  vendor,
		TaddrSupport:            true,
		ChainName:               getblockchaininfoReply.Chain,
		SaplingActivationHeight: uint64(saplingHeight),
		ConsensusBranchId:       getblockchaininfoReply.Consensus.Chaintip,
		BlockHeight:             uint64(getblockchaininfoReply.Blocks),
		GitCommit:               GitCommit,
		Branch:                  Branch,
		BuildDate:               BuildDate,
		BuildUser:               BuildUser,
		EstimatedHeight:         uint64(getblockchaininfoReply.EstimatedHeight),
		ZcashdBuild:             getinfoReply.Build,
		ZcashdSubversion:        getinfoReply.Subversion,
	}, nil
}

func getBlockFromRPC(height int) (*walletrpc.CompactBlock, error) {
	// `block.ParseFromSlice` correctly parses blocks containing v5
	// transactions, but incorrectly computes the IDs of the v5 transactions.
	// We temporarily paper over this bug by fetching the correct txids via a
	// verbose getblock RPC call, which returns the txids.
	//
	// Unfortunately, this RPC doesn't return the raw hex for the block,
	// so a second getblock RPC (non-verbose) is needed (below).
	// https://github.com/zcash/lightwalletd/issues/392

	heightJSON, err := json.Marshal(strconv.Itoa(height))
	if err != nil {
		Log.Fatal("getBlockFromRPC bad height argument", height, err)
	}
	params := make([]json.RawMessage, 2)
	params[0] = heightJSON
	// Fetch the block using the verbose option ("1") because it provides
	// both the list of txids, which we're not yet able to compute for
	// Orchard (V5) transactions, and the block hash (block ID), which
	// we need to fetch the raw data format of the same block. Don't fetch
	// by height in case a reorg occurs between the two getblock calls;
	// using block hash ensures that we're fetching the same block.
	params[1] = json.RawMessage("1")
	result, rpcErr := RawRequest("getblock", params)
	if rpcErr != nil {
		// Check to see if we are requesting a height the zcashd doesn't have yet
		if (strings.Split(rpcErr.Error(), ":"))[0] == "-8" {
			return nil, nil
		}
		return nil, fmt.Errorf("error requesting verbose block: %w", rpcErr)
	}
	var block1 ZcashRpcReplyGetblock1
	err = json.Unmarshal(result, &block1)
	if err != nil {
		return nil, err
	}
	blockHash, err := json.Marshal(block1.Hash)
	if err != nil {
		Log.Fatal("getBlockFromRPC bad block hash", block1.Hash)
	}
	params[0] = blockHash
	params[1] = json.RawMessage("0") // non-verbose (raw hex)
	result, rpcErr = RawRequest("getblock", params)

	// For some reason, the error responses are not JSON
	if rpcErr != nil {
		return nil, fmt.Errorf("error requesting block: %w", rpcErr)
	}

	var blockDataHex string
	err = json.Unmarshal(result, &blockDataHex)
	if err != nil {
		return nil, fmt.Errorf("error reading JSON response: %w", err)
	}

	blockData, err := hex.DecodeString(blockDataHex)
	if err != nil {
		return nil, fmt.Errorf("error decoding getblock output: %w", err)
	}

	block := parser.NewBlock()
	rest, err := block.ParseFromSlice(blockData)
	if err != nil {
		return nil, fmt.Errorf("error parsing block: %w", err)
	}
	if len(rest) != 0 {
		return nil, errors.New("received overlong message")
	}
	if block.GetHeight() != height {
		return nil, errors.New("received unexpected height block")
	}
	for i, t := range block.Transactions() {
		txid, err := hex.DecodeString(block1.Tx[i])
		if err != nil {
			return nil, fmt.Errorf("error decoding getblock txid: %w", err)
		}
		// convert from big-endian
		t.SetTxID(parser.Reverse(txid))
	}
	r := block.ToCompact()
	r.ChainMetadata.SaplingCommitmentTreeSize = block1.Trees.Sapling.Size
	r.ChainMetadata.OrchardCommitmentTreeSize = block1.Trees.Orchard.Size
	return r, nil
}

var (
	ingestorRunning  bool
	stopIngestorChan = make(chan struct{})
)

func startIngestor(c *BlockCache) {
	if !ingestorRunning {
		ingestorRunning = true
		go BlockIngestor(c, 0)
	}
}
func stopIngestor() {
	if ingestorRunning {
		ingestorRunning = false
		stopIngestorChan <- struct{}{}
	}
}

// BlockIngestor runs as a goroutine and polls zcashd for new blocks, adding them
// to the cache. The repetition count, rep, is nonzero only for unit-testing.
func BlockIngestor(c *BlockCache, rep int) {
	lastLog := Time.Now()
	lastHeightLogged := 0

	// Start listening for new blocks
	for i := 0; rep == 0 || i < rep; i++ {
		// stop if requested
		select {
		case <-stopIngestorChan:
			return
		default:
		}

		result, err := RawRequest("getbestblockhash", []json.RawMessage{})
		if err != nil {
			Log.WithFields(logrus.Fields{
				"error": err,
			}).Fatal("error zcashd getbestblockhash rpc")
		}
		var hashHex string
		err = json.Unmarshal(result, &hashHex)
		if err != nil {
			Log.Fatal("bad getbestblockhash return:", err, result)
		}
		lastBestBlockHash, err := hex.DecodeString(hashHex)
		if err != nil {
			Log.Fatal("error decoding getbestblockhash", err, hashHex)
		}

		height := c.GetNextHeight()
		if string(lastBestBlockHash) == string(parser.Reverse(c.GetLatestHash())) {
			// Synced
			c.Sync()
			if lastHeightLogged != height-1 {
				lastHeightLogged = height - 1
				Log.Info("Waiting for block: ", height)
			}
			Time.Sleep(2 * time.Second)
			lastLog = Time.Now()
			continue
		}
		var block *walletrpc.CompactBlock
		block, err = getBlockFromRPC(height)
		if err != nil {
			Log.Info("getblock ", height, " failed, will retry: ", err)
			Time.Sleep(8 * time.Second)
			continue
		}
		if block != nil && c.HashMatch(block.PrevHash) {
			if err = c.Add(height, block); err != nil {
				Log.Fatal("Cache add failed:", err)
			}
			// Don't log these too often.
			if DarksideEnabled || Time.Now().Sub(lastLog).Seconds() >= 4 {
				lastLog = Time.Now()
				Log.Info("Adding block to cache ", height, " ", displayHash(block.Hash))
			}
			continue
		}
		if height == c.GetFirstHeight() {
			c.Sync()
			Log.Info("Waiting for zcashd height to reach Sapling activation height ",
				"(", c.GetFirstHeight(), ")...")
			Time.Sleep(20 * time.Second)
			return
		}
		Log.Info("REORG: dropping block ", height-1, " ", displayHash(c.GetLatestHash()))
		c.Reorg(height - 1)
	}
}

// GetBlock returns the compact block at the requested height, first by querying
// the cache, then, if not found, will request the block from zcashd. It returns
// nil if no block exists at this height.
func GetBlock(cache *BlockCache, height int) (*walletrpc.CompactBlock, error) {
	// First, check the cache to see if we have the block
	var block *walletrpc.CompactBlock
	if cache != nil {
		block := cache.Get(height)
		if block != nil {
			return block, nil
		}
	}

	// Not in the cache
	block, err := getBlockFromRPC(height)
	if err != nil {
		return nil, err
	}
	if block == nil {
		// Block height is too large
		return nil, errors.New("block requested is newer than latest block")
	}
	return block, nil
}

// GetBlockRange returns a sequence of consecutive blocks in the given range.
func GetBlockRange(cache *BlockCache, blockOut chan<- *walletrpc.CompactBlock, errOut chan<- error, start, end int) {
	// Go over [start, end] inclusive
	low := start
	high := end
	if start > end {
		// reverse the order
		low, high = end, start
	}
	for i := low; i <= high; i++ {
		j := i
		if start > end {
			// reverse the order
			j = high - (i - low)
		}
		block, err := GetBlock(cache, j)
		if err != nil {
			errOut <- err
			return
		}
		blockOut <- block
	}
	errOut <- nil
}

func displayHash(hash []byte) string {
	return hex.EncodeToString(parser.Reverse(hash))
}
