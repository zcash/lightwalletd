package main

import (
	"context"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	grpc_middleware "github.com/grpc-ecosystem/go-grpc-middleware"
	grpc_prometheus "github.com/grpc-ecosystem/go-grpc-prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/sirupsen/logrus"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/reflection"

	"github.com/zcash-hackworks/lightwalletd/common"
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
	grpcBindAddr  string `json:"grpc_bind_address,omitempty"`
	httpBindAddr  string `json:"http_bind_address,omitempty"`
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
	flag.StringVar(&opts.grpcBindAddr, "grpc-bind-addr", "127.0.0.1:9067", "the address to listen on for grpc")
	flag.StringVar(&opts.httpBindAddr, "http-bind-addr", "127.0.0.1:9068", "the address to listen on for http")
	flag.StringVar(&opts.tlsCertPath, "tls-cert", "./cert.pem", "the path to a TLS certificate")
	flag.StringVar(&opts.tlsKeyPath, "tls-key", "./cert.key", "the path to a TLS key file")
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
		fmt.Println("lightwalletd version v0.2.0")
		return
	}

	filesThatShouldExist := []string{
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
		server = grpc.NewServer(grpc.UnaryInterceptor(logInterceptor))
	} else {
		transportCreds, err := credentials.NewServerTLSFromFile(opts.tlsCertPath, opts.tlsKeyPath)
		if err != nil {
			log.WithFields(logrus.Fields{
				"cert_file": opts.tlsCertPath,
				"key_path":  opts.tlsKeyPath,
				"error":     err,
			}).Fatal("couldn't load TLS credentials")
		}
		server = grpc.NewServer(
			grpc.Creds(transportCreds),
			grpc.StreamInterceptor(grpc_prometheus.StreamServerInterceptor),
			grpc.StreamInterceptor(grpc_middleware.ChainStreamServer(
				logInterceptor,
				grpc_prometheus.StreamServerInterceptor),
			))
		grpc_prometheus.EnableHandlingTimeHistogram()
		grpc_prometheus.Register(server)
	}

	// Start the HTTP server endpoint
	go func() {
		startHTTPServer(opts)
	}()

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

	// Get the sapling activation height from the RPC
	// (this first RPC also verifies that we can communicate with zcashd)
	saplingHeight, blockHeight, chainName, branchID := common.GetSaplingInfo(rpcClient, log)
	log.Info("Got sapling height ", saplingHeight, " chain ", chainName, " branchID ", branchID)

	// Initialize the cache
	cache := common.NewBlockCache(opts.cacheSize)

	// Start the block cache importer at cacheSize blocks before current height
	cacheStart := blockHeight - opts.cacheSize
	if cacheStart < saplingHeight {
		cacheStart = saplingHeight
	}

	go common.BlockIngestor(rpcClient, cache, log, cacheStart)

	// Compact transaction service initialization
	service, err := frontend.NewLwdStreamer(rpcClient, cache, log)
	if err != nil {
		log.WithFields(logrus.Fields{
			"error": err,
		}).Fatal("couldn't create backend")
	}

	// Register service
	walletrpc.RegisterCompactTxStreamerServer(server, service)

	// Start listening
	listener, err := net.Listen("tcp", opts.grpcBindAddr)
	if err != nil {
		log.WithFields(logrus.Fields{
			"bind_addr": opts.grpcBindAddr,
			"error":     err,
		}).Fatal("couldn't create grpc listener")
	}

	// Signal handler for graceful stops
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		s := <-signals
		log.WithFields(logrus.Fields{
			"signal": s.String(),
		}).Info("caught signal, stopping gRPC server")
		os.Exit(1)
	}()

	log.Infof("Starting gRPC server on %s", opts.grpcBindAddr)

	err = server.Serve(listener)
	if err != nil {
		log.WithFields(logrus.Fields{
			"error": err,
		}).Fatal("gRPC server exited")
	}
}

func startHTTPServer(opts *Options) {
	http.Handle("/metrics", promhttp.Handler())
	http.ListenAndServe(opts.httpBindAddr, nil)
}
