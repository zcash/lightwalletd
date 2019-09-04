package storage

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/pkg/errors"
)

var (
	ErrLotsOfBlocks = errors.New("requested >10k blocks at once")
)

func CreateTables(conn *sql.DB) error {
	stateTable := `
		CREATE TABLE IF NOT EXISTS state (
			current_height INTEGER,
			timestamp DATETIME DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (current_height) REFERENCES blocks (block_height)
		);
	`
	_, err := conn.Exec(stateTable)
	if err != nil {
		return err
	}

	blockTable := `
		CREATE TABLE IF NOT EXISTS blocks (
			block_height INTEGER PRIMARY KEY,
			prev_hash TEXT,
			block_hash TEXT,
			sapling BOOL,
			compact_encoding BLOB
		);
	`
	_, err = conn.Exec(blockTable)
	if err != nil {
		return err
	}

	txTable := `
		CREATE TABLE IF NOT EXISTS transactions (
			block_height INTEGER,
			block_hash TEXT,
			tx_index INTEGER,
			tx_hash TEXT,
			tx_bytes BLOB,
			FOREIGN KEY (block_height) REFERENCES blocks (block_height),
			FOREIGN KEY (block_hash) REFERENCES blocks (block_hash)
		);
	`
	_, err = conn.Exec(txTable)

	return err
}

// TODO consider max/count queries instead of state table. bit of a coupling assumption though.

func GetCurrentHeight(ctx context.Context, db *sql.DB) (int, error) {
	var height int
	query := "SELECT current_height FROM state WHERE rowid = 1"
	err := db.QueryRowContext(ctx, query).Scan(&height)
	return height, err
}

func GetBlock(ctx context.Context, db *sql.DB, height int) ([]byte, error) {
	var blockBytes []byte // avoid a copy with *RawBytes
	query := "SELECT compact_encoding from blocks WHERE block_height = ?"
	err := db.QueryRowContext(ctx, query, height).Scan(&blockBytes)
	if err != nil {
		return nil, err
	}
	return blockBytes, err
}

func GetBlockByHash(ctx context.Context, db *sql.DB, hash string) ([]byte, error) {
	var blockBytes []byte // avoid a copy with *RawBytes
	query := "SELECT compact_encoding from blocks WHERE block_hash = ?"
	err := db.QueryRowContext(ctx, query, hash).Scan(&blockBytes)
	if err != nil {
		return nil, errors.Wrap(err, fmt.Sprintf("getting block with hash %s", hash))
	}
	return blockBytes, err
}

func GetBlockHash(ctx context.Context, db *sql.DB, height int) (string, error) {
	var blockHash string
	query := "SELECT block_hash from blocks WHERE block_height = ?"
	err := db.QueryRowContext(ctx, query, height).Scan(&blockHash)
	if err != nil {
		return "", err
	}
	return blockHash, err
}

func DeleteBlock(ctx context.Context, db *sql.DB, height int) error {
	query := "DELETE FROM blocks WHERE block_height = ?"
	err := db.ExecContext(ctx, query, height)
	if err != nil {
		return err
	}
	return nil
}

// [start, end] inclusive
func GetBlockRange(ctx context.Context, db *sql.DB, blockOut chan<- []byte, errOut chan<- error, start, end int) {
	// TODO sanity check ranges, this limit, etc
	numBlocks := (end - start) + 1
	if numBlocks > 10000 {
		errOut <- ErrLotsOfBlocks
		return
	}

	query := "SELECT compact_encoding from blocks WHERE (block_height BETWEEN ? AND ?)"
	result, err := db.QueryContext(ctx, query, start, end)
	if err != nil {
		errOut <- err
		return
	}
	defer result.Close()

	// My assumption here is that if the context is cancelled then result.Next() will fail.

	var blockBytes []byte
	for result.Next() {
		err = result.Scan(&blockBytes)
		if err != nil {
			errOut <- err
			return
		}
		blockOut <- blockBytes
	}

	if err := result.Err(); err != nil {
		errOut <- err
		return
	}

	// done
	errOut <- nil
}

