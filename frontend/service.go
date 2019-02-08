package frontend

import (
	"context"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/btcsuite/btcd/rpcclient"
	"github.com/golang/protobuf/proto"

	// blank import for sqlite driver support
	_ "github.com/mattn/go-sqlite3"

	"github.com/zcash-hackworks/lightwalletd/storage"
	"github.com/zcash-hackworks/lightwalletd/walletrpc"
)

var (
	ErrUnspecified = errors.New("request for unspecified identifier")
)

// the service type
type SqlStreamer struct {
	db     *sql.DB
	client *rpcclient.Client
}

func NewSQLiteStreamer(dbPath string, client *rpcclient.Client) (walletrpc.CompactTxStreamerServer, error) {
	db, err := sql.Open("sqlite3", fmt.Sprintf("file:%s?_busy_timeout=10000&cache=shared", dbPath))
	db.SetMaxOpenConns(1)
	if err != nil {
		return nil, err
	}

	// Creates our tables if they don't already exist.
	err = storage.CreateTables(db)
	if err != nil {
		return nil, err
	}

	return &SqlStreamer{db, client}, nil
}

func (s *SqlStreamer) GracefulStop() error {
	return s.db.Close()
}

func (s *SqlStreamer) GetLatestBlock(ctx context.Context, placeholder *walletrpc.ChainSpec) (*walletrpc.BlockID, error) {
	// the ChainSpec type is an empty placeholder
	height, err := storage.GetCurrentHeight(ctx, s.db)
	if err != nil {
		return nil, err
	}
	// TODO: also return block hashes here
	return &walletrpc.BlockID{Height: uint64(height)}, nil
}

func (s *SqlStreamer) GetBlock(ctx context.Context, id *walletrpc.BlockID) (*walletrpc.CompactBlock, error) {
	if id.Height == 0 && id.Hash == nil {
		return nil, ErrUnspecified
	}

	var blockBytes []byte
	var err error

	// Precedence: a hash is more specific than a height. If we have it, use it first.
	if id.Hash != nil {
		leHashString := hex.EncodeToString(id.Hash)
		blockBytes, err = storage.GetBlockByHash(ctx, s.db, leHashString)
	} else {
		blockBytes, err = storage.GetBlock(ctx, s.db, int(id.Height))
	}

	if err != nil {
		return nil, err
	}

	cBlock := &walletrpc.CompactBlock{}
	err = proto.Unmarshal(blockBytes, cBlock)
	return cBlock, err
}

func (s *SqlStreamer) GetBlockRange(span *walletrpc.BlockRange, resp walletrpc.CompactTxStreamer_GetBlockRangeServer) error {
	blockChan := make(chan []byte)
	errChan := make(chan error)

	// TODO configure or stress-test this timeout
	timeout, cancel := context.WithTimeout(resp.Context(), 1*time.Second)
	defer cancel()
	go storage.GetBlockRange(timeout,
		s.db,
		blockChan,
		errChan,
		int(span.Start.Height),
		int(span.End.Height),
	)

	for {
		select {
		case err := <-errChan:
			// this will also catch context.DeadlineExceeded from the timeout
			return err
		case blockBytes := <-blockChan:
			cBlock := &walletrpc.CompactBlock{}
			err := proto.Unmarshal(blockBytes, cBlock)
			if err != nil {
				return err // TODO really need better logging in this whole service
			}
			err = resp.Send(cBlock)
			if err != nil {
				return err
			}
		}
	}

	return nil
}

func (s *SqlStreamer) GetTransaction(ctx context.Context, txf *walletrpc.TxFilter) (*walletrpc.RawTransaction, error) {
	var txBytes []byte
	var err error

	if txf.Hash != nil {
		leHashString := hex.EncodeToString(txf.Hash)
		txBytes, err = storage.GetTxByHash(ctx, s.db, leHashString)
		if err != nil {
			return nil, err
		}
		return &walletrpc.RawTransaction{Data: txBytes}, nil

	}

	if txf.Block.Hash != nil {
		leHashString := hex.EncodeToString(txf.Hash)
		txBytes, err = storage.GetTxByHashAndIndex(ctx, s.db, leHashString, int(txf.Index))
		if err != nil {
			return nil, err
		}
		return &walletrpc.RawTransaction{Data: txBytes}, nil
	}

	// A totally unset protobuf will attempt to fetch the genesis coinbase tx.
	txBytes, err = storage.GetTxByHeightAndIndex(ctx, s.db, int(txf.Block.Height), int(txf.Index))
	if err != nil {
		return nil, err
	}
	return &walletrpc.RawTransaction{Data: txBytes}, nil
}

// SendTransaction forwards raw transaction bytes to a zcashd instance over JSON-RPC
func (s *SqlStreamer) SendTransaction(ctx context.Context, rawtx *walletrpc.RawTransaction) (*walletrpc.SendResponse, error) {
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
