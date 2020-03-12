// Copyright (c) 2019-2020 The Zcash developers
// Distributed under the MIT software license, see the accompanying
// file COPYING or https://www.opensource.org/licenses/mit-license.php .
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

	"github.com/zcash/lightwalletd/common"
	"github.com/zcash/lightwalletd/frontend"
	"github.com/zcash/lightwalletd/walletrpc"
)

var logger = logrus.New()

func init() {
	logger.SetFormatter(&logrus.TextFormatter{
		//DisableColors:          true,
		FullTimestamp:          true,
		DisableLevelTruncation: true,
	})

	onexit := func() {
		fmt.Println("Lightwalletd died with a Fatal error. Check logfile for details.")
	}

	common.Log = logger.WithFields(logrus.Fields{
		"app": "frontend-grpc",
	})

	logrus.RegisterExitHandler(onexit)
}

// TODO stream logging

func LoggingInterceptor() grpc.ServerOption {
	return grpc.UnaryInterceptor(logInterceptor)
}

func logInterceptor(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
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
		return common.Log.WithFields(logrus.Fields{"peer_addr": peerInfo.Addr})
	}
	return common.Log.WithFields(logrus.Fields{"peer_addr": "unknown"})
}

type Options struct {
	bindAddr      string `json:"bind_address,omitempty"`
	tlsCertPath   string `json:"tls_cert_path,omitempty"`
	tlsKeyPath    string `json:"tls_cert_key,omitempty"`
	logLevel      uint64 `json:"log_level,omitempty"`
	logPath       string `json:"log_file,omitempty"`
	zcashConfPath string `json:"zcash_conf,omitempty"`
	veryInsecure  bool   `json:"very_insecure,omitempty"`
	cacheSize     int    `json:"cache_size,omitempty"`
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
	flag.StringVar(&opts.tlsCertPath, "tls-cert", "", "the path to a TLS certificate")
	flag.StringVar(&opts.tlsKeyPath, "tls-key", "", "the path to a TLS key file")
	flag.Uint64Var(&opts.logLevel, "log-level", uint64(logrus.InfoLevel), "log level (logrus 1-7)")
	flag.StringVar(&opts.logPath, "log-file", "./server.log", "log file to write to")
	flag.StringVar(&opts.zcashConfPath, "conf-file", "./zcash.conf", "conf file to pull RPC creds from")
	flag.BoolVar(&opts.veryInsecure, "no-tls-very-insecure", false, "run without the required TLS certificate, only for debugging, DO NOT use in production")
	flag.BoolVar(&opts.wantVersion, "version", false, "version (major.minor.patch)")
	flag.IntVar(&opts.cacheSize, "cache-size", 80000, "number of blocks to hold in the cache")

	// TODO prod metrics
	// TODO support config from file and env vars
	flag.Parse()

	if opts.wantVersion {
		fmt.Println("lightwalletd version v0.3.0")
		return
	}

	// production (unlike unit tests) use the real sleep function
	common.Sleep = time.Sleep

	if opts.logPath != "" {
		// instead write parsable logs for logstash/splunk/etc
		output, err := os.OpenFile(opts.logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		if err != nil {
			os.Stderr.WriteString(fmt.Sprintf("Cannot open log file %s: %v\n",
				opts.logPath, err))
			os.Exit(1)
		}
		defer output.Close()
		logger.SetOutput(output)
		logger.SetFormatter(&logrus.JSONFormatter{})
	}
	logger.SetLevel(logrus.Level(opts.logLevel))

	filesThatShouldExist := []string{
		opts.zcashConfPath,
	}
	if opts.tlsCertPath != "" {
		filesThatShouldExist = append(filesThatShouldExist, opts.tlsCertPath)
	}
	if opts.tlsKeyPath != "" {
		filesThatShouldExist = append(filesThatShouldExist, opts.tlsKeyPath)
	}

	for _, filename := range filesThatShouldExist {
		if !fileExists(filename) {
			common.Log.WithFields(logrus.Fields{
				"filename": filename,
			}).Error("cannot open required file")
			os.Stderr.WriteString("Cannot open required file: " + filename + "\n")
			os.Exit(1)
		}
	}

	// gRPC initialization
	var server *grpc.Server
	var err error

	if opts.veryInsecure {
		server = grpc.NewServer(LoggingInterceptor())
	} else {
		var transportCreds credentials.TransportCredentials
		if (opts.tlsCertPath == "") && (opts.tlsKeyPath == "") {
			common.Log.Warning("Certificate and key not provided, generating self signed values")
			tlsCert := common.GenerateCerts()
			transportCreds = credentials.NewServerTLSFromCert(tlsCert)
		} else {
			transportCreds, err = credentials.NewServerTLSFromFile(opts.tlsCertPath, opts.tlsKeyPath)
			if err != nil {
				common.Log.WithFields(logrus.Fields{
					"cert_file": opts.tlsCertPath,
					"key_path":  opts.tlsKeyPath,
					"error":     err,
				}).Fatal("couldn't load TLS credentials")
			}
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
		common.Log.WithFields(logrus.Fields{
			"error": err,
		}).Fatal("setting up RPC connection to zcashd")
	}

	// indirect function for test mocking (so unit tests can talk to stub functions)
	common.RawRequest = rpcClient.RawRequest

	// Get the sapling activation height from the RPC
	// (this first RPC also verifies that we can communicate with zcashd)
	saplingHeight, blockHeight, chainName, branchID := common.GetSaplingInfo()
	common.Log.Info("Got sapling height ", saplingHeight, " chain ", chainName, " branchID ", branchID)

	// Initialize the cache
	cache := common.NewBlockCache(opts.cacheSize)

	// Start the block cache importer at cacheSize blocks before current height
	cacheStart := blockHeight - opts.cacheSize
	if cacheStart < saplingHeight {
		cacheStart = saplingHeight
	}

	// The last argument, repetition count, is only nonzero for testing
	go common.BlockIngestor(cache, cacheStart, 0)

	// Compact transaction service initialization
	service, err := frontend.NewLwdStreamer(cache)
	if err != nil {
		common.Log.WithFields(logrus.Fields{
			"error": err,
		}).Fatal("couldn't create backend")
	}

	// Register service
	walletrpc.RegisterCompactTxStreamerServer(server, service)

	// Start listening
	listener, err := net.Listen("tcp", opts.bindAddr)
	if err != nil {
		common.Log.WithFields(logrus.Fields{
			"bind_addr": opts.bindAddr,
			"error":     err,
		}).Fatal("couldn't create listener")
	}

	// Signal handler for graceful stops
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		s := <-signals
		common.Log.WithFields(logrus.Fields{
			"signal": s.String(),
		}).Info("caught signal, stopping gRPC server")
		os.Stderr.WriteString("Caught signal: " + s.String() + "\n")
		os.Exit(1)
	}()

	common.Log.Infof("Starting gRPC server on %s", opts.bindAddr)

	err = server.Serve(listener)
	if err != nil {
		common.Log.WithFields(logrus.Fields{
			"error": err,
		}).Fatal("gRPC server exited")
	}
}
