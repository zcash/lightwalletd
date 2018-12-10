package main

import (
	"database/sql"
	"errors"
	"flag"
	"net"
	"os"
	"os/signal"
	"syscall"

	"github.com/gtank/ctxd/rpc"
	"github.com/sirupsen/logrus"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

var (
	log = logrus.New()

	ErrNoImpl = errors.New("not yet implemented")
)

func init() {
	log.SetFormatter(&logrus.TextFormatter{
		//DisableColors: true,
		FullTimestamp:          true,
		DisableLevelTruncation: true,
	})
}

type Options struct {
	bindAddr    string `json:"bind_address,omitempty"`
	dbPath      string `json:"db_path"`
	tlsCertPath string `json:"tls_cert_path,omitempty"`
	tlsKeyPath  string `json:"tls_cert_key,omitempty"`
	logLevel    uint64 `json:"log_level,omitempty"`
	logPath     string `json:"log_file,omitempty"`
}

func main() {
	opts := &Options{}
	flag.StringVar(&opts.bindAddr, "bind-addr", "127.0.0.1:9067", "the address to listen on")
	flag.StringVar(&opts.dbPath, "db-path", "", "the location of a sqlite database file")
	flag.StringVar(&opts.tlsCertPath, "tls-cert", "", "the path to a TLS certificate (optional)")
	flag.StringVar(&opts.tlsKeyPath, "tls-key", "", "the path to a TLS key file (optional)")
	flag.Uint64Var(&opts.logLevel, "log-level", uint64(logrus.InfoLevel), "log level (logrus 1-7)")
	// TODO prod logging flag.StringVar(&opts.logPath, "log-file", "", "log file to write to")
	// TODO prod metrics
	// TODO support config from file
	flag.Parse()

	if opts.dbPath == "" {
		flag.Usage()
		os.Exit(1)
	}

	log.SetLevel(logrus.Level(opts.logLevel))

	// gRPC initialization
	var server *grpc.Server

	if opts.tlsCertPath != "" && opts.tlsKeyPath != "" {
		transportCreds, err := credentials.NewServerTLSFromFile(opts.tlsCertPath, opts.tlsKeyPath)
		if err != nil {
			log.WithFields(logrus.Fields{
				"cert_file": opts.tlsCertPath,
				"key_path":  opts.tlsKeyPath,
				"error":     err.Error(),
			}).Fatal("couldn't load TLS credentials")
		}
		server = grpc.NewServer(grpc.Creds(transportCreds))
	} else {
		server = grpc.NewServer()
	}

	// Compact transaction service initialization
	service, err := NewSQLiteStreamer(opts.dbPath)
	if err != nil {
		log.WithFields(logrus.Fields{
			"db_path": opts.dbPath,
			"error":   err.Error(),
		}).Fatal("couldn't create SQL streamer")
	}

	// Register service
	rpc.RegisterCompactTxStreamerServer(server, service)

	// Start listening
	listener, err := net.Listen("tcp", opts.bindAddr)
	if err != nil {
		log.WithFields(logrus.Fields{
			"bind_addr": opts.bindAddr,
			"error":     err.Error(),
		}).Fatal("couldn't create listener")
	}

	// Signal handler for graceful stops
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		s := <-signals
		log.WithFields(logrus.Fields{
			"signal": s.String(),
		}).Info("caught signal, stopping gRPC server")
		server.GracefulStop()
	}()

	err = server.Serve(listener)
	if err != nil {
		log.WithFields(logrus.Fields{
			"error": err.Error(),
		}).Fatal("gRPC server failed")
	}
}

// the service type
type sqlStreamer struct {
	db *sql.DB
}

func NewSQLiteStreamer(dbPath string) (rpc.CompactTxStreamerServer, error) {
	return nil, ErrNoImpl
}
