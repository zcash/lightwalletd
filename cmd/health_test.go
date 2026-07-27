// Copyright (c) 2019-present The Zcash developers
// Distributed under the MIT software license, see the accompanying
// file COPYING or https://www.opensource.org/licenses/mit-license.php .
package cmd

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// Liveness must not depend on the backend. If it did, a backend outage would
// make Kubernetes restart lightwalletd, which cannot fix an unreachable node
// and would produce a restart loop.
func TestLivezIgnoresBackend(t *testing.T) {
	prev := backendReady.Load()
	defer backendReady.Store(prev)

	for _, ready := range []bool{true, false} {
		backendReady.Store(ready)
		rec := httptest.NewRecorder()
		livezHandler(rec, httptest.NewRequest(http.MethodGet, "/livez", nil))
		if rec.Code != http.StatusOK {
			t.Errorf("backendReady=%v: got status %d, want %d", ready, rec.Code, http.StatusOK)
		}
	}
}

// Readiness must track the backend, so an instance that cannot serve is taken
// out of rotation rather than restarted.
func TestReadyzTracksBackend(t *testing.T) {
	prev := backendReady.Load()
	defer backendReady.Store(prev)

	tests := []struct {
		ready bool
		want  int
	}{
		{ready: true, want: http.StatusOK},
		{ready: false, want: http.StatusServiceUnavailable},
	}
	for _, tt := range tests {
		backendReady.Store(tt.ready)
		rec := httptest.NewRecorder()
		readyzHandler(rec, httptest.NewRequest(http.MethodGet, "/readyz", nil))
		if rec.Code != tt.want {
			t.Errorf("backendReady=%v: got status %d, want %d", tt.ready, rec.Code, tt.want)
		}
	}
}

// The probes read cached state rather than calling the backend, so that however
// often they are hit they cannot amplify into load on the node. RawRequest is
// left nil here: if a handler tried to reach the backend it would panic.
func TestProbesDoNotCallBackend(t *testing.T) {
	prev := backendReady.Load()
	defer backendReady.Store(prev)
	backendReady.Store(true)

	for i := 0; i < 100; i++ {
		livezHandler(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/livez", nil))
		readyzHandler(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/readyz", nil))
	}
}
