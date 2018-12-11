package frontend

import (
	"context"
	"database/sql"
	"encoding/hex"
	"errors"

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
	db, err := sql.Open("sqlite3", dbPath)
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

	// Precedence: a hash is more specific than a height. If we have it, use it first.
	if id.Hash != nil {
		leHashString := hex.EncodeToString(id.Hash)
		return storage.GetBlockByHash(ctx, s.db, leHashString)
	}

	// we have a height and not a hash
	if int(id.Height) > 0 {
		return storage.GetBlock(ctx, s.db, int(id.Height))
	}

	return nil, ErrUnspecified
}

func (s *SqlStreamer) GetBlockRange(*rpc.BlockRange, rpc.CompactTxStreamer_GetBlockRangeServer) error {
	return ErrNoImpl
}

func (s *SqlStreamer) GetTransaction(context.Context, *rpc.TxFilter) (*rpc.RawTransaction, error) {
	return nil, ErrNoImpl
}
func (s *SqlStreamer) SendTransaction(context.Context, *rpc.RawTransaction) (*rpc.SendResponse, error) {
	return nil, ErrNoImpl
}
