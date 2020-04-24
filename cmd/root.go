package cmd

import (
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	grpc_middleware "github.com/grpc-ecosystem/go-grpc-middleware"
	grpc_prometheus "github.com/grpc-ecosystem/go-grpc-prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/reflection"

	"github.com/zcash/lightwalletd/common"
	"github.com/zcash/lightwalletd/common/logging"
	"github.com/zcash/lightwalletd/frontend"
	"github.com/zcash/lightwalletd/walletrpc"
)

var cfgFile string
var logger = logrus.New()

// rootCmd represents the base command when called without any subcommands
var rootCmd = &cobra.Command{
	Use:   "lightwalletd",
	Short: "Lightwalletd is a backend service to the Zcash blockchain",
	Long: `Lightwalletd is a backend service that provides a 
         bandwidth-efficient interface to the Zcash blockchain`,
	Run: func(cmd *cobra.Command, args []string) {
		opts := &common.Options{
			GRPCBindAddr:      viper.GetString("grpc-bind-addr"),
			HTTPBindAddr:      viper.GetString("http-bind-addr"),
			TLSCertPath:       viper.GetString("tls-cert"),
			TLSKeyPath:        viper.GetString("tls-key"),
			LogLevel:          viper.GetUint64("log-level"),
			LogFile:           viper.GetString("log-file"),
			ZcashConfPath:     viper.GetString("zcash-conf-path"),
			NoTLSVeryInsecure: viper.GetBool("no-tls-very-insecure"),
			DataDir:           viper.GetString("data-dir"),
			Redownload:        viper.GetBool("redownload"),
			Darkside:          viper.GetBool("darkside-very-insecure"),
		}

		common.Log.Debugf("Options: %#v\n", opts)

		filesThatShouldExist := []string{
			opts.TLSCertPath,
			opts.TLSKeyPath,
			opts.LogFile,
		}
		if !fileExists(opts.LogFile) {
			os.OpenFile(opts.LogFile, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0666)
		}
		if !opts.Darkside {
			filesThatShouldExist = append(filesThatShouldExist, opts.ZcashConfPath)
		}

		for _, filename := range filesThatShouldExist {
			if opts.NoTLSVeryInsecure && (filename == opts.TLSCertPath || filename == opts.TLSKeyPath) {
				continue
			}
			if !fileExists(filename) {
				os.Stderr.WriteString(fmt.Sprintf("\n  ** File does not exist: %s\n\n", filename))
				os.Exit(1)
			}
		}

		// Start server and block, or exit
		if err := startServer(opts); err != nil {
			common.Log.WithFields(logrus.Fields{
				"error": err,
			}).Fatal("couldn't create server")
		}
	},
}

func fileExists(filename string) bool {
	info, err := os.Stat(filename)
	if os.IsNotExist(err) {
		return false
	}
	return !info.IsDir()
}

