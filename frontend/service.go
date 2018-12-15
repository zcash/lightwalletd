package frontend

import (
	"context"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/golang/protobuf/proto"
	_ "github.com/mattn/go-sqlite3"

	"github.com/gtank/ctxd/rpc"
	"github.com/gtank/ctxd/storage"
)

var (
	ErrNoImpl      = errors.New("not yet implemented")
	ErrUnspecified = errors.New("request for unspecified identifier")
)

// the service type
type SqlStreamer struct {
	db *sql.DB
}

func NewSQLiteStreamer(dbPath string) (rpc.CompactTxStreamerServer, error) {
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

	return &SqlStreamer{db}, nil
}

func (s *SqlStreamer) GracefulStop() error {
	return s.db.Close()
}

func (s *SqlStreamer) GetLatestBlock(ctx context.Context, placeholder *rpc.ChainSpec) (*rpc.BlockID, error) {
	// the ChainSpec type is an empty placeholder
	height, err := storage.GetCurrentHeight(ctx, s.db)
	if err != nil {
		return nil, err
	}
	// TODO: also return block hashes here
	return &rpc.BlockID{Height: uint64(height)}, nil
}

func (s *SqlStreamer) GetBlock(ctx context.Context, id *rpc.BlockID) (*rpc.CompactBlock, error) {
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

	cBlock := &rpc.CompactBlock{}
	err = proto.Unmarshal(blockBytes, cBlock)
	return cBlock, err
}

func (s *SqlStreamer) GetBlockRange(span *rpc.BlockRange, resp rpc.CompactTxStreamer_GetBlockRangeServer) error {
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
			cBlock := &rpc.CompactBlock{}
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

func (s *SqlStreamer) GetTransaction(ctx context.Context, txf *rpc.TxFilter) (*rpc.RawTransaction, error) {
	var txBytes []byte
	var err error

	if txf.Hash != nil {
		leHashString := hex.EncodeToString(txf.Hash)
		txBytes, err = storage.GetTxByHash(ctx, s.db, leHashString)
		if err != nil {
			return nil, err
		}
		return &rpc.RawTransaction{Data: txBytes}, nil

	}

	if txf.Block.Hash != nil {
		leHashString := hex.EncodeToString(txf.Hash)
		txBytes, err = storage.GetTxByHashAndIndex(ctx, s.db, leHashString, int(txf.Index))
		if err != nil {
			return nil, err
		}
		return &rpc.RawTransaction{Data: txBytes}, nil
	}

	// A totally unset protobuf will attempt to fetch the genesis coinbase tx.
	txBytes, err = storage.GetTxByHeightAndIndex(ctx, s.db, int(txf.Block.Height), int(txf.Index))
	if err != nil {
		return nil, err
	}
	return &rpc.RawTransaction{Data: txBytes}, nil
}

func (s *SqlStreamer) SendTransaction(ctx context.Context, rawtx *rpc.RawTransaction) (*rpc.SendResponse, error) {
	return nil, ErrNoImpl
}
