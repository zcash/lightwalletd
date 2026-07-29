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

// ConnConfigFromConf reads a config file describing how to reach the Zcash
// node's RPC interface: either TOML (.toml extension) or zcash.conf-style INI.
// Note that a node's own config file (e.g. zebrad.toml) is generally not
// suitable here: its RPC listen address is the node's bind address, which is
// not necessarily an address that lightwalletd can reach. Prefer the
// --rpchost, --rpcport, --rpcuser, and --rpcpassword options instead.
func ConnConfigFromConf(confPath string) (*rpcclient.ConnConfig, error) {
	return connFromConf(confPath)
}

// ConnConfigFromFlags builds an RPC connection config from provided flags.
func ConnConfigFromFlags(opts *common.Options) *rpcclient.ConnConfig {
	return &rpcclient.ConnConfig{
		Host:         net.JoinHostPort(opts.RPCHost, opts.RPCPort),
		User:         opts.RPCUser,
		Pass:         opts.RPCPassword,
		HTTPPostMode: true, // the node RPC only supports HTTP POST mode
		DisableTLS:   true, // the node RPC does not provide TLS by default
	}
}

func connFromConf(confPath string) (*rpcclient.ConnConfig, error) {
	if filepath.Ext(confPath) == ".toml" {
		return connFromToml(confPath)
	} else {
		return connFromIni(confPath)
	}
}

// Read a zcash.conf-style INI config file to find the RPC address and
// credentials of the Zcash node.
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
		return nil, errors.New("rpcpassword not found (or empty), please add rpcpassword= to the config file")
	}

	// Connect to the node's RPC server using HTTP POST mode.
	connCfg := &rpcclient.ConnConfig{
		Host:         net.JoinHostPort(rpcaddr, rpcport),
		User:         username,
		Pass:         password,
		HTTPPostMode: true, // the node RPC only supports HTTP POST mode
		DisableTLS:   true, // the node RPC does not provide TLS by default
	}
	// Notice the notification parameter is nil since notifications are
	// not supported in HTTP POST mode.
	return connCfg, nil
}

// Read a TOML config file containing an [rpc] table (listen_addr, rpcuser,
// rpcpassword) to find the RPC address and credentials of the Zcash node.
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
		HTTPPostMode: true, // the node RPC only supports HTTP POST mode
		DisableTLS:   true, // the node RPC does not provide TLS by default
	}

	// Notice the notification parameter is nil since notifications are
	// not supported in HTTP POST mode.
	return &conf, nil
}
