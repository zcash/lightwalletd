// Copyright (c) 2019-2020 The Zcash developers
// Distributed under the MIT software license, see the accompanying
// file COPYING or https://www.opensource.org/licenses/mit-license.php .

// Package frontend implements the gRPC handlers called by the wallets.
package frontend

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/zcash/lightwalletd/common"
	"github.com/zcash/lightwalletd/hash32"
	"github.com/zcash/lightwalletd/parser"
	"github.com/zcash/lightwalletd/walletrpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type lwdStreamer struct {
	cache      *common.BlockCache
	chainName  string
	pingEnable bool
	mutex      sync.Mutex
	walletrpc.UnimplementedCompactTxStreamerServer
}

// NewLwdStreamer constructs a gRPC context.
func NewLwdStreamer(cache *common.BlockCache, chainName string, enablePing bool) (walletrpc.CompactTxStreamerServer, error) {
	return &lwdStreamer{cache: cache, chainName: chainName, pingEnable: enablePing}, nil
}

// DarksideStreamer holds the gRPC state for darksidewalletd.
type DarksideStreamer struct {
	cache *common.BlockCache
	walletrpc.UnimplementedDarksideStreamerServer
}

// NewDarksideStreamer constructs a gRPC context for darksidewalletd.
func NewDarksideStreamer(cache *common.BlockCache) (walletrpc.DarksideStreamerServer, error) {
	return &DarksideStreamer{cache: cache}, nil
}

// Test to make sure Address is a single t address, return a gRPC error
func checkTaddress(taddr string) error {
	match, err := regexp.Match("\\At[a-zA-Z0-9]{34}\\z", []byte(taddr))
	if err != nil {
		return status.Errorf(codes.InvalidArgument,
			"checkTaddress: invalid transparent address: %s error: %s", taddr, err.Error())
	}
	if !match {
		return status.Errorf(codes.InvalidArgument,
			"checkTaddress: transparent address %s contains invalid characters", taddr)
	}
	return nil
}

// GetLatestBlock returns the height and hash of the best chain, according to zcashd.
func (s *lwdStreamer) GetLatestBlock(ctx context.Context, placeholder *walletrpc.ChainSpec) (*walletrpc.BlockID, error) {
	common.Log.Debugf("gRPC GetLatestBlock(%+v)\n", placeholder)

	blockChainInfo, err := common.GetBlockChainInfo()
	if err != nil {
		return nil, status.Errorf(codes.Unavailable,
			"GetLatestBlock: GetBlockChainInfo failed: %s", err.Error())
	}
	bestBlockHashBigEndian, err := hash32.Decode(blockChainInfo.BestBlockHash)
	if err != nil {
		return nil, status.Errorf(codes.Internal,
			"GetLatestBlock: decode block hash %s failed: %s", blockChainInfo.BestBlockHash, err.Error())
	}
	// Binary block hash should always be in little-endian format
	bestBlockHash := hash32.Reverse(bestBlockHashBigEndian)
	r := &walletrpc.BlockID{
		Height: uint64(blockChainInfo.Blocks),
		Hash:   hash32.ToSlice(bestBlockHash)}
	common.Log.Tracef("  return: %+v\n", r)
	return r, nil
}

// GetTaddressTxids is a streaming RPC that returns transactions that have
// the given transparent address (taddr) as either an input or output.
// NB, this method is misnamed, it does not return txids.
func (s *lwdStreamer) GetTaddressTransactions(addressBlockFilter *walletrpc.TransparentAddressBlockFilter, resp walletrpc.CompactTxStreamer_GetTaddressTransactionsServer) error {
	common.Log.Debugf("gRPC GetTaddressTransactions(%+v)\n", addressBlockFilter)
	if err := checkTaddress(addressBlockFilter.Address); err != nil {
		// This returns a gRPC-compatible error.
		return err
	}

	if addressBlockFilter.Range == nil {
		return status.Error(codes.InvalidArgument, "GetTaddressTransactions: must specify block range")
	}
	if addressBlockFilter.Range.Start == nil {
		return status.Error(codes.InvalidArgument, "GetTaddressTransactions: must specify a start block height")
	}

	request := &common.ZcashdRpcRequestGetaddresstxids{
		Addresses: []string{addressBlockFilter.Address},
		Start:     addressBlockFilter.Range.Start.Height,
	}
	if addressBlockFilter.Range.End != nil {
		request.End = addressBlockFilter.Range.End.Height
	}

	param, err := json.Marshal(request)
	if err != nil {
		return status.Errorf(codes.InvalidArgument,
			"GetTaddressTransactions: error marshalling request: %s", err.Error())
	}
	params := []json.RawMessage{param}

	result, rpcErr := common.RawRequest("getaddresstxids", params)

	// For some reason, the error responses are not JSON
	if rpcErr != nil {
		return status.Errorf(codes.InvalidArgument,
			"GetTaddressTransactions: getaddresstxids failed, error: %s", rpcErr.Error())
	}

	var txids []string
	err = json.Unmarshal(result, &txids)
	if err != nil {
		return status.Errorf(codes.Unknown,
			"GetSubtreeRoots: error unmarshalling getaddresstxids reply: %s", err.Error())
	}

	timeout, cancel := context.WithTimeout(resp.Context(), 30*time.Second)
	defer cancel()

	for _, txidstr := range txids {
		txidBigEndian, _ := hex.DecodeString(txidstr)
		// Txid is read as a string, which is in big-endian order. But when converting
		// to bytes, it should be little-endian
		txHash := hash32.ReverseSlice(txidBigEndian)
		tx, err := s.GetTransaction(timeout, &walletrpc.TxFilter{Hash: txHash})
		if err != nil {
			return err
		}
		if err = resp.Send(tx); err != nil {
			return err
		}
	}
	return nil
}

