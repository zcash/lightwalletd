// +build ignore

package frontend

import (
	"log"

	"github.com/btcsuite/btcd/rpcclient"
)

func NewZRPCFromConf(confPath string) (*rpcclient.Client, error) {
	return nil, errors.New("not yet implemented")
	//return NewZRPCFromCreds(addr, username, password)
}

func NewZRPCFromCreds(addr, username, password string) (*rpcclient.Client, error) {
	// Connect to local zcash RPC server using HTTP POST mode.
	connCfg := &rpcclient.ConnConfig{
		Host:         addr,
		User:         username,
		Pass:         password,
		HTTPPostMode: true, // Zcash only supports HTTP POST mode
		DisableTLS:   true, // Zcash does not provide TLS by default
	}
	// Notice the notification parameter is nil since notifications are
	// not supported in HTTP POST mode.
	return rpcclient.New(connCfg, nil)
}
