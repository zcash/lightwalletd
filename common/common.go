package common

import (
	"encoding/hex"
	"encoding/json"
	"strconv"
	"strings"
	"time"

	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"github.com/zcash/lightwalletd/parser"
	"github.com/zcash/lightwalletd/walletrpc"
)

// RawRequest points to the function to send a an RPC request to zcashd;
// in production, it points to btcsuite/btcd/rpcclient/rawrequest.go:RawRequest();
// in unit tests it points to a function to mock RPCs to zcashd.
var RawRequest func(method string, params []json.RawMessage) (json.RawMessage, error)

// Sleep allows a request to time.Sleep() to be mocked for testing;
// in production, it points to the standard library time.Sleep();
// in unit tests it points to a mock function.
var Sleep func(d time.Duration)

// Log as a global variable simplifies logging
var Log *logrus.Entry

// GetSaplingInfo returns the result of the getblockchaininfo RPC to zcashd
func GetSaplingInfo() (int, int, string, string) {
	// This request must succeed or we can't go on; give zcashd time to start up
	var f interface{}
	retryCount := 0
	for {
		result, rpcErr := RawRequest("getblockchaininfo", []json.RawMessage{})
		if rpcErr == nil {
			if retryCount > 0 {
				Log.Warn("getblockchaininfo RPC successful")
			}
			err := json.Unmarshal(result, &f)
			if err != nil {
				Log.Fatalf("error parsing JSON getblockchaininfo response: %v", err)
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
			"error": rpcErr.Error(),
			"retry": retryCount,
		}).Warn("error with getblockchaininfo rpc, retrying...")
		Sleep(time.Duration(10+retryCount*5) * time.Second) // backoff
	}

	chainName := f.(map[string]interface{})["chain"].(string)

	upgradeJSON := f.(map[string]interface{})["upgrades"]
	saplingJSON := upgradeJSON.(map[string]interface{})["76b809bb"] // Sapling ID
	saplingHeight := saplingJSON.(map[string]interface{})["activationheight"].(float64)

	blockHeight := f.(map[string]interface{})["headers"].(float64)

	consensus := f.(map[string]interface{})["consensus"]

	branchID := consensus.(map[string]interface{})["nextblock"].(string)

	return int(saplingHeight), int(blockHeight), chainName, branchID
}

func getBlockFromRPC(height int) (*walletrpc.CompactBlock, error) {
	params := make([]json.RawMessage, 2)
	params[0] = json.RawMessage("\"" + strconv.Itoa(height) + "\"")
	params[1] = json.RawMessage("0") // non-verbose (raw hex)
	result, rpcErr := RawRequest("getblock", params)

	// For some reason, the error responses are not JSON
	if rpcErr != nil {
		// Check to see if we are requesting a height the zcashd doesn't have yet
		if (strings.Split(rpcErr.Error(), ":"))[0] == "-8" {
			return nil, nil
		}
		return nil, errors.Wrap(rpcErr, "error requesting block")
	}

	var blockDataHex string
	err := json.Unmarshal(result, &blockDataHex)
	if err != nil {
		return nil, errors.Wrap(err, "error reading JSON response")
	}

	blockData, err := hex.DecodeString(blockDataHex)
	if err != nil {
		return nil, errors.Wrap(err, "error decoding getblock output")
	}

	block := parser.NewBlock()
	rest, err := block.ParseFromSlice(blockData)
	if err != nil {
		return nil, errors.Wrap(err, "error parsing block")
	}
	if len(rest) != 0 {
		return nil, errors.New("received overlong message")
	}

	return block.ToCompact(), nil
}

// BlockIngestor runs as a goroutine and polls zcashd for new blocks, adding them
// to the cache. The repetition count, rep, is nonzero only for unit-testing.
func BlockIngestor(cache *BlockCache, startHeight int, rep int) {
	reorgCount := 0
	height := startHeight

	// Start listening for new blocks
	retryCount := 0
	for i := 0; rep == 0 || i < rep; i++ {
		block, err := getBlockFromRPC(height)
		if block == nil || err != nil {
			if err != nil {
				Log.WithFields(logrus.Fields{
					"height": height,
					"error":  err,
				}).Warn("error with getblock rpc")
				retryCount++
				if retryCount > 10 {
					Log.WithFields(logrus.Fields{
						"timeouts": retryCount,
					}).Fatal("unable to issue RPC call to zcashd node")
				}
			}
			// We're up to date in our polling; wait for a new block
			Sleep(10 * time.Second)
			continue
		}
		retryCount = 0

		if (height % 100) == 0 {
			Log.Info("Ingestor adding block to cache: ", height)
		}
		reorg, err := cache.Add(height, block)
		if err != nil {
			Log.Fatal("Cache add failed")
		}

		// Check for reorgs once we have inital block hash from startup
		if reorg {
			// This must back up at least 1, but it's arbitrary, any value
			// will work; this is probably a good balance.
			height -= 2
			reorgCount++
			if reorgCount > 50 {
				Log.Fatal("Reorg exceeded maximum number of sequential reorgs! Help!")
			}
			Log.WithFields(logrus.Fields{
				"height": height,
				"hash":   displayHash(block.Hash),
				"phash":  displayHash(block.PrevHash),
				"reorg":  reorgCount,
			}).Warn("REORG")
			continue
		} else {
			reorgCount = 0
		}
		height++
	}
}

// GetBlock returns the compact block at the requested height, first by querying
// the cache, then, if not found, will request the block from zcashd. It returns
// nil if no block exists at this height.
func GetBlock(cache *BlockCache, height int) (*walletrpc.CompactBlock, error) {
	// First, check the cache to see if we have the block
	block := cache.Get(height)
	if block != nil {
		return block, nil
	}

	// Not in the cache, ask zcashd
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
func GetBlockRange(cache *BlockCache, blockOut chan<- walletrpc.CompactBlock, errOut chan<- error, start, end int) {
	// Go over [start, end] inclusive
	for i := start; i <= end; i++ {
		block, err := GetBlock(cache, i)
		if err != nil {
			errOut <- err
			return
		}
		blockOut <- *block
	}
	errOut <- nil
}

func displayHash(hash []byte) string {
	rhash := make([]byte, len(hash))
	copy(rhash, hash)
	// Reverse byte order
	for i := 0; i < len(rhash)/2; i++ {
		j := len(rhash) - 1 - i
		rhash[i], rhash[j] = rhash[j], rhash[i]
	}
	return hex.EncodeToString(rhash)
}