// This method is deprecated; use GetTaddressTransactions instead. The two functions have the
// same functionality, but the name GetTaddressTxids is misleading, because the method returns
// transactions, not transaction IDs (txids). See https://github.com/zcash/lightwalletd/issues/426
func (s *lwdStreamer) GetTaddressTxids(addressBlockFilter *walletrpc.TransparentAddressBlockFilter, resp walletrpc.CompactTxStreamer_GetTaddressTxidsServer) error {
	common.Log.Debugf("gRPC GetTaddressTxids, deprecated, calling GetTaddressTransactions...\n")
	return s.GetTaddressTransactions(addressBlockFilter, resp)
}

// GetBlock returns the compact block at the requested height. Requesting a
// block by hash is not yet supported.
func (s *lwdStreamer) GetBlock(ctx context.Context, id *walletrpc.BlockID) (*walletrpc.CompactBlock, error) {
	common.Log.Debugf("gRPC GetBlock(%+v)\n", id)
	if id.Height == 0 && id.Hash == nil {
		return nil, status.Error(codes.InvalidArgument,
			"GetBlock: request for unspecified identifier")
	}

	// Precedence: a hash is more specific than a height. If we have it, use it first.
	if id.Hash != nil {
		// TODO: Get block by hash
		// see https://github.com/zcash/lightwalletd/pull/309
		return nil, status.Error(codes.InvalidArgument,
			"GetBlock: Block hash specifier is not yet implemented")
	}
	cBlock, err := common.GetBlock(s.cache, int(id.Height))

	if err != nil {
		return nil, err
	}
	common.Log.Tracef("  return: %+v\n", cBlock)
	return cBlock, err
}

// GetBlockNullifiers is the same as GetBlock except that it returns the compact block
// with actions containing only the nullifiers (a subset of the full compact block).
func (s *lwdStreamer) GetBlockNullifiers(ctx context.Context, id *walletrpc.BlockID) (*walletrpc.CompactBlock, error) {
	common.Log.Debugf("gRPC GetBlockNullifiers(%+v)\n", id)
	if id.Height == 0 && id.Hash == nil {
		return nil, status.Error(codes.InvalidArgument,
			"GetBlockNullifiers: must specify a block height")
	}

	// Precedence: a hash is more specific than a height. If we have it, use it first.
	if id.Hash != nil {
		// TODO: Get block by hash
		// see https://github.com/zcash/lightwalletd/pull/309
		return nil, status.Error(codes.InvalidArgument,
			"GetBlockNullifiers: GetBlock by Hash is not yet implemented")
	}
	cBlock, err := common.GetBlock(s.cache, int(id.Height))
	if err != nil {
		// GetBlock() returns gRPC-compatible errors.
		return nil, err
	}
	for _, tx := range cBlock.Vtx {
		for i, action := range tx.Actions {
			tx.Actions[i] = &walletrpc.CompactOrchardAction{Nullifier: action.Nullifier}
		}
		tx.Outputs = nil
		tx.Vin = nil
		tx.Vout = nil
	}
	// these are not needed (we prefer to save bandwidth)
	cBlock.ChainMetadata.SaplingCommitmentTreeSize = 0
	cBlock.ChainMetadata.OrchardCommitmentTreeSize = 0
	common.Log.Tracef("  return: %+v\n", cBlock)
	return cBlock, err
}

// GetBlockRange is a streaming RPC that returns blocks, in compact form,
// (as also returned by GetBlock) from the block height 'start' to height
// 'end' inclusively.
func (s *lwdStreamer) GetBlockRange(span *walletrpc.BlockRange, resp walletrpc.CompactTxStreamer_GetBlockRangeServer) error {
	common.Log.Debugf("gRPC GetBlockRange(%+v)\n", span)
	blockChan := make(chan *walletrpc.CompactBlock)
	if span.Start == nil || span.End == nil {
		return status.Error(codes.InvalidArgument,
			"GetBlockRange: must specify start and end heights")
	}
	errChan := make(chan error)
	go common.GetBlockRange(s.cache, blockChan, errChan, span)

	for {
		select {
		case err := <-errChan:
			// this will also catch context.DeadlineExceeded from the timeout
			return err
		case cBlock := <-blockChan:
			err := resp.Send(cBlock)
			if err != nil {
				return err
			}
		}
	}
}

// GetBlockRangeNullifiers is the same as GetBlockRange except that only
// the actions contain only nullifiers (a subset of the full compact block).
func (s *lwdStreamer) GetBlockRangeNullifiers(span *walletrpc.BlockRange, resp walletrpc.CompactTxStreamer_GetBlockRangeNullifiersServer) error {
	common.Log.Debugf("gRPC GetBlockRangeNullifiers(%+v)\n", span)
	blockChan := make(chan *walletrpc.CompactBlock)
	if span.Start == nil || span.End == nil {
		return status.Error(codes.InvalidArgument,
			"GetBlockRangeNullifiers: must specify start and end heights")
	}
	// Remove requests for transparent elements (use GetBlockRange to get those);
	// this function returns only nullifiers.
	filtered := make([]walletrpc.PoolType, 0)
	for _, poolType := range span.PoolTypes {
		if poolType != walletrpc.PoolType_TRANSPARENT {
			filtered = append(filtered, poolType)
		}
	}
	span.PoolTypes = filtered
	errChan := make(chan error)
	go common.GetBlockRange(s.cache, blockChan, errChan, span)

	for {
		select {
		case err := <-errChan:
			// this will also catch context.DeadlineExceeded from the timeout
			return err
		case cBlock := <-blockChan:
			for _, tx := range cBlock.Vtx {
				for i, action := range tx.Actions {
					tx.Actions[i] = &walletrpc.CompactOrchardAction{Nullifier: action.Nullifier}
				}
				tx.Outputs = nil
			}
			// these are not needed (we prefer to save bandwidth)
			cBlock.ChainMetadata.SaplingCommitmentTreeSize = 0
			cBlock.ChainMetadata.OrchardCommitmentTreeSize = 0
			if err := resp.Send(cBlock); err != nil {
				return err
			}
		}
	}
}

