// Copyright (c) 2019-2020 The Zcash developers
// Distributed under the MIT software license, see the accompanying
// file COPYING or https://www.opensource.org/licenses/mit-license.php .

package frontend

import (
	"errors"
	"fmt"
	"net"

	"github.com/btcsuite/btcd/rpcclient"
	"github.com/zcash/lightwalletd/common"
	ini "gopkg.in/ini.v1"
)

// NewZRPCFromConf reads the zcashd configuration file.
func NewZRPCFromConf(confPath interface{}) (*rpcclient.Client, error) {
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

// If passed a string, interpret as a path, open and read; if passed
// a byte slice, interpret as the config file content (used in testing).
func connFromConf(confPath interface{}) (*rpcclient.ConnConfig, error) {
	cfg, err := ini.Load(confPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
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
