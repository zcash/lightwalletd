// Copyright (c) 2019-present The Zcash developers
// Distributed under the MIT software license, see the accompanying
// file COPYING or https://www.opensource.org/licenses/mit-license.php .
package common

import (
	"errors"
	"fmt"
	"net"
	"testing"

	"github.com/btcsuite/btcd/btcjson"
)

// FirstRPC waits indefinitely for the backend, so it must be able to tell a
// backend that is merely not up yet from one that is up and refusing our
// credentials -- the latter never resolves on its own.
func TestBackendRejectedCredentials(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "http 401 unauthorized",
			err:  &BackendResponseError{StatusCode: 401, Body: "Unauthorized"},
			want: true,
		},
		{
			name: "http 403 forbidden",
			err:  &BackendResponseError{StatusCode: 403, Body: "Forbidden"},
			want: true,
		},
		{
			// A proxy in front of a restarting node; transient.
			name: "http 502 bad gateway",
			err:  &BackendResponseError{StatusCode: 502, Body: "Bad Gateway"},
			want: false,
		},
		{
			name: "http 503 unavailable",
			err:  &BackendResponseError{StatusCode: 503, Body: "Service Unavailable"},
			want: false,
		},
		{
			// zcashd answers with this while starting up. Treating it as fatal
			// would defeat the point of waiting for the backend at all.
			name: "jsonrpc -28 still warming up",
			err:  &btcjson.RPCError{Code: -28, Message: "Loading block index..."},
			want: false,
		},
		{
			name: "transport error, node not listening",
			err:  &net.OpError{Op: "dial", Err: errors.New("connection refused")},
			want: false,
		},
		{
			name: "plain error",
			err:  errors.New("something else"),
			want: false,
		},
		{
			// The predicate must see through wrapping, since errors pass through
			// several layers before reaching FirstRPC.
			name: "wrapped 401",
			err:  fmt.Errorf("getblockchaininfo: %w", &BackendResponseError{StatusCode: 401}),
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := backendRejectedCredentials(tt.err); got != tt.want {
				t.Errorf("backendRejectedCredentials(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}

// The message is user-facing in a Fatal log line, and matches the format the
// untyped error used before, so operators searching logs still find it.
func TestBackendResponseErrorMessage(t *testing.T) {
	err := &BackendResponseError{StatusCode: 401, Body: "Unauthorized"}
	want := `status code: 401, response: "Unauthorized"`
	if err.Error() != want {
		t.Errorf("got %q, want %q", err.Error(), want)
	}
}
