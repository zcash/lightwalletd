package frontend

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/btcsuite/btcd/rpcclient"

	"github.com/sirupsen/logrus"
	"github.com/zcash-hackworks/lightwalletd/common"
	"github.com/zcash-hackworks/lightwalletd/walletrpc"
)

var (
	ErrUnspecified = errors.New("request for unspecified identifier")
)

// the service type
type LwdStreamer struct {
	cache  *common.BlockCache
	client *rpcclient.Client
	log    *logrus.Entry
}

func NewLwdStreamer(client *rpcclient.Client, cache *common.BlockCache, log *logrus.Entry) (walletrpc.CompactTxStreamerServer, error) {
	return &LwdStreamer{cache, client, log}, nil
}

func (s *LwdStreamer) GetCache() *common.BlockCache {
	return s.cache
}

func (s *LwdStreamer) GetLatestBlock(ctx context.Context, placeholder *walletrpc.ChainSpec) (*walletrpc.BlockID, error) {
	latestBlock := s.cache.GetLatestBlock()

	if latestBlock == -1 {
		return nil, errors.New("Cache is empty. Server is probably not yet ready.")
	}

	// TODO: also return block hashes here
	return &walletrpc.BlockID{Height: uint64(latestBlock)}, nil
}

func (s *LwdStreamer) GetAddressTxids(addressBlockFilter *walletrpc.TransparentAddressBlockFilter, resp walletrpc.CompactTxStreamer_GetAddressTxidsServer) error {
	// Test to make sure Address is a single t address
	match, err := regexp.Match("\\At[a-zA-Z0-9]{34}\\z", []byte(addressBlockFilter.Address))
	if err != nil || !match {
		s.log.Errorf("Unrecognized address: %s", addressBlockFilter.Address)
		return nil
	}

	params := make([]json.RawMessage, 1)
	st := "{\"addresses\": [\"" + addressBlockFilter.Address + "\"]," +
		"\"start\": " + strconv.FormatUint(addressBlockFilter.Range.Start.Height, 10) +
		", \"end\": " + strconv.FormatUint(addressBlockFilter.Range.End.Height, 10) + "}"

	params[0] = json.RawMessage(st)

	result, rpcErr := s.client.RawRequest("getaddresstxids", params)

	// For some reason, the error responses are not JSON
	if rpcErr != nil {
		s.log.Errorf("Got error: %s", rpcErr.Error())
		return nil
	}

	var txids []string
	err = json.Unmarshal(result, &txids)
	if err != nil {
		s.log.Errorf("Got error: %s", err.Error())
		return nil
	}

	timeout, cancel := context.WithTimeout(resp.Context(), 30*time.Second)
	defer cancel()

	for _, txidstr := range txids {
		txid, _ := hex.DecodeString(txidstr)
		// Txid is read as a string, which is in big-endian order. But when converting
		// to bytes, it should be little-endian
		for left, right := 0, len(txid)-1; left < right; left, right = left+1, right-1 {
			txid[left], txid[right] = txid[right], txid[left]
		}
		tx, err := s.GetTransaction(timeout, &walletrpc.TxFilter{Hash: txid})
		if err != nil {
			s.log.Errorf("Got error: %s", err.Error())
			return nil
		}
		resp.Send(tx)
	}
	return nil
}

func (s *LwdStreamer) GetBlock(ctx context.Context, id *walletrpc.BlockID) (*walletrpc.CompactBlock, error) {
	if id.Height == 0 && id.Hash == nil {
		return nil, ErrUnspecified
	}

	// Precedence: a hash is more specific than a height. If we have it, use it first.
	if id.Hash != nil {
		// TODO: Get block by hash
		return nil, errors.New("GetBlock by Hash is not yet implemented")
	}
	cBlock, err := common.GetBlock(s.client, s.cache, int(id.Height))

	if err != nil {
		return nil, err
	}

	return cBlock, err
}

func (s *LwdStreamer) GetBlockRange(span *walletrpc.BlockRange, resp walletrpc.CompactTxStreamer_GetBlockRangeServer) error {
	blockChan := make(chan walletrpc.CompactBlock)
	errChan := make(chan error)

	go common.GetBlockRange(s.client, s.cache, blockChan, errChan, int(span.Start.Height), int(span.End.Height))

	for {
		select {
		case err := <-errChan:
			// this will also catch context.DeadlineExceeded from the timeout
			return err
		case cBlock := <-blockChan:
			err := resp.Send(&cBlock)
			if err != nil {
				return err
			}
		}
	}

	return nil
}

