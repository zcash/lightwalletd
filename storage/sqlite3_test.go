package storage

import (
	"context"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"testing"
	"time"

	"github.com/golang/protobuf/proto"
	_ "github.com/mattn/go-sqlite3"
	"github.com/pkg/errors"

	"github.com/gtank/ctxd/parser"
	"github.com/gtank/ctxd/rpc"
)

type compactTest struct {
	BlockHeight int    `json:"block"`
	BlockHash   string `json:"hash"`
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

			err = StoreBlock(db, height, hash, hasSapling, marshaled)
			if err != nil {
				t.Error(err)
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

	ctx := context.Background()

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

		retBlock, err := GetBlock(ctx, db, blockHeight)
		if err != nil {
			t.Error(errors.Wrap(err, "retrieving stored block"))
		}
		cblock := &rpc.CompactBlock{}
		err = proto.Unmarshal(retBlock, cblock)
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

	}
}