func startServer(opts *common.Options) error {
	if opts.LogFile != "" {
		// instead write parsable logs for logstash/splunk/etc
		output, err := os.OpenFile(opts.LogFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		if err != nil {
			common.Log.WithFields(logrus.Fields{
				"error": err,
				"path":  opts.LogFile,
			}).Fatal("couldn't open log file")
		}
		defer output.Close()
		logger.SetOutput(output)
		logger.SetFormatter(&logrus.JSONFormatter{})
	}

	logger.SetLevel(logrus.Level(opts.LogLevel))

	common.Log.WithFields(logrus.Fields{
		"gitCommit": common.GitCommit,
		"buildDate": common.BuildDate,
		"buildUser": common.BuildUser,
	}).Infof("Starting gRPC server version %s on %s", common.Version, opts.GRPCBindAddr)

	// gRPC initialization
	var server *grpc.Server

	if opts.NoTLSVeryInsecure {
		common.Log.Warningln("Starting insecure server")
		fmt.Println("Starting insecure server")
		server = grpc.NewServer(
			grpc.StreamInterceptor(
				grpc_middleware.ChainStreamServer(
					grpc_prometheus.StreamServerInterceptor),
			),
			grpc.UnaryInterceptor(grpc_middleware.ChainUnaryServer(
				logging.LogInterceptor,
				grpc_prometheus.UnaryServerInterceptor),
			))
	} else {
		transportCreds, err := credentials.NewServerTLSFromFile(opts.TLSCertPath, opts.TLSKeyPath)
		if err != nil {
			common.Log.WithFields(logrus.Fields{
				"cert_file": opts.TLSCertPath,
				"key_path":  opts.TLSKeyPath,
				"error":     err,
			}).Fatal("couldn't load TLS credentials")
		}
		server = grpc.NewServer(
			grpc.Creds(transportCreds),
			grpc.StreamInterceptor(grpc_middleware.ChainStreamServer(
				grpc_prometheus.StreamServerInterceptor),
			),
			grpc.UnaryInterceptor(grpc_middleware.ChainUnaryServer(
				logging.LogInterceptor,
				grpc_prometheus.UnaryServerInterceptor),
			))
	}
	grpc_prometheus.EnableHandlingTimeHistogram()
	grpc_prometheus.Register(server)
	go startHTTPServer(opts)

	// Enable reflection for debugging
	if opts.LogLevel >= uint64(logrus.WarnLevel) {
		reflection.Register(server)
	}

	// Initialize Zcash RPC client. Right now (Jan 2018) this is only for
	// sending transactions, but in the future it could back a different type
	// of block streamer.

	if opts.Darkside {
		common.RawRequest = common.DarkSideRawRequest
	} else {
		rpcClient, err := frontend.NewZRPCFromConf(opts.ZcashConfPath)
		if err != nil {
			common.Log.WithFields(logrus.Fields{
				"error": err,
			}).Fatal("setting up RPC connection to zcashd")
		}
		// Indirect function for test mocking (so unit tests can talk to stub functions).
		common.RawRequest = rpcClient.RawRequest
	}

	// Get the sapling activation height from the RPC
	// (this first RPC also verifies that we can communicate with zcashd)
	saplingHeight, blockHeight, chainName, branchID := common.GetSaplingInfo()
	common.Log.Info("Got sapling height ", saplingHeight, " block height ", blockHeight, " chain ", chainName, " branchID ", branchID)

	dbPath := filepath.Join(opts.DataDir, "db")
	if opts.Darkside {
		os.RemoveAll(filepath.Join(dbPath, chainName))
	}

	if err := os.MkdirAll(opts.DataDir, 0755); err != nil {
		os.Stderr.WriteString(fmt.Sprintf("\n  ** Can't create data directory: %s\n\n", opts.DataDir))
		os.Exit(1)
	}
	if err := os.MkdirAll(dbPath, 0755); err != nil {
		os.Stderr.WriteString(fmt.Sprintf("\n  ** Can't create db directory: %s\n\n", dbPath))
		os.Exit(1)
	}
	cache := common.NewBlockCache(dbPath, chainName, saplingHeight, opts.Redownload)
	go common.BlockIngestor(cache, 0 /*loop forever*/)

	// Compact transaction service initialization
	{
		service, err := frontend.NewLwdStreamer(cache)
		if err != nil {
			common.Log.WithFields(logrus.Fields{
				"error": err,
			}).Fatal("couldn't create backend")
		}
		walletrpc.RegisterCompactTxStreamerServer(server, service)
	}
	if opts.Darkside {
		service, err := frontend.NewDarksideStreamer(cache)
		if err != nil {
			common.Log.WithFields(logrus.Fields{
				"error": err,
			}).Fatal("couldn't create backend")
		}
		walletrpc.RegisterDarksideStreamerServer(server, service)
	}

	// Start listening
	listener, err := net.Listen("tcp", opts.GRPCBindAddr)
	if err != nil {
		common.Log.WithFields(logrus.Fields{
			"bind_addr": opts.GRPCBindAddr,
			"error":     err,
		}).Fatal("couldn't create listener")
	}

	// Signal handler for graceful stops
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		s := <-signals
		cache.Sync()
		common.Log.WithFields(logrus.Fields{
			"signal": s.String(),
		}).Info("caught signal, stopping gRPC server")
		os.Exit(1)
	}()

	err = server.Serve(listener)
	if err != nil {
		common.Log.WithFields(logrus.Fields{
			"error": err,
		}).Fatal("gRPC server exited")
	}
	return nil
}

// Execute adds all child commands to the root command and sets flags appropriately.
// This is called by main.main(). It only needs to happen once to the rootCmd.
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}

