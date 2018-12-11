package storage

import (
	"context"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"testing"

	protobuf "github.com/golang/protobuf/proto"
	"github.com/gtank/ctxd/parser"
	_ "github.com/mattn/go-sqlite3"
	"github.com/pkg/errors"
)

func TestSqliteStorage(t *testing.T) {
	type compactTest struct {
		BlockHeight int    `json:"block"`
		BlockHash   string `json:"hash"`
		Full        string `json:"full"`
		Compact     string `json:"compact"`
	}
	var compactTests []compactTest

	blockJSON, err := ioutil.ReadFile("testdata/compact_blocks.json")
	if err != nil {
		t.Fatal(err)
	}

	err = json.Unmarshal(blockJSON, &compactTests)
	if err != nil {
		t.Fatal(err)
	}

	conn, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	err = CreateTables(conn)
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
		hash := hex.EncodeToString(block.GetDisplayHash())
		hasSapling := block.HasSaplingTransactions()
		protoBlock := block.ToCompact()
		version := 1
		marshaled, _ := protobuf.Marshal(protoBlock)

		err = StoreBlock(conn, height, hash, hasSapling, version, marshaled)
		if err != nil {
			t.Error(err)
			continue
		}
	}

	var count int
	countBlocks := "SELECT count(*) FROM blocks"
	err = conn.QueryRow(countBlocks).Scan(&count)
	if err != nil {
		t.Error(errors.Wrap(err, fmt.Sprintf("counting compact blocks")))
	}

	if count != len(compactTests) {
		t.Errorf("Wrong row count, want %d got %d", len(compactTests), count)
	}

	blockHeight, err := GetCurrentHeight(context.Background(), conn)
	if err != nil {
		t.Error(errors.Wrap(err, fmt.Sprintf("checking current block height")))
	}

	lastBlockTest := compactTests[len(compactTests)-1]
	if blockHeight != lastBlockTest.BlockHeight {
		t.Errorf("Wrong block height, got: %d", blockHeight)
	}

	retBlock, err := GetBlock(context.Background(), conn, blockHeight)
	if err != nil {
		t.Error(errors.Wrap(err, "retrieving stored block"))
	}

	if int(retBlock.Height) != lastBlockTest.BlockHeight {
		t.Error("incorrect retrieval")
	}

	blockRange, err := GetBlockRange(conn, 289460, 289465)
	if err != nil {
		t.Error(err)
	}
	if len(blockRange) != 6 {
		t.Error("failed to retrieve full range")
	}

	blockRange, err = GetBlockRange(conn, 289462, 289465)
	if err != nil {
		t.Error(err)
	}
	if len(blockRange) != 4 {
		t.Error("failed to retrieve partial range")
	}

	blockRange, err = GetBlockRange(conn, 1337, 1338)
	if err != ErrBadRange {
		t.Error("Somehow retrieved nonexistent blocks!")
	}
}
