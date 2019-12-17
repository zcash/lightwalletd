package main

import (
	"context"
	"flag"
	"fmt"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/sirupsen/logrus"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/reflection"

	"github.com/zcash-hackworks/lightwalletd/frontend"
	"github.com/zcash-hackworks/lightwalletd/walletrpc"
)

var log *logrus.Entry
var logger = logrus.New()

func init() {
	logger.SetFormatter(&logrus.TextFormatter{
		//DisableColors:          true,
		FullTimestamp:          true,
		DisableLevelTruncation: true,
	})

	onexit := func() {
		fmt.Printf("Lightwalletd died with a Fatal error. Check logfile for details.\n")
	}

	log = logger.WithFields(logrus.Fields{
		"app": "frontend-grpc",
	})

	logrus.RegisterExitHandler(onexit)
}

// TODO stream logging

func LoggingInterceptor() grpc.ServerOption {
	return grpc.UnaryInterceptor(logInterceptor)
}

func logInterceptor(
	ctx context.Context,
	req interface{},
	info *grpc.UnaryServerInfo,
	handler grpc.UnaryHandler,
) (interface{}, error) {
	reqLog := loggerFromContext(ctx)
	start := time.Now()

	resp, err := handler(ctx, req)

	entry := reqLog.WithFields(logrus.Fields{
		"method":   info.FullMethod,
		"duration": time.Since(start),
		"error":    err,
	})

	if err != nil {
		entry.Error("call failed")
	} else {
		entry.Info("method called")
	}

	return resp, err
}

func loggerFromContext(ctx context.Context) *logrus.Entry {
	// TODO: anonymize the addresses. cryptopan?
	if peerInfo, ok := peer.FromContext(ctx); ok {
		return log.WithFields(logrus.Fields{"peer_addr": peerInfo.Addr})
	}
	return log.WithFields(logrus.Fields{"peer_addr": "unknown"})
}

type Options struct {
	bindAddr      string `json:"bind_address,omitempty"`
	dbPath        string `json:"db_path"`
	tlsCertPath   string `json:"tls_cert_path,omitempty"`
	tlsKeyPath    string `json:"tls_cert_key,omitempty"`
	logLevel      uint64 `json:"log_level,omitempty"`
	logPath       string `json:"log_file,omitempty"`
	zcashConfPath string `json:"zcash_conf,omitempty"`
	veryInsecure  bool   `json:"very_insecure,omitempty"`
	wantVersion   bool
}

func fileExists(filename string) bool {
	info, err := os.Stat(filename)
	if os.IsNotExist(err) {
		return false
	}
	return !info.IsDir()
}

func main() {
	opts := &Options{}
	flag.StringVar(&opts.bindAddr, "bind-addr", "127.0.0.1:9067", "the address to listen on")
	flag.StringVar(&opts.dbPath, "db-path", "./database.sqlite", "the path to a sqlite database file")
	flag.StringVar(&opts.tlsCertPath, "tls-cert", "./cert.pem", "the path to a TLS certificate")
	flag.StringVar(&opts.tlsKeyPath, "tls-key", "./cert.key", "the path to a TLS key file")
	flag.Uint64Var(&opts.logLevel, "log-level", uint64(logrus.InfoLevel), "log level (logrus 1-7)")
	flag.StringVar(&opts.logPath, "log-file", "./server.log", "log file to write to")
	flag.StringVar(&opts.zcashConfPath, "conf-file", "./zcash.conf", "conf file to pull RPC creds from")
	flag.BoolVar(&opts.veryInsecure, "very-insecure", false, "run without the required TLS certificate, only for debugging, DO NOT use in production")
	flag.BoolVar(&opts.wantVersion, "version", false, "version (major.minor.patch)")
	// TODO prod metrics
	// TODO support config from file and env vars
	flag.Parse()

	if opts.wantVersion {
		fmt.Println("lightwalletd ingest version v0.1.0")
		return
	}

	filesThatShouldExist := []string{
		opts.dbPath,
		opts.tlsCertPath,
		opts.tlsKeyPath,
		opts.logPath,
		opts.zcashConfPath,
	}

	for _, filename := range filesThatShouldExist {
		if !fileExists(opts.logPath) {
			os.OpenFile(opts.logPath, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0666)
		}
		if opts.veryInsecure && (filename == opts.tlsCertPath || filename == opts.tlsKeyPath) {
			continue
		}
		if !fileExists(filename) {
			os.Stderr.WriteString(fmt.Sprintf("\n  ** File does not exist: %s\n\n", filename))
			flag.Usage()
			os.Exit(1)
		}
	}

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

	// gRPC initialization
	var server *grpc.Server

	if opts.veryInsecure {
		server = grpc.NewServer(LoggingInterceptor())
	} else {
		transportCreds, err := credentials.NewServerTLSFromFile(opts.tlsCertPath, opts.tlsKeyPath)
		if err != nil {
			log.WithFields(logrus.Fields{
				"cert_file": opts.tlsCertPath,
				"key_path":  opts.tlsKeyPath,
				"error":     err,
			}).Fatal("couldn't load TLS credentials")
		}
		server = grpc.NewServer(grpc.Creds(transportCreds), LoggingInterceptor())
	}

	// Enable reflection for debugging
	if opts.logLevel >= uint64(logrus.WarnLevel) {
		reflection.Register(server)
	}

	// Initialize Zcash RPC client. Right now (Jan 2018) this is only for
	// sending transactions, but in the future it could back a different type
	// of block streamer.

	rpcClient, err := frontend.NewZRPCFromConf(opts.zcashConfPath)
	if err != nil {
		log.WithFields(logrus.Fields{
			"error": err,
		}).Fatal("setting up RPC connection to zcashd")
	}

	// Compact transaction service initialization
	service, err := frontend.NewSQLiteStreamer(opts.dbPath, rpcClient, log)
	if err != nil {
		log.WithFields(logrus.Fields{
			"db_path": opts.dbPath,
			"error":   err,
		}).Fatal("couldn't create SQL backend")
	}
	defer service.(*frontend.SqlStreamer).GracefulStop()

	// Register service
	walletrpc.RegisterCompactTxStreamerServer(server, service)

	// Start listening
	listener, err := net.Listen("tcp", opts.bindAddr)
	if err != nil {
		log.WithFields(logrus.Fields{
			"bind_addr": opts.bindAddr,
			"error":     err,
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

	log.Infof("Starting gRPC server on %s", opts.bindAddr)

	err = server.Serve(listener)
	if err != nil {
		log.WithFields(logrus.Fields{
			"error": err,
		}).Fatal("gRPC server exited")
	}
}
