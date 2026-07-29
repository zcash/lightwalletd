// Copyright (c) 2019-present The Zcash developers
// Distributed under the MIT software license, see the accompanying
// file COPYING or https://www.opensource.org/licenses/mit-license.php .
package cmd

import (
	"fmt"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/sirupsen/logrus"
	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"

	"github.com/zcash/lightwalletd/common"
)

// backendPollInterval is how often the backend node is checked on behalf of the
// readiness probe and the gRPC health service.
const backendPollInterval = 5 * time.Second

// backendReady records whether the backend node was reachable as of the last
// poll. The probes read this rather than querying the backend themselves, so
// that however often they are called -- including by something that isn't a
// probe at all -- they never turn into load on the node.
var backendReady atomic.Bool

// checkBackend reports whether the backend node is answering RPCs.
// getblockchaininfo is used because it is the cheapest call that proves the
// node is actually serving, rather than merely accepting connections.
func checkBackend() error {
	_, err := common.GetBlockChainInfo()
	return err
}

// pollBackendHealth keeps backendReady and the gRPC health service current for
// the life of the process. Running one poller, rather than checking on each
// request, also keeps the two probes from ever disagreeing.
func pollBackendHealth(healthServer *health.Server) {
	var reported bool
	first := true
	for {
		err := checkBackend()
		ready := err == nil
		backendReady.Store(ready)

		status := healthpb.HealthCheckResponse_NOT_SERVING
		if ready {
			status = healthpb.HealthCheckResponse_SERVING
		}
		healthServer.SetServingStatus("", status)

		// Log transitions only; the backend being down for a while should not
		// fill the log with one line per poll.
		if first || ready != reported {
			if ready {
				if !first {
					common.Log.Info("backend is reachable again, reporting ready")
				}
			} else {
				common.Log.WithFields(logrus.Fields{
					"error": err,
				}).Warn("backend is unreachable, reporting not ready")
			}
			reported = ready
			first = false
		}
		time.Sleep(backendPollInterval)
	}
}

// livezHandler answers the liveness probe: is this process running at all?
//
// It deliberately ignores the backend. Restarting lightwalletd cannot fix an
// unreachable node, so a liveness probe that failed on backend outages would
// turn a backend blip into a restart loop -- exactly the crash looping that
// waiting for the backend on startup is meant to avoid.
func livezHandler(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	fmt.Fprintln(w, "ok")
}

// readyzHandler answers the readiness probe: can this instance serve wallet
// requests? That requires the backend node, so a failure here should take the
// instance out of rotation without restarting it.
func readyzHandler(w http.ResponseWriter, r *http.Request) {
	if !backendReady.Load() {
		http.Error(w, "backend unavailable", http.StatusServiceUnavailable)
		return
	}
	w.WriteHeader(http.StatusOK)
	fmt.Fprintln(w, "ok")
}