// GetTreeState returns the note commitment tree state corresponding to the given block.
// See section 3.7 of the Zcash protocol specification. It returns several other useful
// values also (even though they can be obtained using GetBlock).
// The block can be specified by either height or hash.
func (s *lwdStreamer) GetTreeState(ctx context.Context, id *walletrpc.BlockID) (*walletrpc.TreeState, error) {
	if id.Height == 0 && id.Hash == nil {
		return nil, status.Error(codes.InvalidArgument,
			"GetTreeState: must specify a block height or ID (hash)")
	}
	// The Zcash z_gettreestate rpc accepts either a block height or block hash
	params := make([]json.RawMessage, 1)
	var hashJSON []byte
	if id.Height > 0 {
		heightJSON, err := json.Marshal(strconv.Itoa(int(id.Height)))
		if err != nil {
			return nil, status.Errorf(codes.InvalidArgument,
				"GetTreeState: cannot parse block height: %s", err.Error())
		}
		common.Log.Debugf("gRPC GetTreeState(height=%+v)\n", id.Height)
		params[0] = heightJSON
	} else {
		// id.Hash is big-endian, keep in big-endian for the rpc
		hash := hex.EncodeToString(id.Hash)
		common.Log.Debugf("gRPC GetTreeState(hash=%+v)\n", hash)
		hashJSON, err := json.Marshal(hash)
		if err != nil {
			return nil, status.Errorf(codes.InvalidArgument,
				"GetTreeState: cannot marshal block hash: %s", err.Error())
		}
		params[0] = hashJSON
	}
	var gettreestateReply common.ZcashdRpcReplyGettreestate
	for {
		result, rpcErr := common.RawRequest("z_gettreestate", params)
		if rpcErr != nil {
			return nil, status.Errorf(codes.InvalidArgument,
				"GetTreeState: z_gettreestate failed: %s", rpcErr.Error())
		}
		err := json.Unmarshal(result, &gettreestateReply)
		if err != nil {
			return nil, status.Errorf(codes.InvalidArgument,
				"GetTreeState: cannot marshal treestate: %s", err.Error())
		}
		if gettreestateReply.Sapling.Commitments.FinalState != "" {
			break
		}
		if gettreestateReply.Sapling.SkipHash == "" {
			break
		}
		hashJSON, err = json.Marshal(gettreestateReply.Sapling.SkipHash)
		if err != nil {
			return nil, status.Errorf(codes.InvalidArgument,
				"GetTreeState: cannot marshal SkipHash: %s", err.Error())
		}
		params[0] = hashJSON
	}
	if gettreestateReply.Sapling.Commitments.FinalState == "" {
		return nil, status.Error(codes.InvalidArgument,
			"GetTreeState: z_gettreestate did not return treestate")
	}
	r := &walletrpc.TreeState{
		Network:     s.chainName,
		Height:      uint64(gettreestateReply.Height),
		Hash:        gettreestateReply.Hash,
		Time:        gettreestateReply.Time,
		SaplingTree: gettreestateReply.Sapling.Commitments.FinalState,
		OrchardTree: gettreestateReply.Orchard.Commitments.FinalState,
	}
	common.Log.Tracef("  return: %+v\n", r)
	return r, nil
}

func (s *lwdStreamer) GetLatestTreeState(ctx context.Context, in *walletrpc.Empty) (*walletrpc.TreeState, error) {
	common.Log.Debugf("gRPC GetLatestTreeState()\n")
	blockChainInfo, err := common.GetBlockChainInfo()
	if err != nil {
		return nil, status.Errorf(codes.Unavailable,
			"GetLatestTreeState: getblockchaininfo failed, error: %s", err.Error())
	}
	latestHeight := blockChainInfo.Blocks
	r, err := s.GetTreeState(ctx, &walletrpc.BlockID{Height: uint64(latestHeight)})
	if err == nil {
		common.Log.Tracef("  return: %+v\n", r)
	}
	return r, err
}

// GetTransaction returns the raw transaction bytes that are returned
// by the zcashd 'getrawtransaction' RPC.
func (s *lwdStreamer) GetTransaction(ctx context.Context, txf *walletrpc.TxFilter) (*walletrpc.RawTransaction, error) {
	common.Log.Debugf("gRPC GetTransaction(%+v)\n", txf)
	if txf.Hash != nil {
		if len(txf.Hash) != 32 {
			return nil, status.Errorf(codes.InvalidArgument,
				"GetTransaction: transaction ID has invalid length: %d", len(txf.Hash))
		}
		// Convert from little endian to big endian.
		txidHex := hash32.Encode(hash32.Reverse(hash32.FromSlice(txf.Hash)))
		txidJSON, err := json.Marshal(txidHex)
		if err != nil {
			return nil, status.Errorf(codes.InvalidArgument,
				"GetTransaction: Cannot marshal txid: %s", err.Error())
		}

		params := []json.RawMessage{txidJSON, json.RawMessage("1")}
		result, rpcErr := common.RawRequest("getrawtransaction", params)
		if rpcErr != nil {
			// For some reason, the error responses are not JSON
			return nil, status.Errorf(codes.NotFound,
				"GetTransaction: getrawtransaction %s failed: %s", txidHex, rpcErr.Error())
		}
		tx, err := common.ParseRawTransaction(result)
		if err != nil {
			return nil, status.Errorf(codes.Internal,
				"GetTransaction: Cannot parse transaction: %s", err.Error())
		}
		common.Log.Tracef("  return: %+v\n", tx)
		return tx, err
	}

	if txf.Block != nil && txf.Block.Hash != nil {
		return nil, status.Error(codes.InvalidArgument,
			"GetTransaction: specify a txid, not a blockhash+num")
	}
	return nil, status.Error(codes.InvalidArgument,
		"GetTransaction: specify a txid")
}

