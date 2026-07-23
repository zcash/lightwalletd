// Copyright (c) 2026 The Zcash developers
// Distributed under the MIT software license, see the accompanying
// file COPYING or https://www.opensource.org/licenses/mit-license.php .

package frontend

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/btcsuite/btcd/btcjson"
	"github.com/btcsuite/btcd/rpcclient"
)

const (
	requestRetryInterval = 500 * time.Millisecond
	defaultHTTPTimeout   = time.Minute
)

type jsonRPCResponse struct {
	Result json.RawMessage   `json:"result"`
	Error  *btcjson.RPCError `json:"error"`
}

func rpcHTTPURL(cfg *rpcclient.ConnConfig) string {
	protocol := "http"
	if !cfg.DisableTLS {
		protocol = "https"
	}
	return protocol + "://" + cfg.Host
}

// newContextHTTPClient builds an HTTP client that honours the proxy and TLS
// settings on cfg, mirroring btcsuite/btcd/rpcclient's newHTTPClient. This keeps
// NewContextRawRequest a faithful drop-in for rpcclient.RawRequest rather than
// silently dropping proxy or custom-CA configuration when TLS is enabled.
func newContextHTTPClient(cfg *rpcclient.ConnConfig) (*http.Client, error) {
	var proxyFunc func(*http.Request) (*url.URL, error)
	if cfg.Proxy != "" {
		proxyURL, err := url.Parse(cfg.Proxy)
		if err != nil {
			return nil, err
		}
		proxyFunc = http.ProxyURL(proxyURL)
	}

	var tlsConfig *tls.Config
	if !cfg.DisableTLS && len(cfg.Certificates) > 0 {
		pool := x509.NewCertPool()
		pool.AppendCertsFromPEM(cfg.Certificates)
		tlsConfig = &tls.Config{RootCAs: pool}
	}

	return &http.Client{
		Timeout: defaultHTTPTimeout,
		Transport: &http.Transport{
			Proxy:           proxyFunc,
			TLSClientConfig: tlsConfig,
		},
	}, nil
}

// NewContextRawRequest returns a context-aware JSON-RPC function for zcashd and
// zebrad. It is a port of btcsuite/btcd/rpcclient's HTTP POST path (same retry
// count, backoff schedule, and per-request connection handling) with ctx threaded
// through via http.NewRequestWithContext, so that a cancelled gRPC stream aborts
// the in-flight request instead of leaking a goroutine and a zcashd RPC slot.
//
// This wrapper exists only because btcd's rpcclient does not yet expose a
// context-aware request method. Once btcsuite/btcd#2506 (RawRequestWithContext)
// is merged and released, bump the btcd dependency and replace this with a direct
// delegation to it: https://github.com/btcsuite/btcd/pull/2506
func NewContextRawRequest(cfg *rpcclient.ConnConfig) (func(context.Context, string, []json.RawMessage) (json.RawMessage, error), error) {
	httpClient, err := newContextHTTPClient(cfg)
	if err != nil {
		return nil, err
	}
	httpURL := rpcHTTPURL(cfg)

	return func(ctx context.Context, method string, params []json.RawMessage) (json.RawMessage, error) {
		if method == "" {
			return nil, errors.New("no method")
		}
		if params == nil {
			params = []json.RawMessage{}
		}
		if ctx == nil {
			ctx = context.Background()
		}

		reqBody, err := json.Marshal(&btcjson.Request{
			Jsonrpc: btcjson.RpcVersion1,
			ID:      1,
			Method:  method,
			Params:  params,
		})
		if err != nil {
			return nil, err
		}

		tries := 10
		var lastErr error
		for i := 0; i < tries; i++ {
			if err := ctx.Err(); err != nil {
				return nil, err
			}

			httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, httpURL, bytes.NewReader(reqBody))
			if err != nil {
				return nil, err
			}
			httpReq.Close = true
			httpReq.Header.Set("Content-Type", "application/json")
			for key, value := range cfg.ExtraHeaders {
				httpReq.Header.Set(key, value)
			}
			httpReq.SetBasicAuth(cfg.User, cfg.Pass)

			httpResp, err := httpClient.Do(httpReq)
			if err != nil {
				if ctx.Err() != nil {
					return nil, ctx.Err()
				}
				if i == tries-1 {
					return nil, err
				}
				lastErr = err
				backoff := requestRetryInterval * time.Duration(i+1)
				if backoff > time.Minute {
					backoff = time.Minute
				}
				select {
				case <-time.After(backoff):
				case <-ctx.Done():
					return nil, ctx.Err()
				}
				continue
			}
			respBytes, readErr := io.ReadAll(httpResp.Body)
			httpResp.Body.Close()
			if readErr != nil {
				return nil, fmt.Errorf("error reading json reply: %w", readErr)
			}
			var resp jsonRPCResponse
			if err := json.Unmarshal(respBytes, &resp); err != nil {
				return nil, fmt.Errorf("status code: %d, response: %q",
					httpResp.StatusCode, string(respBytes))
			}
			if resp.Error != nil {
				return nil, resp.Error
			}
			return resp.Result, nil
		}
		return nil, fmt.Errorf("invalid http POST response after retries, method: %s, last error=%v",
			method, lastErr)
	}, nil
}