func StoreBlock(conn *sql.DB, height int, prev_hash string, hash string, sapling bool, encoded []byte) error {
	insertBlock := "REPLACE INTO blocks (block_height, prev_hash, block_hash, sapling, compact_encoding) values ( ?, ?, ?, ?, ?)"

	tx, err := conn.Begin()
	if err != nil {
		return errors.Wrap(err, fmt.Sprintf("creating db tx %d", height))
	}

	_, err = tx.Exec(insertBlock, height, prev_hash, hash, sapling, encoded)
	if err != nil {
		return errors.Wrap(err, fmt.Sprintf("storing compact block %d", height))
	}

	var currentHeight int
	query := "SELECT current_height FROM state WHERE rowid = 1"
	err = tx.QueryRow(query).Scan(&currentHeight)

	if err != nil || height > currentHeight {
		err = setCurrentHeight(tx, height)
	}

	err = tx.Commit()
	if err != nil {
		return errors.Wrap(err, fmt.Sprintf("committing db tx %d", height))

	}
	return nil
}

func setCurrentHeight(tx *sql.Tx, height int) error {
	update := "UPDATE state SET current_height=?, timestamp=CURRENT_TIMESTAMP WHERE rowid = 1"
	result, err := tx.Exec(update, height)
	if err != nil {
		return errors.Wrap(err, "updating state row")
	}
	rowCount, err := result.RowsAffected()
	if err != nil {
		return errors.Wrap(err, "checking if state row exists after update")
	}
	if rowCount == 0 {
		// row does not yet exist
		insert := "INSERT OR IGNORE INTO state (rowid, current_height) VALUES (1, ?)"
		result, err = tx.Exec(insert, height)
		if err != nil {
			return errors.Wrap(err, "on state row insert")
		}
		rowCount, err = result.RowsAffected()
		if err != nil {
			return errors.Wrap(err, "checking if state row exists after insert")
		}
		if rowCount != 1 {
			return errors.New("totally failed to update current height state")
		}
	}
	return nil
}

func StoreTransaction(db *sql.DB, blockHeight int, blockHash string, txIndex int, txHash string, txBytes []byte) error {
	insertTx := "INSERT INTO transactions (block_height, block_hash, tx_index, tx_hash, tx_bytes) VALUES (?,?,?,?,?)"
	_, err := db.Exec(insertTx, blockHeight, blockHash, txIndex, txHash, txBytes)
	if err != nil {
		return errors.Wrap(err, fmt.Sprintf("storing tx %x", txHash))
	}
	return nil
}

// GetTxByHash retrieves a full transaction by its little-endian hash.
func GetTxByHash(ctx context.Context, db *sql.DB, txHash string) ([]byte, error) {
	var txBytes []byte // avoid a copy with *RawBytes
	query := "SELECT tx_bytes from transactions WHERE tx_hash = ?"
	err := db.QueryRowContext(ctx, query, txHash).Scan(&txBytes)
	if err != nil {
		return nil, errors.Wrap(err, fmt.Sprintf("getting tx with hash %s", txHash))
	}
	return txBytes, nil
}

// GetTxByHeightAndIndex retrieves a full transaction by its parent block height and index
func GetTxByHeightAndIndex(ctx context.Context, db *sql.DB, blockHeight, txIndex int) ([]byte, error) {
	var txBytes []byte // avoid a copy with *RawBytes
	query := "SELECT tx_bytes from transactions WHERE (block_height = ? AND tx_index = ?)"
	err := db.QueryRowContext(ctx, query, blockHeight, txIndex).Scan(&txBytes)
	if err != nil {
		return nil, errors.Wrap(err, fmt.Sprintf("getting tx (%d, %d)", blockHeight, txIndex))
	}
	return txBytes, nil
}

// GetTxByHashAndIndex retrieves a full transaction by its parent block hash and index
func GetTxByHashAndIndex(ctx context.Context, db *sql.DB, blockHash string, txIndex int) ([]byte, error) {
	var txBytes []byte // avoid a copy with *RawBytes
	query := "SELECT tx_bytes from transactions WHERE (block_hash = ? AND tx_index = ?)"
	err := db.QueryRowContext(ctx, query, blockHash, txIndex).Scan(&txBytes)
	if err != nil {
		return nil, errors.Wrap(err, fmt.Sprintf("getting tx (%x, %d)", blockHash, txIndex))
	}
	return txBytes, nil
}
