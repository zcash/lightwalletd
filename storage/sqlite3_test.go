package storage

import (
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"testing"

	protobuf "github.com/golang/protobuf/proto"
	"github.com/gtank/ctxd/parser"
	_ "github.com/mattn/go-sqlite3"
	"github.com/pkg/errors"
)

func TestMain(m *testing.M) {
	conn, err := sql.Open("sqlite3", "testdata/blocks.db")
	if err != nil {
		log.Fatal(err)
	}

	err = createBlockTable(conn)
	if err != nil {
		conn.Close()
		log.Fatal(err)
	}

	ret := m.Run()
	conn.Close()
	//os.Remove("testdata/blocks.db")
	os.Exit(ret)
}

func TestFillDB(t *testing.T) {
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

	conn, err := sql.Open("sqlite3", "testdata/blocks.db")
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	for _, test := range compactTests {
		blockData, _ := hex.DecodeString(test.Full)
		block := parser.NewBlock()
		blockData, err = block.ParseFromSlice(blockData)
		if err != nil {
			t.Error(errors.Wrap(err, fmt.Sprintf("parsing testnet block %d", test.BlockHeight)))
			continue
		}

		height := block.GetHeight()
		hash := hex.EncodeToString(block.GetHash())
		hasSapling := block.HasSaplingTransactions()
		marshaled, _ := protobuf.Marshal(block.ToCompact())
		compactB64 := base64.RawStdEncoding.EncodeToString(marshaled)

		insertBlock := "INSERT INTO blocks (height, hash, has_sapling_tx, compact_encoding) values (?, ?, ?, ?)"
		_, err := conn.Exec(insertBlock, height, hash, hasSapling, compactB64)
		if err != nil {
			t.Error(errors.Wrap(err, fmt.Sprintf("storing compact block %d", height)))
			continue
		}
	}
}
