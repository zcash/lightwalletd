package main

import (
	"database/sql"
	"encoding/binary"
	"encoding/hex"
	"flag"
	"fmt"
	"os"

	"github.com/golang/protobuf/proto"
	_ "github.com/mattn/go-sqlite3"
	zmq "github.com/pebbe/zmq4"
	"github.com/sirupsen/logrus"

	"github.com/gtank/ctxd/parser"
	"github.com/gtank/ctxd/storage"
)

var log *logrus.Entry
var logger = logrus.New()
var db *sql.DB

type Options struct {
	zmqAddr  string
	dbPath   string
	logLevel uint64
	logPath  string
}

func main() {
	opts := &Options{}
	flag.StringVar(&opts.zmqAddr, "zmq-addr", "127.0.0.1:28332", "the address of the 0MQ publisher")
	flag.StringVar(&opts.dbPath, "db-path", "", "the path to a sqlite database file")
	flag.Uint64Var(&opts.logLevel, "log-level", uint64(logrus.InfoLevel), "log level (logrus 1-7)")
	flag.StringVar(&opts.logPath, "log-file", "", "log file to write to")
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
	db, err := sql.Open("sqlite3", opts.dbPath)
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

	// Initialize ZMQ
	ctx, err := zmq.NewContext()
	if err != nil {
		log.WithFields(logrus.Fields{
			"error": err,
		}).Fatal("couldn't create zmq context")
	}
	defer ctx.Term()

	// WARNING: The Socket is not thread safe. This means that you cannot
	// access the same Socket from different goroutines without using something
	// like a mutex.
	sock, err := ctx.NewSocket(zmq.SUB)
	if err != nil {
		log.WithFields(logrus.Fields{
			"error": err,
		}).Fatal("couldn't create zmq context socket")
	}

	err = sock.SetSubscribe("rawblock")
	if err != nil {
		log.WithFields(logrus.Fields{
			"error":  err,
			"stream": "rawblock",
		}).Fatal("couldn't subscribe to stream")
	}

	connString := fmt.Sprintf("tcp://%s", opts.zmqAddr)
	err = sock.Connect(connString)
	if err != nil {
		log.WithFields(logrus.Fields{
			"error":      err,
			"connection": connString,
		}).Fatal("couldn't connect to socket")
	}
	defer sock.Close()

	log.Printf("Listening to 0mq on %s", opts.zmqAddr)

	// Start listening for new blocks
	for {
		msg, err := sock.RecvMessageBytes(0)
		if err != nil {
			log.WithFields(logrus.Fields{
				"error": err,
			}).Error("error on msg recv")
			continue
		}

		if len(msg) < 3 {
			log.WithFields(logrus.Fields{
				"msg": msg,
			}).Warn("got unknown message type")
			continue
		}

		topic, body := msg[0], msg[1]

		var sequence int
		if len(msg[2]) == 4 {
			sequence = int(binary.LittleEndian.Uint32(msg[len(msg)-1]))
		}

		switch string(topic) {

		case "rawblock":
			log.WithFields(logrus.Fields{
				"seqnum": sequence,
				"header": fmt.Sprintf("%x", body[:80]),
			}).Debug("got block")

			// there's an implicit mutex here
			go handleBlock(db, sequence, body)

		default:
			log.WithFields(logrus.Fields{
				"seqnum": sequence,
				"topic":  topic,
			}).Warn("got message with unknown topic")
		}
	}
}

func handleBlock(db *sql.DB, sequence int, blockData []byte) {
	block := parser.NewBlock()
	rest, err := block.ParseFromSlice(blockData)
	if err != nil {
		log.WithFields(logrus.Fields{
			"seqnum": sequence,
			"error":  err,
		}).Error("error parsing block")
		return
	}
	if len(rest) != 0 {
		log.WithFields(logrus.Fields{
			"seqnum": sequence,
			"length": len(rest),
		}).Warn("received overlong message")
		return
	}

	blockHash := hex.EncodeToString(block.GetEncodableHash())
	marshaledBlock, _ := proto.Marshal(block.ToCompact())

	err = storage.StoreBlock(
		db,
		block.GetHeight(),
		blockHash,
		block.HasSaplingTransactions(),
		marshaledBlock,
	)

	entry := log.WithFields(logrus.Fields{
		"seqnum":        sequence,
		"block_height":  block.GetHeight(),
		"block_hash":    blockHash,
		"block_version": block.GetVersion(),
		"tx_count":      block.GetTxCount(),
		"has_sapling":   block.HasSaplingTransactions(),
		"error":         err,
	})

	if err != nil {
		entry.Error("error storing block")
	} else {
		entry.Info("received new block")
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
			"block_hash":   blockHash,
			"tx_index":     index,
			"has_sapling":  tx.HasSaplingTransactions(),
			"error":        err,
		})
		if err != nil {
			entry.Error("error storing tx")
		} else {
			entry.Debug("storing tx")
		}
	}
}
