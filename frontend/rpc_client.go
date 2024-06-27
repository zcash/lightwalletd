// Copyright (c) 2019-2020 The Zcash developers
// Distributed under the MIT software license, see the accompanying
// file COPYING or https://www.opensource.org/licenses/mit-license.php .

package frontend

import (
	"errors"
	"fmt"
	"net"
	"path/filepath"

	"github.com/BurntSushi/toml"
	"github.com/btcsuite/btcd/rpcclient"
	"github.com/zcash/lightwalletd/common"
	ini "gopkg.in/ini.v1"
)

// NewZRPCFromConf reads the zcashd configuration file.
func NewZRPCFromConf(confPath string) (*rpcclient.Client, error) {
	connCfg, err := connFromConf(confPath)
	if err != nil {
		return nil, err
	}
	return rpcclient.New(connCfg, nil)
}

// NewZRPCFromFlags gets zcashd rpc connection information from provided flags.
func NewZRPCFromFlags(opts *common.Options) (*rpcclient.Client, error) {
	// Connect to local Zcash RPC server using HTTP POST mode.
	connCfg := &rpcclient.ConnConfig{
		Host:         net.JoinHostPort(opts.RPCHost, opts.RPCPort),
		User:         opts.RPCUser,
		Pass:         opts.RPCPassword,
		HTTPPostMode: true, // Zcash only supports HTTP POST mode
		DisableTLS:   true, // Zcash does not provide TLS by default
	}
	return rpcclient.New(connCfg, nil)
}

func connFromConf(confPath string) (*rpcclient.ConnConfig, error) {
	if filepath.Ext(confPath) == ".toml" {
		return connFromToml(confPath)
	} else {
		return connFromIni(confPath)
	}
}

func connFromIni(confPath string) (*rpcclient.ConnConfig, error) {
	cfg, err := ini.Load(confPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file in .conf format: %w", err)
	}

	rpcaddr := cfg.Section("").Key("rpcbind").String()
	if rpcaddr == "" {
		rpcaddr = "127.0.0.1"
	}
	rpcport := cfg.Section("").Key("rpcport").String()
	if rpcport == "" {
		rpcport = "8232" // default mainnet
		testnet, _ := cfg.Section("").Key("testnet").Int()
		regtest, _ := cfg.Section("").Key("regtest").Int()
		if testnet > 0 || regtest > 0 {
			rpcport = "18232"
		}
	}
	username := cfg.Section("").Key("rpcuser").String()
	password := cfg.Section("").Key("rpcpassword").String()

	if password == "" {
		return nil, errors.New("rpcpassword not found (or empty), please add rpcpassword= to zcash.conf")
	}

	// Connect to local Zcash RPC server using HTTP POST mode.
	connCfg := &rpcclient.ConnConfig{
		Host:         net.JoinHostPort(rpcaddr, rpcport),
		User:         username,
		Pass:         password,
		HTTPPostMode: true, // Zcash only supports HTTP POST mode
		DisableTLS:   true, // Zcash does not provide TLS by default
	}
	// Notice the notification parameter is nil since notifications are
	// not supported in HTTP POST mode.
	return connCfg, nil
}

// If passed a string, interpret as a path, open and read; if passed
// a byte slice, interpret as the config file content (used in testing).
func connFromToml(confPath string) (*rpcclient.ConnConfig, error) {
	var tomlConf struct {
		Rpc struct {
			Listen_addr string
			RPCUser     string
			RPCPassword string
		}
	}
	_, err := toml.DecodeFile(confPath, &tomlConf)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file in .toml format: %w", err)
	}
	conf := rpcclient.ConnConfig{
		Host:         tomlConf.Rpc.Listen_addr,
		User:         tomlConf.Rpc.RPCUser,
		Pass:         tomlConf.Rpc.RPCPassword,
		HTTPPostMode: true, // Zcash only supports HTTP POST mode
		DisableTLS:   true, // Zcash does not provide TLS by default
	}

	// Notice the notification parameter is nil since notifications are
	// not supported in HTTP POST mode.
	return &conf, nil
}