func init() {
	rootCmd.AddCommand(versionCmd)
	cobra.OnInitialize(initConfig)
	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default is current directory, lightwalletd.yaml)")
	rootCmd.Flags().String("http-bind-addr", "127.0.0.1:9068", "the address to listen for http on")
	rootCmd.Flags().String("grpc-bind-addr", "127.0.0.1:9067", "the address to listen for grpc on")
	rootCmd.Flags().String("tls-cert", "./cert.pem", "the path to a TLS certificate")
	rootCmd.Flags().String("tls-key", "./cert.key", "the path to a TLS key file")
	rootCmd.Flags().Int("log-level", int(logrus.InfoLevel), "log level (logrus 1-7)")
	rootCmd.Flags().String("log-file", "./server.log", "log file to write to")
	rootCmd.Flags().String("zcash-conf-path", "./zcash.conf", "conf file to pull RPC creds from")
	rootCmd.Flags().Bool("no-tls-very-insecure", false, "run without the required TLS certificate, only for debugging, DO NOT use in production")
	rootCmd.Flags().Bool("redownload", false, "re-fetch all blocks from zcashd; reinitialize local cache files")
	rootCmd.Flags().String("data-dir", "/var/lib/lightwalletd", "data directory (such as db)")
	rootCmd.Flags().Bool("darkside-very-insecure", false, "run with GRPC-controllable mock zcashd for integration testing (shuts down after 30 minutes)")

	viper.BindPFlag("grpc-bind-addr", rootCmd.Flags().Lookup("grpc-bind-addr"))
	viper.SetDefault("grpc-bind-addr", "127.0.0.1:9067")
	viper.BindPFlag("http-bind-addr", rootCmd.Flags().Lookup("http-bind-addr"))
	viper.SetDefault("http-bind-addr", "127.0.0.1:9068")
	viper.BindPFlag("tls-cert", rootCmd.Flags().Lookup("tls-cert"))
	viper.SetDefault("tls-cert", "./cert.pem")
	viper.BindPFlag("tls-key", rootCmd.Flags().Lookup("tls-key"))
	viper.SetDefault("tls-key", "./cert.key")
	viper.BindPFlag("log-level", rootCmd.Flags().Lookup("log-level"))
	viper.SetDefault("log-level", int(logrus.InfoLevel))
	viper.BindPFlag("log-file", rootCmd.Flags().Lookup("log-file"))
	viper.SetDefault("log-file", "./server.log")
	viper.BindPFlag("zcash-conf-path", rootCmd.Flags().Lookup("zcash-conf-path"))
	viper.SetDefault("zcash-conf-path", "./zcash.conf")
	viper.BindPFlag("no-tls-very-insecure", rootCmd.Flags().Lookup("no-tls-very-insecure"))
	viper.SetDefault("no-tls-very-insecure", false)
	viper.BindPFlag("redownload", rootCmd.Flags().Lookup("redownload"))
	viper.SetDefault("redownload", false)
	viper.BindPFlag("data-dir", rootCmd.Flags().Lookup("data-dir"))
	viper.SetDefault("data-dir", "/var/lib/lightwalletd")
	viper.BindPFlag("darkside-very-insecure", rootCmd.Flags().Lookup("darkside-very-insecure"))
	viper.SetDefault("darkside-very-insecure", false)

	logger.SetFormatter(&logrus.TextFormatter{
		//DisableColors:          true,
		FullTimestamp:          true,
		DisableLevelTruncation: true,
	})

	onexit := func() {
		fmt.Printf("Lightwalletd died with a Fatal error. Check logfile for details.\n")
	}

	common.Log = logger.WithFields(logrus.Fields{
		"app": "lightwalletd",
	})

	logrus.RegisterExitHandler(onexit)

	// Indirect function for test mocking (so unit tests can talk to stub functions)
	common.Sleep = time.Sleep
}

// initConfig reads in config file and ENV variables if set.
func initConfig() {
	if cfgFile != "" {
		// Use config file from the flag.
		viper.SetConfigFile(cfgFile)
	} else {
		// Look in the current directory for a configuration file
		viper.AddConfigPath(".")
		// Viper auto appends extention to this config name
		// For example, lightwalletd.yml
		viper.SetConfigName("lightwalletd")
	}

	// Replace `-` in config options with `_` for ENV keys
	replacer := strings.NewReplacer("-", "_")
	viper.SetEnvKeyReplacer(replacer)
	viper.AutomaticEnv() // read in environment variables that match
	// If a config file is found, read it in.
	var err error
	if err = viper.ReadInConfig(); err == nil {
		fmt.Println("Using config file:", viper.ConfigFileUsed())
	}

}

func startHTTPServer(opts *common.Options) {
	http.Handle("/metrics", promhttp.Handler())
	http.ListenAndServe(opts.HTTPBindAddr, nil)
}