// GetLightdInfo gets the LightWalletD (this server) info, and includes information
// it gets from its backend zcashd.
func (s *lwdStreamer) GetLightdInfo(ctx context.Context, in *walletrpc.Empty) (*walletrpc.LightdInfo, error) {
	lightdinfo, err := common.GetLightdInfo()
	if err != nil {
		return nil, status.Errorf(codes.Internal, "GetLightdInfo failed: %s", err.Error())
	}
	return lightdinfo, err
}

// SendTransaction forwards raw transaction bytes to a zcashd instance over JSON-RPC
func (s *lwdStreamer) SendTransaction(ctx context.Context, rawtx *walletrpc.RawTransaction) (*walletrpc.SendResponse, error) {
	common.Log.Debugf("gRPC SendTransaction(%+v)\n", rawtx)
	// sendrawtransaction "hexstring" ( allowhighfees )
	//
	// Submits raw transaction (binary) to local node and network.
	//
	// Result:
	// "hex"             (string) The transaction hash in hex

	// Verify rawtx
	if rawtx == nil || rawtx.Data == nil {
		return nil, status.Error(codes.InvalidArgument, "bad transaction data")
	}

	// Construct raw JSON-RPC params
	params := make([]json.RawMessage, 1)
	txJSON, err := json.Marshal(hex.EncodeToString(rawtx.Data))
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "cannot marshal tx: %s", err.Error())
	}
	params[0] = txJSON
	result, rpcErr := common.RawRequest("sendrawtransaction", params)

	var errCode int64
	var errMsg string

	// For some reason, the error responses are not JSON
	if rpcErr != nil {
		errParts := strings.SplitN(rpcErr.Error(), ":", 2)
		if len(errParts) < 2 {
			return nil, status.Errorf(codes.Unknown,
				"sendTransaction couldn't parse sendrawtransaction error code, error: %s", rpcErr.Error())
		}
		errMsg = strings.TrimSpace(errParts[1])
		errCode, err = strconv.ParseInt(errParts[0], 10, 32)
		if err != nil {
			// This should never happen. We can't panic here, but it's that class of error.
			// This is why we need integration testing to work better than regtest currently does. TODO.
			return nil, status.Errorf(codes.Unknown,
				"sendTransaction couldn't parse error code, error: %s", err.Error())
		}
	} else {
		// Return the transaction ID (txid) as hex string.
		errMsg = string(result)
	}

	// TODO these are called Error but they aren't at the moment.
	// A success will return code 0 and message txhash.
	r := &walletrpc.SendResponse{
		ErrorCode:    int32(errCode),
		ErrorMessage: errMsg,
	}
	common.Log.Tracef("  return: %+v\n", r)
	return r, nil
}

func getTaddressBalanceZcashdRpc(addressList []string) (*walletrpc.Balance, error) {
	for _, addr := range addressList {
		if err := checkTaddress(addr); err != nil {
			return nil, err
		}
	}
	params := make([]json.RawMessage, 1)
	addrList := &common.ZcashdRpcRequestGetaddressbalance{
		Addresses: addressList,
	}
	param, err := json.Marshal(addrList)
	if err != nil {
		return nil, err
	}
	params[0] = param

	result, rpcErr := common.RawRequest("getaddressbalance", params)
	if rpcErr != nil {
		var code codes.Code
		switch {
		case strings.Contains(rpcErr.Error(), "Invalid address"):
			code = codes.InvalidArgument
		case strings.Contains(rpcErr.Error(), "No information available"):
			code = codes.NotFound
		}
		return nil, status.Errorf(code,
			"getTaddressBalanceZcashdRpc: getaddressbalance error: %s", rpcErr.Error())
	}
	var balanceReply common.ZcashdRpcReplyGetaddressbalance
	err = json.Unmarshal(result, &balanceReply)
	if err != nil {
		return nil, status.Errorf(codes.Unknown,
			"getTaddressBalanceZcashdRpc: failed to unmarshal getaddressbalance reply, error: %s", err.Error())
	}
	return &walletrpc.Balance{ValueZat: balanceReply.Balance}, nil
}

// GetTaddressBalance returns the total balance for a list of taddrs
func (s *lwdStreamer) GetTaddressBalance(ctx context.Context, addresses *walletrpc.AddressList) (*walletrpc.Balance, error) {
	common.Log.Debugf("gRPC GetTaddressBalance(%+v)\n", addresses)
	r, err := getTaddressBalanceZcashdRpc(addresses.Addresses)
	if err == nil {
		common.Log.Tracef("  return: %+v\n", r)
	}
	return r, err
}

// GetTaddressBalanceStream returns the total balance for a list of taddrs
func (s *lwdStreamer) GetTaddressBalanceStream(addresses walletrpc.CompactTxStreamer_GetTaddressBalanceStreamServer) error {
	common.Log.Debugf("gRPC GetTaddressBalanceStream(%+v)\n", addresses)
	addressList := make([]string, 0)
	for {
		addr, err := addresses.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			return status.Errorf(codes.Internal, "GetTaddressBalanceStream Recv error: %s", err.Error())
		}
		addressList = append(addressList, addr.Address)
	}
	balance, err := getTaddressBalanceZcashdRpc(addressList)
	if err != nil {
		return err
	}
	addresses.SendAndClose(balance)
	common.Log.Tracef("  return: %+v\n", balance)
	return nil
}

