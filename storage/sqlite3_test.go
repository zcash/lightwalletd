package storage

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"strings"
	"testing"
	"time"

	"github.com/golang/protobuf/proto"
	_ "github.com/mattn/go-sqlite3"
	"github.com/pkg/errors"

	"github.com/zcash-hackworks/lightwalletd/parser"
	"github.com/zcash-hackworks/lightwalletd/walletrpc"
)

type compactTest struct {
	BlockHeight int    `json:"block"`
	BlockHash   string `json:"hash"`
	PrevHash    string `json:"prev"`
	Full        string `json:"full"`
	Compact     string `json:"compact"`
}

var compactTests []compactTest

func TestSqliteStorage(t *testing.T) {
	blockJSON, err := ioutil.ReadFile("../testdata/compact_blocks.json")
	if err != nil {
		t.Fatal(err)
	}

	err = json.Unmarshal(blockJSON, &compactTests)
	if err != nil {
		t.Fatal(err)
	}

	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	ctx := context.Background()

	// Fill tables
	{
		err = CreateTables(db)
		if err != nil {
			t.Fatal(err)
		}

		for _, test := range compactTests {
			blockData, _ := hex.DecodeString(test.Full)
			block := parser.NewBlock()
			blockData, err = block.ParseFromSlice(blockData)
			if err != nil {
				t.Error(errors.Wrap(err, fmt.Sprintf("parsing testnet block %d", test.BlockHeight)))
				continue
			}

			height := block.GetHeight()
			hash := hex.EncodeToString(block.GetEncodableHash())
			hasSapling := block.HasSaplingTransactions()
			protoBlock := block.ToCompact()
			marshaled, _ := proto.Marshal(protoBlock)

			err = StoreBlock(db, height, test.PrevHash, hash, hasSapling, marshaled)
			if err != nil {
				t.Error(err)
				continue
			}
			blockLookup, err := GetBlockByHash(ctx, db, hash)
			if err != nil {
				t.Error(errors.Wrap(err, fmt.Sprintf("GetBlockByHash block %d", test.BlockHeight)))
				continue
			}
			if !bytes.Equal(blockLookup, marshaled) {
				t.Errorf("GetBlockByHash unexpected result, block %d", test.BlockHeight)
			}
			// nonexistent hash
			_, err = GetBlockByHash(ctx, db, "4ff234f7b51971cbeb7719a1c32d1c7e1ed92afafed266a7b1ae235717df0501")
			if err == nil {
				t.Fatal(errors.Wrap(err, fmt.Sprintf("GetBlockByHash unexpected success block %d", test.BlockHeight)))
				continue
			}
			if !strings.Contains(err.Error(), "getting block with hash") ||
				!strings.Contains(err.Error(), "no rows in result set") {
				t.Error(errors.Wrap(err, fmt.Sprintf("GetBlockByHash wrong error block %d", test.BlockHeight)))
				continue
			}
		}
	}

	// Count the blocks
	{
		var count int
		countBlocks := "SELECT count(*) FROM blocks"
		err = db.QueryRow(countBlocks).Scan(&count)
		if err != nil {
			t.Error(errors.Wrap(err, fmt.Sprintf("counting compact blocks")))
		}

		if count != len(compactTests) {
			t.Errorf("Wrong row count, want %d got %d", len(compactTests), count)
		}
	}

	// Check height state is as expected
	{
		blockHeight, err := GetCurrentHeight(ctx, db)
		if err != nil {
			t.Error(errors.Wrap(err, fmt.Sprintf("checking current block height")))
		}

		lastBlockTest := compactTests[len(compactTests)-1]

		if blockHeight != lastBlockTest.BlockHeight {
			t.Errorf("Wrong block height, got: %d", blockHeight)
		}

		storedBlock, err := GetBlock(ctx, db, blockHeight)
		if err != nil {
			t.Error(errors.Wrap(err, "retrieving stored block"))
		}
		_, err = GetBlock(ctx, db, blockHeight+1)
		if err == nil {
			t.Fatal(errors.Wrap(err, "GetBlock unexpected success"))
		}
		if !strings.Contains(err.Error(), "getting block with height") ||
			!strings.Contains(err.Error(), "no rows in result set") {
			t.Error(errors.Wrap(err, fmt.Sprintf("GetBlock wrong error string: %s", err.Error())))
		}

		cblock := &walletrpc.CompactBlock{}
		err = proto.Unmarshal(storedBlock, cblock)
		if err != nil {
			t.Fatal(err)
		}

		if int(cblock.Height) != lastBlockTest.BlockHeight {
			t.Error("incorrect retrieval")
		}
	}

	// Block ranges
	{
		blockOut := make(chan []byte)
		errOut := make(chan error)

		count := 0
		go GetBlockRange(ctx, db, blockOut, errOut, 289460, 289465)
	recvLoop0:
		for {
			select {
			case <-blockOut:
				count++
			case err := <-errOut:
				if err != nil {
					t.Error(errors.Wrap(err, "in full blockrange"))
				}
				break recvLoop0
			}
		}

		if count != 6 {
			t.Error("failed to retrieve full range")
		}

		// Test timeout
		timeout, _ := context.WithTimeout(ctx, 0*time.Second)
		go GetBlockRange(timeout, db, blockOut, errOut, 289460, 289465)
	recvLoop1:
		for {
			select {
			case err := <-errOut:
				if err != context.DeadlineExceeded {
					t.Errorf("got the wrong error: %v", err)
				}
				break recvLoop1
			}
		}

		// Test a smaller range
		count = 0
		go GetBlockRange(ctx, db, blockOut, errOut, 289462, 289465)
	recvLoop2:
		for {
			select {
			case <-blockOut:
				count++
			case err := <-errOut:
				if err != nil {
					t.Error(errors.Wrap(err, "in short blockrange"))
				}
				break recvLoop2
			}
		}

		if count != 4 {
			t.Errorf("failed to retrieve the shorter range")
		}

		// Test a nonsense range
		count = 0
		go GetBlockRange(ctx, db, blockOut, errOut, 1, 2)
	recvLoop3:
		for {
			select {
			case <-blockOut:
				count++
			case err := <-errOut:
				if err != nil {
					t.Error(errors.Wrap(err, "in invalid blockrange"))
				}
				break recvLoop3
			}
		}

		if count > 0 {
			t.Errorf("got some blocks that shouldn't be there")
		}

		// Test requesting range that's too large
		count = 0
		go GetBlockRange(timeout, db, blockOut, errOut, 279465, 289465)
	recvLoop4:
		for {
			select {
			case <-blockOut:
				count++
			case err := <-errOut:
				if err != ErrLotsOfBlocks {
					t.Error(errors.Wrap(err, "in too-large blockrange"))
				}
				break recvLoop4
			}
		}

		if count > 0 {
			t.Errorf("got some blocks that shouldn't be there")
		}
	}

	// Transaction storage
	{
		blockData, _ := hex.DecodeString(compactTests[0].Full)
		block := parser.NewBlock()
		_, _ = block.ParseFromSlice(blockData)
		tx := block.Transactions()[0]

		blockHash := hex.EncodeToString(block.GetEncodableHash())
		txHash := hex.EncodeToString(tx.GetEncodableHash())
		err = StoreTransaction(
			db,
			block.GetHeight(),
			blockHash,
			0,
			txHash,
			tx.Bytes(),
		)

		if err != nil {
			t.Error(err)
		}

		var storedBytes []byte
		getTx := "SELECT tx_bytes FROM transactions WHERE tx_hash = ?"
		err = db.QueryRow(getTx, txHash).Scan(&storedBytes)
		if err != nil {
			t.Error(errors.Wrap(err, fmt.Sprintf("error getting a full transaction")))
		}

		if len(storedBytes) != len(tx.Bytes()) {
			t.Errorf("Wrong tx size, want %d got %d", len(tx.Bytes()), storedBytes)
		}
		{
			r, err := GetTxByHash(ctx, db, txHash)
			if err != nil || !bytes.Equal(r, tx.Bytes()) {
				t.Error("GetTxByHash() incorrect return")
			}
			// nonexistent tx hash
			_, err = GetTxByHash(ctx, db, "42")
			if err == nil {
				t.Fatal(errors.Wrap(err, "GetTxByHash unexpected success"))
			}
			if !strings.Contains(err.Error(), "getting tx with hash") ||
				!strings.Contains(err.Error(), "no rows in result set") {
				t.Error(errors.Wrap(err, fmt.Sprintf("GetTxByHash wrong error string: %s", err.Error())))
			}
		}
		{
			r, err := GetTxByHeightAndIndex(ctx, db, block.GetHeight(), 0)
			if err != nil || !bytes.Equal(r, tx.Bytes()) {
				t.Error("GetTxByHeightAndIndex() incorrect return")
			}
			// nonexistent height
			_, err = GetTxByHeightAndIndex(ctx, db, 47, 0)
			if err == nil {
				t.Fatal(errors.Wrap(err, "GetTxByHeightAndIndex unexpected success"))
			}
			if !strings.Contains(err.Error(), "getting tx (") ||
				!strings.Contains(err.Error(), "no rows in result set") {
				t.Error(errors.Wrap(err, fmt.Sprintf("GetTxByHeightAndIndex wrong error string: %s", err.Error())))
			}
			// nonexistent index
			_, err = GetTxByHeightAndIndex(ctx, db, block.GetHeight(), 1)
			if err == nil {
				t.Fatal(errors.Wrap(err, "GetTxByHeightAndIndex unexpected success"))
			}
			if !strings.Contains(err.Error(), "getting tx (") ||
				!strings.Contains(err.Error(), "no rows in result set") {
				t.Error(errors.Wrap(err, fmt.Sprintf("GetTxByHeightAndIndex wrong error string: %s", err.Error())))
			}
		}
		{
			r, err := GetTxByHashAndIndex(ctx, db, blockHash, 0)
			if err != nil || !bytes.Equal(r, tx.Bytes()) {
				t.Error("GetTxByHashAndIndex() incorrect return")
			}
			// nonexistent block hash
			_, err = GetTxByHashAndIndex(ctx, db, "43", 0)
			if err == nil {
				t.Fatal(errors.Wrap(err, "GetTxByHashAndIndex unexpected success"))
			}
			if !strings.Contains(err.Error(), "getting tx (") ||
				!strings.Contains(err.Error(), "no rows in result set") {
				t.Error(errors.Wrap(err, fmt.Sprintf("GetTxByHashAndIndex wrong error string: %s", err.Error())))
			}
			// nonexistent index
			_, err = GetTxByHashAndIndex(ctx, db, blockHash, 1)
			if err == nil {
				t.Fatal(errors.Wrap(err, "GetTxByHashAndIndex unexpected success"))
			}
			if !strings.Contains(err.Error(), "getting tx (") ||
				!strings.Contains(err.Error(), "no rows in result set") {
				t.Error(errors.Wrap(err, fmt.Sprintf("GetTxByHashAndIndex wrong error string: %s", err.Error())))
			}
		}
	}

}
