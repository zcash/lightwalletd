package cmd

import (
	"fmt"
	"net"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/reflection"

	"github.com/zcash-hackworks/lightwalletd/common"
	"github.com/zcash-hackworks/lightwalletd/common/logging"
	"github.com/zcash-hackworks/lightwalletd/frontend"
	"github.com/zcash-hackworks/lightwalletd/walletrpc"
)

var cfgFile string
var log *logrus.Entry
var logger = logrus.New()

// rootCmd represents the base command when called without any subcommands
var rootCmd = &cobra.Command{
	Use:   "lightwalletd",
	Short: "Lightwalletd is a backend service to the Zcash blockchain",
	Long: `Lightwalletd is a backend service that provides a 
         bandwidth-efficient interface to the Zcash blockchain`,
	Run: func(cmd *cobra.Command, args []string) {
		opts := &common.Options{
			BindAddr:          viper.GetString("bind-addr"),
			TLSCertPath:       viper.GetString("tls-cert"),
			TLSKeyPath:        viper.GetString("tls-key"),
			LogLevel:          viper.GetUint64("log-level"),
			LogFile:           viper.GetString("log-file"),
			ZcashConfPath:     viper.GetString("zcash-conf-path"),
			NoTLSVeryInsecure: viper.GetBool("no-tls-very-insecure"),
			CacheSize:         viper.GetInt("cache-size"),
		}

		log.Debugf("Options: %#v\n", opts)

		filesThatShouldExist := []string{
			opts.TLSCertPath,
			opts.TLSKeyPath,
			opts.LogFile,
			opts.ZcashConfPath,
		}

		for _, filename := range filesThatShouldExist {
			if !fileExists(opts.LogFile) {
				os.OpenFile(opts.LogFile, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0666)
			}
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
			log.WithFields(logrus.Fields{
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
			log.WithFields(logrus.Fields{
				"error": err,
				"path":  opts.LogFile,
			}).Fatal("couldn't open log file")
		}
		defer output.Close()
		logger.SetOutput(output)
		logger.SetFormatter(&logrus.JSONFormatter{})
	}

	logger.SetLevel(logrus.Level(opts.LogLevel))
	// gRPC initialization
	var server *grpc.Server

	if opts.NoTLSVeryInsecure {
		log.Warningln("Starting insecure server")
		fmt.Println("Starting insecure server")
		server = grpc.NewServer(logging.LoggingInterceptor())
	} else {
		transportCreds, err := credentials.NewServerTLSFromFile(opts.TLSCertPath, opts.TLSKeyPath)
		if err != nil {
			log.WithFields(logrus.Fields{
				"cert_file": opts.TLSCertPath,
				"key_path":  opts.TLSKeyPath,
				"error":     err,
			}).Fatal("couldn't load TLS credentials")
		}
		server = grpc.NewServer(grpc.Creds(transportCreds), logging.LoggingInterceptor())
	}

	// Enable reflection for debugging
	if opts.LogLevel >= uint64(logrus.WarnLevel) {
		reflection.Register(server)
	}

	// Initialize Zcash RPC client. Right now (Jan 2018) this is only for
	// sending transactions, but in the future it could back a different type
	// of block streamer.

	rpcClient, err := frontend.NewZRPCFromConf(opts.ZcashConfPath)
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
	cache := common.NewBlockCache(opts.CacheSize)

	// Start the block cache importer at cacheSize blocks before current height
	cacheStart := blockHeight - opts.CacheSize
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
	listener, err := net.Listen("tcp", opts.BindAddr)
	if err != nil {
		log.WithFields(logrus.Fields{
			"bind_addr": opts.BindAddr,
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
		os.Exit(1)
	}()

	log.Infof("Starting gRPC server on %s", opts.BindAddr)

	err = server.Serve(listener)
	if err != nil {
		log.WithFields(logrus.Fields{
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
	rootCmd.Flags().String("bind-addr", "127.0.0.1:9067", "the address to listen on")
	rootCmd.Flags().String("tls-cert", "./cert.pem", "the path to a TLS certificate")
	rootCmd.Flags().String("tls-key", "./cert.key", "the path to a TLS key file")
	rootCmd.Flags().Int("log-level", int(logrus.InfoLevel), "log level (logrus 1-7)")
	rootCmd.Flags().String("log-file", "./server.log", "log file to write to")
	rootCmd.Flags().String("zcash-conf-path", "./zcash.conf", "conf file to pull RPC creds from")
	rootCmd.Flags().Bool("no-tls-very-insecure", false, "run without the required TLS certificate, only for debugging, DO NOT use in production")
	rootCmd.Flags().Int("cache-size", 80000, "number of blocks to hold in the cache")

	viper.BindPFlag("bind-addr", rootCmd.Flags().Lookup("bind-addr"))
	viper.SetDefault("bind-addr", "127.0.0.1:9067")
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
	viper.BindPFlag("cache-size", rootCmd.Flags().Lookup("cache-size"))
	viper.SetDefault("cache-size", 80000)

	logger.SetFormatter(&logrus.TextFormatter{
		//DisableColors:          true,
		FullTimestamp:          true,
		DisableLevelTruncation: true,
	})

	onexit := func() {
		fmt.Printf("Lightwalletd died with a Fatal error. Check logfile for details.\n")
	}

	log = logger.WithFields(logrus.Fields{
		"app": "lightwalletd",
	})

	logrus.RegisterExitHandler(onexit)
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