func (s *lwdStreamer) GetMempoolStream(_empty *walletrpc.Empty, resp walletrpc.CompactTxStreamer_GetMempoolStreamServer) error {
	common.Log.Debugf("gRPC GetMempoolStream()\n")
	err := common.GetMempool(func(tx *walletrpc.RawTransaction) error {
		return resp.Send(tx)
	})
	return err
}

// Key is 32-byte txid (as a 64-character string), data is pointer to compact tx
// if the tx has shielded parts, or else it will be a null tx, including a zero-length
// txid. (We do need these entries in the map so we don't fetch the tx again.)
var mempoolMap *map[string]*walletrpc.CompactTx

// Txids in big-endian hex (from the backend)
var mempoolList []string

// Last time we pulled a copy of the mempool from zcashd.
var lastMempool time.Time

func (s *lwdStreamer) GetMempoolTx(exclude *walletrpc.GetMempoolTxRequest, resp walletrpc.CompactTxStreamer_GetMempoolTxServer) error {
	common.Log.Debugf("gRPC GetMempoolTx(%+v)\n", exclude)
	for i := 0; i < len(exclude.ExcludeTxidSuffixes); i++ {
		if len(exclude.ExcludeTxidSuffixes[i]) > 32 {
			return status.Errorf(codes.InvalidArgument, "exclude txid %d is larger than 32 bytes", i)
		}
	}
	if slices.Contains(exclude.PoolTypes, walletrpc.PoolType_POOL_TYPE_INVALID) {
		return status.Errorf(codes.InvalidArgument, "invalid pool type requested")
	}
	if len(exclude.PoolTypes) == 0 {
		// legacy behavior: return only blocks containing shielded components.
		exclude.PoolTypes = []walletrpc.PoolType{
			walletrpc.PoolType_SAPLING,
			walletrpc.PoolType_ORCHARD,
		}
	}
	s.mutex.Lock()
	defer s.mutex.Unlock()

	if time.Since(lastMempool).Seconds() >= 2 {
		lastMempool = time.Now()
		// Refresh our copy of the mempool.
		params := make([]json.RawMessage, 0)
		result, rpcErr := common.RawRequest("getrawmempool", params)
		if rpcErr != nil {
			return status.Errorf(codes.Internal, "GetMempoolTx: getrawmempool error: %s", rpcErr.Error())
		}
		err := json.Unmarshal(result, &mempoolList)
		if err != nil {
			return status.Errorf(codes.Unknown,
				"GetMempoolTx: failed to unmarshal getrawmempool reply, error: %s", err.Error())
		}
		newmempoolMap := make(map[string]*walletrpc.CompactTx)
		if mempoolMap == nil {
			mempoolMap = &newmempoolMap
		}
		for _, txidstr := range mempoolList {
			if ctx, ok := (*mempoolMap)[txidstr]; ok {
				// This ctx has already been fetched, copy pointer to it.
				newmempoolMap[txidstr] = ctx
				continue
			}
			txidJSON, err := json.Marshal(txidstr)
			if err != nil {
				return status.Errorf(codes.Unknown,
					"GetMempoolTx: failed to marshal txid, error: %s", err.Error())
			}
			// The "0" is because we only need the raw hex, which is returned as
			// just a hex string, and not even a json string (with quotes).
			params := []json.RawMessage{txidJSON, json.RawMessage("0")}
			result, rpcErr := common.RawRequest("getrawtransaction", params)
			if rpcErr != nil {
				// Not an error; mempool transactions can disappear
				continue
			}
			// strip the quotes
			var txStr string
			err = json.Unmarshal(result, &txStr)
			if err != nil {
				return status.Errorf(codes.Internal,
					"GetMempoolTx: failed to unmarshal getrawtransaction reply, error: %s", err.Error())
			}

			// convert to binary
			txBytes, err := hex.DecodeString(txStr)
			if err != nil {
				return status.Errorf(codes.Internal,
					"GetMempoolTx: failed decode getrawtransaction reply, error: %s", err.Error())
			}
			tx := parser.NewTransaction()
			txdata, err := tx.ParseFromSlice(txBytes)
			if err != nil {
				return status.Errorf(codes.Internal,
					"GetMempoolTx: failed to parse getrawtransaction reply, error: %s", err.Error())
			}
			if len(txdata) > 0 {
				return status.Error(codes.Internal,
					"GetMempoolTx: extra data deserializing transaction")
			}
			txidBigEndian, err := hex.DecodeString(txidstr)
			if err != nil {
				return status.Errorf(codes.Internal,
					"GetMempoolTx: failed decode txid, error: %s", err.Error())
			}
			// convert from big endian bytes to little endian and set as the txid
			tx.SetTxID(hash32.Reverse(hash32.FromSlice(txidBigEndian)))
			newmempoolMap[txidstr] = tx.ToCompact( /* height */ 0)
		}
		mempoolMap = &newmempoolMap
	}
	excludeHex := make([]string, len(exclude.ExcludeTxidSuffixes))
	for i := range exclude.ExcludeTxidSuffixes {
		rev := make([]byte, len(exclude.ExcludeTxidSuffixes[i]))
		for j := range rev {
			rev[j] = exclude.ExcludeTxidSuffixes[i][len(exclude.ExcludeTxidSuffixes[i])-j-1]
		}
		excludeHex[i] = hex.EncodeToString(rev)
	}
	for _, txid := range MempoolFilter(mempoolList, excludeHex) {
		if ftx := common.FilterTxPool((*mempoolMap)[txid], exclude.PoolTypes); ftx != nil {
			err := resp.Send(ftx)
			if err != nil {
				return err
			}
		}
	}
	return nil
}

