package storage

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/golang/protobuf/proto"
	"github.com/gtank/ctxd/rpc"
	"github.com/pkg/errors"
)

var (
	ErrBadRange = errors.New("no blocks in specified range")
)

func CreateTables(conn *sql.DB) error {
	stateTable := `
		CREATE TABLE IF NOT EXISTS state (
			current_height INTEGER,
			timestamp DATETIME DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (current_height) REFERENCES blocks (height)
		);
	`
	_, err := conn.Exec(stateTable)
	if err != nil {
		return err
	}

	blockTable := `
		CREATE TABLE IF NOT EXISTS blocks (
			height INTEGER PRIMARY KEY,
			hash TEXT,
			has_sapling_tx BOOL,
			encoding_version INTEGER,
			compact_encoding BLOB
		);
	`
	_, err = conn.Exec(blockTable)
	return err
}

func GetCurrentHeight(ctx context.Context, db *sql.DB) (int, error) {
	var height int
	query := "SELECT current_height FROM state WHERE rowid = 1"
	err := db.QueryRowContext(ctx, query).Scan(&height)
	return height, err
}

func SetCurrentHeight(conn *sql.DB, height int) error {
	update := "UPDATE state SET current_height=?, timestamp=CURRENT_TIMESTAMP WHERE rowid = 1"
	result, err := conn.Exec(update, height)
	if err != nil {
		return errors.Wrap(err, "updating state row")
	}
	rowCount, err := result.RowsAffected()
	if err != nil {
		return errors.Wrap(err, "checking if state row exists")
	}
	if rowCount == 0 {
		// row does not yet exist
		insert := "INSERT OR IGNORE INTO state (rowid, current_height) VALUES (1, ?)"
		result, err = conn.Exec(insert, height)
		if err != nil {
			return errors.Wrap(err, "on state row insert")
		}
		rowCount, err = result.RowsAffected()
		if err != nil {
			return errors.Wrap(err, "checking if state row exists")
		}
		if rowCount != 1 {
			return errors.New("totally failed to update current height state")
		}
	}
	return nil
}

func GetBlock(ctx context.Context, db *sql.DB, height int) (*rpc.CompactBlock, error) {
	var blockBytes []byte // avoid a copy with *RawBytes
	query := "SELECT compact_encoding from blocks WHERE height = ?"
	err := db.QueryRowContext(ctx, query, height).Scan(&blockBytes)
	if err != nil {
		return nil, err
	}
	compactBlock := &rpc.CompactBlock{}
	err = proto.Unmarshal(blockBytes, compactBlock)
	return compactBlock, err
}

func GetBlockByHash(ctx context.Context, db *sql.DB, hash string) (*rpc.CompactBlock, error) {
	var blockBytes []byte // avoid a copy with *RawBytes
	query := "SELECT compact_encoding from blocks WHERE hash = ?"
	err := db.QueryRowContext(ctx, query, hash).Scan(&blockBytes)
	if err != nil {
		return nil, errors.Wrap(err, fmt.Sprintf("getting block with hash %s", hash))
	}
	compactBlock := &rpc.CompactBlock{}
	err = proto.Unmarshal(blockBytes, compactBlock)
	return compactBlock, err
}

// [start, end]
func GetBlockRange(conn *sql.DB, start, end int) ([]*rpc.CompactBlock, error) {
	// TODO sanity check range bounds
	query := "SELECT compact_encoding from blocks WHERE (height BETWEEN ? AND ?)"
	result, err := conn.Query(query, start, end)
	if err != nil {
		return nil, err
	}
	defer result.Close()

	compactBlocks := make([]*rpc.CompactBlock, 0, (end-start)+1)
	for result.Next() {
		var blockBytes []byte // avoid a copy with *RawBytes
		err = result.Scan(&blockBytes)
		if err != nil {
			return nil, err
		}
		newBlock := &rpc.CompactBlock{}
		err = proto.Unmarshal(blockBytes, newBlock)
		if err != nil {
			return nil, err
		}
		compactBlocks = append(compactBlocks, newBlock)
	}

	err = result.Err()
	if err != nil {
		return nil, err
	}

	if len(compactBlocks) == 0 {
		return nil, ErrBadRange
	}
	return compactBlocks, nil
}

func StoreBlock(conn *sql.DB, height int, hash string, sapling bool, version int, encoded []byte) error {
	insertBlock := "INSERT INTO blocks (height, hash, has_sapling_tx, encoding_version, compact_encoding) values (?, ?, ?, ?, ?)"
	_, err := conn.Exec(insertBlock, height, hash, sapling, version, encoded)
	if err != nil {
		return errors.Wrap(err, fmt.Sprintf("storing compact block %d", height))
	}

	currentHeight, err := GetCurrentHeight(context.Background(), conn)
	if err != nil || height > currentHeight {
		err = SetCurrentHeight(conn, height)
	}
	return err
}
