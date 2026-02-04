// Copyright (c) 2019-present The Zcash developers
// Distributed under the MIT software license, see the accompanying
// file COPYING or https://www.opensource.org/licenses/mit-license.php .
package cmd

import (
	"context"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"google.golang.org/grpc/stats"
)

func gaugeValue(g prometheus.Gauge) float64 {
	var m dto.Metric
	g.Write(&m)
	return m.GetGauge().GetValue()
}

func TestHandleConnBeginIncrementsGauge(t *testing.T) {
	grpcServerConnectionsCurrent.Set(0)
	h := &connStatsHandler{}
	ctx := context.Background()

	h.HandleConn(ctx, &stats.ConnBegin{})
	if v := gaugeValue(grpcServerConnectionsCurrent); v != 1 {
		t.Fatalf("expected gauge 1 after ConnBegin, got %v", v)
	}

	h.HandleConn(ctx, &stats.ConnEnd{})
	if v := gaugeValue(grpcServerConnectionsCurrent); v != 0 {
		t.Fatalf("expected gauge 0 after ConnEnd, got %v", v)
	}
}

func TestMultipleConnections(t *testing.T) {
	grpcServerConnectionsCurrent.Set(0)
	h := &connStatsHandler{}
	ctx := context.Background()

	h.HandleConn(ctx, &stats.ConnBegin{})
	h.HandleConn(ctx, &stats.ConnBegin{})
	h.HandleConn(ctx, &stats.ConnBegin{})
	if v := gaugeValue(grpcServerConnectionsCurrent); v != 3 {
		t.Fatalf("expected gauge 3 after 3 ConnBegin, got %v", v)
	}

	h.HandleConn(ctx, &stats.ConnEnd{})
	if v := gaugeValue(grpcServerConnectionsCurrent); v != 2 {
		t.Fatalf("expected gauge 2 after 1 ConnEnd, got %v", v)
	}
}

func TestTagRPCPassthrough(t *testing.T) {
	h := &connStatsHandler{}
	ctx := context.Background()
	got := h.TagRPC(ctx, &stats.RPCTagInfo{})
	if got != ctx {
		t.Fatal("TagRPC should return the same context")
	}
}
