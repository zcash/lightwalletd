package common

import (
	"encoding/hex"
	"encoding/json"
	"strconv"
	"strings"
	"time"

	"github.com/btcsuite/btcd/rpcclient"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"github.com/zcash-hackworks/lightwalletd/parser"
	"github.com/zcash-hackworks/lightwalletd/walletrpc"
)

type Options struct {
	BindAddr          string `json:"bind_address,omitempty"`
	TLSCertPath       string `json:"tls_cert_path,omitempty"`
	TLSKeyPath        string `json:"tls_cert_key,omitempty"`
	LogLevel          uint64 `json:"log_level,omitempty"`
	LogFile           string `json:"log_file,omitempty"`
	ZcashConfPath     string `json:"zcash_conf,omitempty"`
	NoTLSVeryInsecure bool   `json:"no_tls_very_insecure,omitempty"`
	CacheSize         int    `json:"cache_size,omitempty"`
}

func GetSaplingInfo(rpcClient *rpcclient.Client, log *logrus.Entry) (int, int, string, string) {
	// This request must succeed or we can't go on; give zcashd time to start up
	var f interface{}
	retryCount := 0
	for {
		result, rpcErr := rpcClient.RawRequest("getblockchaininfo", make([]json.RawMessage, 0))
		if rpcErr == nil {
			if retryCount > 0 {
				log.Warn("getblockchaininfo RPC successful")
			}
			err := json.Unmarshal(result, &f)
			if err != nil {
				log.Fatalf("error parsing JSON getblockchaininfo response: %v", err)
			}
			break
		}
		retryCount++
		if retryCount > 10 {
			log.WithFields(logrus.Fields{
				"timeouts": retryCount,
			}).Fatal("unable to issue getblockchaininfo RPC call to zcashd node")
		}
		log.WithFields(logrus.Fields{
			"error": rpcErr.Error(),
			"retry": retryCount,
		}).Warn("error with getblockchaininfo rpc, retrying...")
		time.Sleep(time.Duration(10+retryCount*5) * time.Second) // backoff
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

func getBlockFromRPC(rpcClient *rpcclient.Client, height int) (*walletrpc.CompactBlock, error) {
	params := make([]json.RawMessage, 2)
	params[0] = json.RawMessage("\"" + strconv.Itoa(height) + "\"")
	params[1] = json.RawMessage("0")
	result, rpcErr := rpcClient.RawRequest("getblock", params)

	// For some reason, the error responses are not JSON
	if rpcErr != nil {
		errParts := strings.SplitN(rpcErr.Error(), ":", 2)
		errCode, err := strconv.ParseInt(errParts[0], 10, 32)
		// Check to see if we are requesting a height the zcashd doesn't have yet
		if err == nil && errCode == -8 {
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

func BlockIngestor(rpcClient *rpcclient.Client, cache *BlockCache, log *logrus.Entry, startHeight int) {
	reorgCount := 0
	height := startHeight

	// Start listening for new blocks
	retryCount := 0
	for {
		block, err := getBlockFromRPC(rpcClient, height)
		if block == nil || err != nil {
			if err != nil {
				log.WithFields(logrus.Fields{
					"height": height,
					"error":  err,
				}).Warn("error with getblock rpc")
				retryCount++
				if retryCount > 10 {
					log.WithFields(logrus.Fields{
						"timeouts": retryCount,
					}).Fatal("unable to issue RPC call to zcashd node")
				}
			}
			// We're up to date in our polling; wait for a new block
			time.Sleep(10 * time.Second)
			continue
		}
		retryCount = 0

		log.Info("Ingestor adding block to cache: ", height)
		err, reorg := cache.Add(height, block)

		if err != nil {
			// It's unclear how this will recover, but we certainly
			// don't want to loop full-speed
			log.Error("Error adding block to cache: ", err)
			time.Sleep(10 * time.Second)
			continue
		}

		// Check for reorgs once we have inital block hash from startup
		if reorg {
			// This must back up at least 1, but it's arbitrary, any value
			// will work; this is probably a good balance.
			height -= 2
			reorgCount++
			if reorgCount > 10 {
				log.Fatal("Reorg exceeded max of 100 blocks! Help!")
			}
			log.WithFields(logrus.Fields{
				"height": height,
				"hash":   displayHash(block.Hash),
				"phash":  displayHash(block.PrevHash),
				"reorg":  reorgCount,
			}).Warn("REORG")
			continue
		}
		reorgCount = 0
		height++
	}
}

func GetBlock(rpcClient *rpcclient.Client, cache *BlockCache, height int) (*walletrpc.CompactBlock, error) {
	// First, check the cache to see if we have the block
	block := cache.Get(height)
	if block != nil {
		return block, nil
	}

	// Not in the cache, ask zcashd
	block, err := getBlockFromRPC(rpcClient, height)
	if err != nil {
		return nil, err
	}
	if block == nil {
		// Block height is too large
		return nil, errors.New("block requested is newer than latest block")
	}
	return block, nil
}

func GetBlockRange(rpcClient *rpcclient.Client, cache *BlockCache,
	blockOut chan<- walletrpc.CompactBlock, errOut chan<- error, start, end int) {

	// Go over [start, end] inclusive
	for i := start; i <= end; i++ {
		block, err := GetBlock(rpcClient, cache, i)
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