// Return the subset of items that aren't excluded, but
// if more than one item matches an exclude entry, return
// all those items.
func MempoolFilter(items, exclude []string) []string {
	slices.Sort(items)
	slices.Sort(exclude)
	// Determine how many items match each exclude item.
	nmatches := make([]int, len(exclude))
	// is the exclude string less than the item string?
	lessthan := func(e, i string) bool {
		l := min(len(e), len(i))
		return e < i[0:l]
	}
	ei := 0
	for _, item := range items {
		for ei < len(exclude) && lessthan(exclude[ei], item) {
			ei++
		}
		match := ei < len(exclude) && strings.HasPrefix(item, exclude[ei])
		if match {
			nmatches[ei]++
		}
	}

	// Add each item that isn't uniquely excluded to the results.
	tosend := make([]string, 0)
	ei = 0
	for _, item := range items {
		for ei < len(exclude) && lessthan(exclude[ei], item) {
			ei++
		}
		match := ei < len(exclude) && strings.HasPrefix(item, exclude[ei])
		if !match || nmatches[ei] > 1 {
			tosend = append(tosend, item)
		}
	}
	return tosend
}

func getAddressUtxos(arg *walletrpc.GetAddressUtxosArg, f func(*walletrpc.GetAddressUtxosReply) error) error {
	for _, a := range arg.Addresses {
		if err := checkTaddress(a); err != nil {
			return err
		}
	}
	addrList := &common.ZcashdRpcRequestGetaddressutxos{
		Addresses: arg.Addresses,
	}
	param, err := json.Marshal(addrList)
	if err != nil {
		return status.Errorf(codes.Unknown,
			"getAddressUtxos: failed to marshal addrList, error: %s", err.Error())
	}
	params := []json.RawMessage{param}
	result, rpcErr := common.RawRequest("getaddressutxos", params)
	if rpcErr != nil {
		var code codes.Code
		switch {
		case strings.Contains(rpcErr.Error(), "Invalid address"):
			code = codes.InvalidArgument
		case strings.Contains(rpcErr.Error(), "No information available"):
			code = codes.NotFound
		}
		return status.Errorf(code,
			"getAddressUtxos: getaddressutxos error: %s", rpcErr.Error())
	}
	var utxosReply []common.ZcashdRpcReplyGetaddressutxos
	err = json.Unmarshal(result, &utxosReply)
	if err != nil {
		return status.Errorf(codes.InvalidArgument,
			"getAddressUtxos: failed to unmarshal getaddressutxos reply, error: %s", err.Error())
	}
	n := 0
	for _, utxo := range utxosReply {
		if uint64(utxo.Height) < arg.StartHeight {
			continue
		}
		n++
		if arg.MaxEntries > 0 && uint32(n) > arg.MaxEntries {
			break
		}
		txidBigEndian, err := hex.DecodeString(utxo.Txid)
		if err != nil {
			return status.Errorf(codes.Internal,
				"getAddressUtxos: failed decode txid, error: %s", err.Error())
		}
		scriptBytes, err := hex.DecodeString(utxo.Script)
		if err != nil {
			return status.Errorf(codes.Internal,
				"getAddressUtxos: failed decode utxo script, error: %s", err.Error())
		}
		// When expressed as bytes, a txid must be little-endian.
		err = f(&walletrpc.GetAddressUtxosReply{
			Address:  utxo.Address,
			Txid:     hash32.ReverseSlice(txidBigEndian),
			Index:    int32(utxo.OutputIndex),
			Script:   scriptBytes,
			ValueZat: int64(utxo.Satoshis),
			Height:   uint64(utxo.Height),
		})
		if err != nil {
			return err
		}
	}
	return nil
}

func (s *lwdStreamer) GetAddressUtxos(ctx context.Context, arg *walletrpc.GetAddressUtxosArg) (*walletrpc.GetAddressUtxosReplyList, error) {
	common.Log.Debugf("gRPC GetAddressUtxos(%+v)\n", arg)
	addressUtxos := make([]*walletrpc.GetAddressUtxosReply, 0)
	err := getAddressUtxos(arg, func(utxo *walletrpc.GetAddressUtxosReply) error {
		addressUtxos = append(addressUtxos, utxo)
		return nil
	})
	if err != nil {
		return nil, err
	}
	r := &walletrpc.GetAddressUtxosReplyList{AddressUtxos: addressUtxos}
	common.Log.Tracef("  return: %+v\n", r)
	return r, nil
}

func (s *lwdStreamer) GetSubtreeRoots(arg *walletrpc.GetSubtreeRootsArg, resp walletrpc.CompactTxStreamer_GetSubtreeRootsServer) error {
	common.Log.Debugf("gRPC GetSubtreeRoots(%+v)\n", arg)
	if common.DarksideEnabled {
		return common.DarksideGetSubtreeRoots(arg, resp)
	}
	switch arg.ShieldedProtocol {
	case walletrpc.ShieldedProtocol_sapling:
	case walletrpc.ShieldedProtocol_orchard:
		break
	default:
		return errors.New("unrecognized shielded protocol")
	}
	protocol, err := json.Marshal(arg.ShieldedProtocol.String())
	if err != nil {
		return status.Errorf(codes.InvalidArgument,
			"GetSubtreeRoots: bad shielded protocol specifier error: %s", err.Error())
	}
	startIndexJSON, err := json.Marshal(arg.StartIndex)
	if err != nil {
		return status.Errorf(codes.InvalidArgument,
			"GetSubtreeRoots: bad startIndex, error: %s", err.Error())
	}
	params := []json.RawMessage{
		protocol,
		startIndexJSON,
	}
	if arg.MaxEntries > 0 {
		maxEntriesJSON, err := json.Marshal(arg.MaxEntries)
		if err != nil {
			return status.Errorf(codes.InvalidArgument,
				"GetSubtreeRoots: bad maxEntries, error: %s", err.Error())
		}
		params = append(params, maxEntriesJSON)
	}
	result, rpcErr := common.RawRequest("z_getsubtreesbyindex", params)
	if rpcErr != nil {
		return status.Errorf(codes.InvalidArgument,
			"GetSubtreeRoots: z_getsubtreesbyindex, error: %s", rpcErr.Error())
	}
	var reply common.ZcashdRpcReplyGetsubtreebyindex
	err = json.Unmarshal(result, &reply)
	if err != nil {
		return status.Errorf(codes.Unknown,
			"GetSubtreeRoots: failed to unmarshal z_getsubtreesbyindex reply, error: %s", err.Error())
	}
	for i := 0; i < len(reply.Subtrees); i++ {
		subtree := reply.Subtrees[i]
		block, err := common.GetBlock(s.cache, subtree.End_height)
		if block == nil {
			// It may be worth trying to determine a more specific error code
			return status.Error(codes.Internal, err.Error())
		}
		roothash, err := hex.DecodeString(subtree.Root)
		if err != nil {
			return status.Errorf(codes.Internal,
				"GetSubtreeRoots: failed to decode subtree.Root %s, error: %s", subtree.Root, err.Error())
		}
		r := walletrpc.SubtreeRoot{
			RootHash:              roothash,
			CompletingBlockHash:   hash32.ReverseSlice(block.Hash),
			CompletingBlockHeight: block.Height,
		}
		err = resp.Send(&r)
		if err != nil {
			return err
		}
	}
	return nil // success
}

