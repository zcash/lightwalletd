// Copyright (c) 2019-present The Zcash developers
// Distributed under the MIT software license, see the accompanying
// file COPYING or https://www.opensource.org/licenses/mit-license.php .
package cmd

import (
	"context"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"google.golang.org/grpc/stats"
)

var grpcServerConnectionsCurrent = promauto.NewGauge(prometheus.GaugeOpts{
	Name: "grpc_server_connections_current",
	Help: "Number of currently active gRPC client connections.",
})

// connStatsHandler implements stats.Handler to track gRPC connection lifecycle.
type connStatsHandler struct{}

func (h *connStatsHandler) TagRPC(ctx context.Context, info *stats.RPCTagInfo) context.Context {
	return ctx
}

func (h *connStatsHandler) HandleRPC(ctx context.Context, s stats.RPCStats) {}

func (h *connStatsHandler) TagConn(ctx context.Context, info *stats.ConnTagInfo) context.Context {
	return ctx
}

func (h *connStatsHandler) HandleConn(ctx context.Context, s stats.ConnStats) {
	switch s.(type) {
	case *stats.ConnBegin:
		grpcServerConnectionsCurrent.Inc()
	case *stats.ConnEnd:
		grpcServerConnectionsCurrent.Dec()
	}
}
