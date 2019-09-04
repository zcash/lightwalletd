package main

import (
	"context"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"
	"strconv"
	"strings"

	"github.com/pkg/errors"
	"github.com/btcsuite/btcd/rpcclient"
	"github.com/golang/protobuf/proto"
	"github.com/sirupsen/logrus"

	"github.com/samosudov/lightwalletd/frontend"
	"github.com/samosudov/lightwalletd/parser"
	"github.com/samosudov/lightwalletd/storage"
)

var log *logrus.Entry
var logger = logrus.New()
var db *sql.DB

type Options struct {
	dbPath   string
	logLevel uint64
	logPath  string
	zcashConfPath string
	startHeight uint64
	rpcHost string
	rpcPort string
	rpcLogin string
	rpcPass string
}

func main() {
	opts := &Options{}
	flag.StringVar(&opts.dbPath, "db-path", "", "the path to a sqlite database file")
	flag.Uint64Var(&opts.logLevel, "log-level", uint64(logrus.InfoLevel), "log level (logrus 1-7)")
	flag.StringVar(&opts.logPath, "log-file", "", "log file to write to")
	flag.StringVar(&opts.zcashConfPath, "conf-file", "", "conf file to pull RPC creds from")
	flag.Uint64Var(&opts.startHeight, "start-height", uint64(0), "start sync from this height (> 419200 for mainnet)")
	flag.StringVar(&opts.rpcHost, "rpc-host", "", "RPC host of zcashd")
	flag.StringVar(&opts.rpcPort, "rpc-port", "", "RPC port of zcashd")
	flag.StringVar(&opts.rpcLogin, "rpc-login", "", "RPC login of zcashd")
	flag.StringVar(&opts.rpcPass, "rpc-pass", "", "RPC password of zcashd")


	// TODO prod metrics
	// TODO support config from file and env vars
	flag.Parse()

	if opts.dbPath == "" {
		flag.Usage()
		os.Exit(1)
	}

	// Initialize logging
	logger.SetFormatter(&logrus.TextFormatter{
		//DisableColors: true,
		FullTimestamp:          true,
		DisableLevelTruncation: true,
	})

	if opts.logPath != "" {
		// instead write parsable logs for logstash/splunk/etc
		output, err := os.OpenFile(opts.logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		if err != nil {
			log.WithFields(logrus.Fields{
				"error": err,
				"path":  opts.logPath,
			}).Fatal("couldn't open log file")
		}
		defer output.Close()
		logger.SetOutput(output)
		logger.SetFormatter(&logrus.JSONFormatter{})
	}

	logger.SetLevel(logrus.Level(opts.logLevel))

	log = logger.WithFields(logrus.Fields{
		"app": "zmqclient",
	})

	// Initialize database
	db, err := sql.Open("sqlite3", fmt.Sprintf("file:%s?_busy_timeout=10000&cache=shared", opts.dbPath))
	db.SetMaxOpenConns(1)
	if err != nil {
		log.WithFields(logrus.Fields{
			"db_path": opts.dbPath,
			"error":   err,
		}).Fatal("couldn't open SQL db")
	}

	// Creates our tables if they don't already exist.
	err = storage.CreateTables(db)
	if err != nil {
		log.WithFields(logrus.Fields{
			"error": err,
		}).Fatal("couldn't create SQL tables")
	}

	//Initialize RPC connection with full node zcashd
	rpcClient, err := frontend.NewZRPCFromConf(opts.zcashConfPath)
	if err != nil {
		log.WithFields(logrus.Fields{
			"error": err,
		}).Warn("zcash.conf failed, will try empty credentials for rpc")

		//Default to testnet, but user MUST specify rpcuser and rpcpassword in zcash.conf; no default
		rpcClient, err = frontend.NewZRPCFromCreds(opts.rpcHost+":"+opts.rpcPort, opts.rpcLogin, opts.rpcPass)

		if err != nil {
			log.WithFields(logrus.Fields{
				"error": err,
			}).Fatal("couldn't start rpc connection")
		}
	}

	ctx := context.Background()
	height, err := storage.GetCurrentHeight(ctx, db)
	if err != nil {
		log.WithFields(logrus.Fields{
			"error": err,
  	}).Warn("unable to get current height from local db storage")

	}

	//ingest from Sapling testnet height
	if height < 419200 {
		height = 419200
		log.WithFields(logrus.Fields{
			"error": err,
  	}).Warn("invalid current height read from local db storage")
	}

	if opts.startHeight != 0 {
		height = int(opts.startHeight)
	}

	// Start listening for new blocks
	for {
		block, err := getBlock(rpcClient, height)
		if err != nil {
			log.WithFields(logrus.Fields{
				"height": height,
				"error":  err,
			}).Warn("error with getblock")
			timeout_count++
			if timeout_count == 3 {
				log.WithFields(logrus.Fields{
					"timeouts":  timeout_count,
				}).Warn("unable to issue RPC call to zcashd node 3 times")
				break
			}
		}
		if block != nil {
			handleBlock(db, block)
			height++
			if timeout_count > 0 {
				timeout_count--
			}
			//TODO store block current/prev hash for formal reorg
		} else {
			height -= 4
			//TODO implement blocknotify to minimize polling on corner cases
			time.Sleep(60 * time.Second)
		}
	}
}

func getBlock(rpcClient *rpcclient.Client, height int) (*parser.Block, error) {
	params := make([]json.RawMessage, 2)
	params[0] = json.RawMessage("\"" + strconv.Itoa(height) + "\"")
	params[1] = json.RawMessage("0")
	result, rpcErr := rpcClient.RawRequest("getblock", params)

	var err error
	var errCode int64

	// For some reason, the error responses are not JSON
	if rpcErr != nil {
		errParts := strings.SplitN(rpcErr.Error(), ":", 2)
		errCode, err = strconv.ParseInt(errParts[0], 10, 32)
		//Check to see if we are requesting a height the zcashd doesn't have yet
		if err == nil && errCode == -8 {
			return nil, nil
		}
		return nil, errors.Wrap(rpcErr, "error requesting block")
	}

	var blockDataHex string
	err = json.Unmarshal(result, &blockDataHex)
	if err != nil{
		return nil, errors.Wrap(err, "error reading JSON response")
	}

	blockData, err := hex.DecodeString(blockDataHex)
	if err != nil {
		return nil, errors.Wrap(err, "error decoding getblock output")
	}

	block := parser.NewBlock()
	rest, err := block.ParseFromSlice(blockData)
	if err != nil {
		return nil, errors.Wrap(err, "error parsing block")
	}
	if len(rest) != 0 {
		return nil, errors.New("received overlong message")
	}
	return block, nil
}


func handleBlock(db *sql.DB, block *parser.Block) {
	nodeBlockHash := hex.EncodeToString(block.GetEncodableHash())

	nodeBlockHeight := block.GetHeight()
	localBlockHash, err := storage.GetBlockHash(ctx, s.db, handleBlockHeight)

	// if local block has different hash delete it before store new block
	if localBlockHash != nil && localBlockHash != nodeBlockHash {
		log.WithFields(logrus.Fields{
			"nodeBlockHeight": nodeBlockHeight,
		}).Warn("reorg: local block hash is different, will be changed with new node block")
		deleteError = storage.DeleteBlock(ctx, s.db, nodeBlockHeight)
		if deleteError != nil {
			log.WithFields(logrus.Fields{
				"error": deleteError,
	  	}).Warn("reorg: cant delete block with the same height (local block hash is different)")
		}
	}

  prevBlockHash := hex.EncodeToString(block.GetPrevHash())
	marshaledBlock, _ := proto.Marshal(block.ToCompact())

	err := storage.StoreBlock(
		db,
		block.GetHeight(),
		prevBlockHash,
		blockHash,
		block.HasSaplingTransactions(),
		marshaledBlock,
	)

	entry := log.WithFields(logrus.Fields{
		"block_height":  block.GetHeight(),
		"block_hash":    hex.EncodeToString(block.GetDisplayHash()),
		"prev_hash":     hex.EncodeToString(block.GetDisplayPrevHash()),
		"block_version": block.GetVersion(),
		"tx_count":      block.GetTxCount(),
		"sapling":       block.HasSaplingTransactions(),
		"error":         err,
	})

	if err != nil {
		entry.Error("new block")
	} else {
		entry.Info("new block")
	}

	for index, tx := range block.Transactions() {
		txHash := hex.EncodeToString(tx.GetEncodableHash())
		err = storage.StoreTransaction(
			db,
			block.GetHeight(),
			blockHash,
			index,
			txHash,
			tx.Bytes(),
		)
		entry = log.WithFields(logrus.Fields{
			"block_height": block.GetHeight(),
			"block_hash":   hex.EncodeToString(block.GetDisplayHash()),
			"tx_index":     index,
			"tx_size":      len(tx.Bytes()),
			"sapling":      tx.HasSaplingTransactions(),
			"error":        err,
		})
		if err != nil {
			entry.Error("storing tx")
		} else {
			entry.Debug("storing tx")
		}
	}
}