func (s *lwdStreamer) GetAddressUtxosStream(arg *walletrpc.GetAddressUtxosArg, resp walletrpc.CompactTxStreamer_GetAddressUtxosStreamServer) error {
	common.Log.Debugf("gRPC GetAddressUtxosStream(%+v)\n", arg)
	err := getAddressUtxos(arg, func(utxo *walletrpc.GetAddressUtxosReply) error {
		return resp.Send(utxo)
	})
	if err != nil {
		return err
	}
	return nil
}

// This rpc is used only for testing.
var concurrent int64

func (s *lwdStreamer) Ping(ctx context.Context, in *walletrpc.Duration) (*walletrpc.PingResponse, error) {
	// This gRPC allows the client to create an arbitrary number of
	// concurrent threads, which could run the server out of resources,
	// so only allow if explicitly enabled.
	if !s.pingEnable {
		return nil, status.Errorf(codes.FailedPrecondition,
			"Ping not enabled, start lightwalletd with --ping-very-insecure")
	}
	var response walletrpc.PingResponse
	response.Entry = atomic.AddInt64(&concurrent, 1)
	time.Sleep(time.Duration(in.IntervalUs) * time.Microsecond)
	response.Exit = atomic.AddInt64(&concurrent, -1)
	return &response, nil
}

// SetMetaState lets the test driver control some GetLightdInfo values.
func (s *DarksideStreamer) Reset(ctx context.Context, ms *walletrpc.DarksideMetaState) (*walletrpc.Empty, error) {
	match, err := regexp.Match("\\A[a-fA-F0-9]+\\z", []byte(ms.BranchID))
	if err != nil || !match {
		return nil, status.Errorf(codes.InvalidArgument,
			"Reset: invalid BranchID (must be hex): %s", ms.BranchID)
	}

	match, err = regexp.Match("\\A[a-zA-Z0-9]+\\z", []byte(ms.ChainName))
	if err != nil || !match {
		return nil, errors.New("invalid chain name")
	}
	err = common.DarksideReset(
		int(ms.SaplingActivation),
		ms.BranchID,
		ms.ChainName,
		ms.StartSaplingCommitmentTreeSize,
		ms.StartOrchardCommitmentTreeSize,
	)
	if err != nil {
		common.Log.Fatal("Reset failed, error: ", err.Error())
	}
	mempoolMap = nil
	mempoolList = nil
	return &walletrpc.Empty{}, nil
}

func (s *DarksideStreamer) Stop(ctx context.Context, _ *walletrpc.Empty) (*walletrpc.Empty, error) {
	common.Log.Info("Stopping by gRPC request")
	go func() {
		// minor improvement: let the successful reply gets back to the client
		// (otherwise it looks like the Stop request failed when it didn't)
		common.Time.Sleep(1 * time.Second)
		os.Exit(0)
	}()
	return &walletrpc.Empty{}, nil
}

// StageBlocksStream accepts a list of blocks from the wallet test code,
// and makes them available to present from the mock zcashd's GetBlock rpc.
func (s *DarksideStreamer) StageBlocksStream(blocks walletrpc.DarksideStreamer_StageBlocksStreamServer) error {
	for {
		b, err := blocks.Recv()
		if err == io.EOF {
			blocks.SendAndClose(&walletrpc.Empty{})
			return nil
		}
		if err != nil {
			return err
		}
		common.DarksideStageBlockStream(b.Block)
	}
}

// StageBlocks loads blocks from the given URL to the staging area.
func (s *DarksideStreamer) StageBlocks(ctx context.Context, u *walletrpc.DarksideBlocksURL) (*walletrpc.Empty, error) {
	if err := common.DarksideStageBlocks(u.Url); err != nil {
		return nil, status.Errorf(codes.Unknown,
			"StageBlocks: DarksideStageBlocks failed, error: %s", err.Error())
	}
	return &walletrpc.Empty{}, nil
}

// StageBlocksCreate stages a set of synthetic (manufactured on the fly) blocks.
func (s *DarksideStreamer) StageBlocksCreate(ctx context.Context, e *walletrpc.DarksideEmptyBlocks) (*walletrpc.Empty, error) {
	if err := common.DarksideStageBlocksCreate(e.Height, e.Nonce, e.Count); err != nil {
		return nil, status.Errorf(codes.Unknown,
			"StageBlocksCreate: DarksideStageBlocksCreate failed, error: %s", err.Error())
	}
	return &walletrpc.Empty{}, nil
}