func (s *LwdStreamer) GetTransaction(ctx context.Context, txf *walletrpc.TxFilter) (*walletrpc.RawTransaction, error) {
	var txBytes []byte
	var txHeight float64

	if txf.Hash != nil {
		txid := txf.Hash
		for left, right := 0, len(txid)-1; left < right; left, right = left+1, right-1 {
			txid[left], txid[right] = txid[right], txid[left]
		}
		leHashString := hex.EncodeToString(txid)

		// First call to get the raw transaction bytes
		params := make([]json.RawMessage, 1)
		params[0] = json.RawMessage("\"" + leHashString + "\"")

		result, rpcErr := s.client.RawRequest("getrawtransaction", params)

		var err error
		// For some reason, the error responses are not JSON
		if rpcErr != nil {
			s.log.Errorf("Got error: %s", rpcErr.Error())
			errParts := strings.SplitN(rpcErr.Error(), ":", 2)
			_, err = strconv.ParseInt(errParts[0], 10, 32)
			return nil, err
		}
		var txhex string
		err = json.Unmarshal(result, &txhex)
		if err != nil {
			return nil, err
		}

		txBytes, err = hex.DecodeString(txhex)
		if err != nil {
			return nil, err
		}
		// Second call to get height
		params = make([]json.RawMessage, 2)
		params[0] = json.RawMessage("\"" + leHashString + "\"")
		params[1] = json.RawMessage("1")

		result, rpcErr = s.client.RawRequest("getrawtransaction", params)

		// For some reason, the error responses are not JSON
		if rpcErr != nil {
			s.log.Errorf("Got error: %s", rpcErr.Error())
			errParts := strings.SplitN(rpcErr.Error(), ":", 2)
			_, err = strconv.ParseInt(errParts[0], 10, 32)
			return nil, err
		}
		var txinfo interface{}
		err = json.Unmarshal(result, &txinfo)
		if err != nil {
			return nil, err
		}
		txHeight = txinfo.(map[string]interface{})["height"].(float64)
		return &walletrpc.RawTransaction{Data: txBytes, Height: uint64(txHeight)}, nil
	}

	if txf.Block != nil && txf.Block.Hash != nil {
		s.log.Error("Can't GetTransaction with a blockhash+num. Please call GetTransaction with txid")
		return nil, errors.New("Can't GetTransaction with a blockhash+num. Please call GetTransaction with txid")
	}
	s.log.Error("Please call GetTransaction with txid")
	return nil, errors.New("Please call GetTransaction with txid")
}

// GetLightdInfo gets the LightWalletD (this server) info
func (s *LwdStreamer) GetLightdInfo(ctx context.Context, in *walletrpc.Empty) (*walletrpc.LightdInfo, error) {
	saplingHeight, blockHeight, chainName, consensusBranchId := common.GetSaplingInfo(s.client, s.log)

	// TODO these are called Error but they aren't at the moment.
	// A success will return code 0 and message txhash.
	return &walletrpc.LightdInfo{
		Version:                 "0.2.1",
		Vendor:                  "ECC LightWalletD",
		TaddrSupport:            true,
		ChainName:               chainName,
		SaplingActivationHeight: uint64(saplingHeight),
		ConsensusBranchId:       consensusBranchId,
		BlockHeight:             uint64(blockHeight),
	}, nil
}

// SendTransaction forwards raw transaction bytes to a zcashd instance over JSON-RPC
func (s *LwdStreamer) SendTransaction(ctx context.Context, rawtx *walletrpc.RawTransaction) (*walletrpc.SendResponse, error) {
	// sendrawtransaction "hexstring" ( allowhighfees )
	//
	// Submits raw transaction (serialized, hex-encoded) to local node and network.
	//
	// Also see createrawtransaction and signrawtransaction calls.
	//
	// Arguments:
	// 1. "hexstring"    (string, required) The hex string of the raw transaction)
	// 2. allowhighfees    (boolean, optional, default=false) Allow high fees
	//
	// Result:
	// "hex"             (string) The transaction hash in hex

	// Construct raw JSON-RPC params
	params := make([]json.RawMessage, 1)
	txHexString := hex.EncodeToString(rawtx.Data)
	params[0] = json.RawMessage("\"" + txHexString + "\"")
	result, rpcErr := s.client.RawRequest("sendrawtransaction", params)

	var err error
	var errCode int64
	var errMsg string

	// For some reason, the error responses are not JSON
	if rpcErr != nil {
		errParts := strings.SplitN(rpcErr.Error(), ":", 2)
		errMsg = strings.TrimSpace(errParts[1])
		errCode, err = strconv.ParseInt(errParts[0], 10, 32)
		if err != nil {
			// This should never happen. We can't panic here, but it's that class of error.
			// This is why we need integration testing to work better than regtest currently does. TODO.
			return nil, errors.New("SendTransaction couldn't parse error code")
		}
	} else {
		errMsg = string(result)
	}

	// TODO these are called Error but they aren't at the moment.
	// A success will return code 0 and message txhash.
	return &walletrpc.SendResponse{
		ErrorCode:    int32(errCode),
		ErrorMessage: errMsg,
	}, nil
}