// StageTransactionsStream adds the given transactions to the staging area.
func (s *DarksideStreamer) StageTransactionsStream(tx walletrpc.DarksideStreamer_StageTransactionsStreamServer) error {
	// My current thinking is that this should take a JSON array of {height, txid}, store them,
	// then DarksideAddBlock() would "inject" transactions into blocks as its storing
	// them (remembering to update the header so the block hash changes).
	for {
		transaction, err := tx.Recv()
		if err == io.EOF {
			tx.SendAndClose(&walletrpc.Empty{})
			return nil
		}
		if err != nil {
			return status.Errorf(codes.Unknown,
				"StageTransactionsStream: Recv failed, error: %s", err.Error())
		}
		err = common.DarksideStageTransaction(int(transaction.Height), transaction.Data)
		if err != nil {
			return status.Errorf(codes.Unknown,
				"StageTransactionsStream: DarksideStageTransaction failed, error: %s", err.Error())
		}
	}
}

// StageTransactions loads blocks from the given URL to the staging area.
func (s *DarksideStreamer) StageTransactions(ctx context.Context, u *walletrpc.DarksideTransactionsURL) (*walletrpc.Empty, error) {
	if err := common.DarksideStageTransactionsURL(int(u.Height), u.Url); err != nil {
		return nil, status.Errorf(codes.Unknown,
			"StageTransactions: DarksideStageTransactionsURL failed, error: %s", err.Error())
	}
	return &walletrpc.Empty{}, nil
}

// ApplyStaged merges all staged transactions into staged blocks and all staged blocks into the active blockchain.
func (s *DarksideStreamer) ApplyStaged(ctx context.Context, h *walletrpc.DarksideHeight) (*walletrpc.Empty, error) {
	return &walletrpc.Empty{}, common.DarksideApplyStaged(int(h.Height))
}

// GetIncomingTransactions returns the transactions that were submitted via SendTransaction().
func (s *DarksideStreamer) GetIncomingTransactions(in *walletrpc.Empty, resp walletrpc.DarksideStreamer_GetIncomingTransactionsServer) error {
	// Get all of the incoming transactions we're received via SendTransaction()
	for _, txBytes := range common.DarksideGetIncomingTransactions() {
		err := resp.Send(&walletrpc.RawTransaction{Data: txBytes, Height: 0})
		if err != nil {
			return err
		}
	}
	return nil
}

// ClearIncomingTransactions empties the incoming transaction list.
func (s *DarksideStreamer) ClearIncomingTransactions(ctx context.Context, e *walletrpc.Empty) (*walletrpc.Empty, error) {
	common.DarksideClearIncomingTransactions()
	return &walletrpc.Empty{}, nil
}

// AddAddressUtxo adds a UTXO which will be returned by GetAddressUtxos() (above)
func (s *DarksideStreamer) AddAddressUtxo(ctx context.Context, arg *walletrpc.GetAddressUtxosReply) (*walletrpc.Empty, error) {
	utxosReply := common.ZcashdRpcReplyGetaddressutxos{
		Address:     arg.Address,
		Txid:        hash32.Encode(hash32.Reverse(hash32.FromSlice(arg.Txid))),
		OutputIndex: int64(arg.Index),
		Script:      hex.EncodeToString(arg.Script),
		Satoshis:    uint64(arg.ValueZat),
		Height:      int(arg.Height),
	}
	err := common.DarksideAddAddressUtxo(utxosReply)
	if err != nil {
		return nil, status.Errorf(codes.Unknown,
			"AddAddressUtxo: DarksideAddAddressUtxo failed, error: %s", err.Error())
	}
	return &walletrpc.Empty{}, nil
}

// ClearAddressUtxo removes the list of cached utxo entries
func (s *DarksideStreamer) ClearAddressUtxo(ctx context.Context, arg *walletrpc.Empty) (*walletrpc.Empty, error) {
	err := common.DarksideClearAddressUtxos()
	return &walletrpc.Empty{}, err
}

// Adds a tree state to the cached tree states
func (s *DarksideStreamer) AddTreeState(ctx context.Context, arg *walletrpc.TreeState) (*walletrpc.Empty, error) {
	tree := common.DarksideTreeState{
		Network:     arg.Network,
		Height:      arg.Height,
		Hash:        arg.Hash,
		Time:        arg.Time,
		SaplingTree: arg.SaplingTree,
		OrchardTree: arg.OrchardTree,
	}
	err := common.DarksideAddTreeState(tree)

	return &walletrpc.Empty{}, err
}

// removes a TreeState from the cache if present
func (s *DarksideStreamer) RemoveTreeState(ctx context.Context, arg *walletrpc.BlockID) (*walletrpc.Empty, error) {
	err := common.DarksideRemoveTreeState(arg)
	if err != nil {
		return nil, status.Errorf(codes.Unknown,
			"RemoveTreeState: DarksideRemoveTreeState failed, error: %s", err.Error())
	}
	return &walletrpc.Empty{}, err
}

// Clears all the TreeStates present in the cache.
func (s *DarksideStreamer) ClearAllTreeStates(ctx context.Context, arg *walletrpc.Empty) (*walletrpc.Empty, error) {
	err := common.DarksideClearAllTreeStates()
	if err != nil {
		return nil, status.Errorf(codes.Unknown,
			"ClearAllTreeStates: DarksideClearAllTreeStates failed, error: %s", err.Error())
	}
	return &walletrpc.Empty{}, err
}

func (s *DarksideStreamer) SetSubtreeRoots(ctx context.Context, arg *walletrpc.DarksideSubtreeRoots) (*walletrpc.Empty, error) {
	err := common.DarksideSetSubtreeRoots(arg)
	if err != nil {
		return nil, status.Errorf(codes.Unknown,
			"SetSubtreeRoots: DarksideSetSubtreeRoots failed, error: %s", err.Error())
	}
	return &walletrpc.Empty{}, err
}
