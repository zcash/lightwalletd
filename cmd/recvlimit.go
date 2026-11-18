// Copyright (c) 2019-present The Zcash developers
// Distributed under the MIT software license, see the accompanying
// file COPYING or https://www.opensource.org/licenses/mit-license.php .
package cmd

import (
	"context"
	"sync"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	reflectionv1 "google.golang.org/grpc/reflection/grpc_reflection_v1"
	reflectionv1alpha "google.golang.org/grpc/reflection/grpc_reflection_v1alpha"
	"google.golang.org/grpc/stats"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/tap"

	"github.com/zcash/lightwalletd/walletrpc"
)

// maxRecvMsgSize fits the largest transaction SendTransaction accepts.
const maxRecvMsgSize = 2 << 20

// recvFirstMsgTimeout cancels a stream whose request hasn't arrived in time.
const recvFirstMsgTimeout = 20 * time.Second

var errRecvBusy = status.Error(codes.Unavailable,
	"too many requests being received at once; retry shortly")

type recvReleaseKey struct{}

type recvConnKey struct{}

// recvConn identifies a connection by its address.
type recvConn struct{ _ byte }

// recvHolder is one stream holding a slot.
type recvHolder struct {
	conn   *recvConn
	cancel context.CancelFunc
}

// recvPool is a fixed number of slots shared fairly between connections.
type recvPool struct {
	mu      sync.Mutex
	limit   int
	inUse   int
	holders map[*recvConn][]*recvHolder
}

func newRecvPool(limit uint64) *recvPool {
	return &recvPool{limit: int(limit), holders: map[*recvConn][]*recvHolder{}}
}

// acquire claims a slot for conn. When the pool is full, it takes the oldest
// slot of the connection holding the most, if that one holds at least two
// more than conn, so no connection can keep another below its fair share.
func (p *recvPool) acquire(conn *recvConn, cancel context.CancelFunc) (func(), bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.inUse >= p.limit {
		var heaviest *recvConn
		for c, hs := range p.holders {
			if heaviest == nil || len(hs) > len(p.holders[heaviest]) {
				heaviest = c
			}
		}
		if heaviest == nil || len(p.holders[heaviest]) < len(p.holders[conn])+2 {
			return nil, false
		}
		victim := p.holders[heaviest][0]
		p.removeLocked(victim)
		victim.cancel()
	}
	h := &recvHolder{conn: conn, cancel: cancel}
	p.holders[conn] = append(p.holders[conn], h)
	p.inUse++
	var once sync.Once
	return func() {
		once.Do(func() {
			p.mu.Lock()
			p.removeLocked(h)
			p.mu.Unlock()
		})
	}, true
}

// removeLocked frees h's slot, if it still holds one.
func (p *recvPool) removeLocked(h *recvHolder) {
	hs := p.holders[h.conn]
	for i, x := range hs {
		if x == h {
			hs = append(hs[:i], hs[i+1:]...)
			p.inUse--
			if len(hs) == 0 {
				delete(p.holders, h.conn)
			} else {
				p.holders[h.conn] = hs
			}
			return
		}
	}
}

// recvLimiter bounds the requests being received at once, server-wide.
type recvLimiter struct {
	slots           *recvPool
	streams         *recvPool
	clientStreaming map[string]bool
	timeout         time.Duration
}

func newRecvLimiter(limit uint64) *recvLimiter {
	l := &recvLimiter{
		slots:           newRecvPool(limit),
		streams:         newRecvPool(limit),
		clientStreaming: map[string]bool{},
		timeout:         recvFirstMsgTimeout,
	}
	for _, desc := range []grpc.ServiceDesc{
		walletrpc.CompactTxStreamer_ServiceDesc,
		reflectionv1.ServerReflection_ServiceDesc,
		reflectionv1alpha.ServerReflection_ServiceDesc,
	} {
		for _, s := range desc.Streams {
			if s.ClientStreams {
				l.clientStreaming["/"+desc.ServiceName+"/"+s.StreamName] = true
			}
		}
	}
	return l
}

// tapHandle claims a slot when a stream opens, before any of its body arrives.
func (l *recvLimiter) tapHandle(ctx context.Context, info *tap.Info) (context.Context, error) {
	conn, ok := ctx.Value(recvConnKey{}).(*recvConn)
	if !ok {
		conn = &recvConn{}
	}
	ctx, cancel := context.WithCancel(ctx)
	if l.clientStreaming[info.FullMethodName] {
		// Client streams keep their slot, in a separate pool, until they end.
		release, ok := l.streams.acquire(conn, cancel)
		if !ok {
			cancel()
			return nil, errRecvBusy
		}
		context.AfterFunc(ctx, release)
		return ctx, nil
	}
	release, ok := l.slots.acquire(conn, cancel)
	if !ok {
		cancel()
		return nil, errRecvBusy
	}
	timer := time.AfterFunc(l.timeout, cancel)
	stop := func() {
		timer.Stop()
		release()
	}
	context.AfterFunc(ctx, stop)
	return context.WithValue(ctx, recvReleaseKey{}, stop), nil
}

func (l *recvLimiter) TagRPC(ctx context.Context, _ *stats.RPCTagInfo) context.Context {
	return ctx
}

// HandleRPC frees the slot once the request message has been received.
func (l *recvLimiter) HandleRPC(ctx context.Context, s stats.RPCStats) {
	if _, ok := s.(*stats.InPayload); ok {
		if release, ok := ctx.Value(recvReleaseKey{}).(func()); ok {
			release()
		}
	}
}

func (l *recvLimiter) TagConn(ctx context.Context, _ *stats.ConnTagInfo) context.Context {
	return context.WithValue(ctx, recvConnKey{}, &recvConn{})
}

func (l *recvLimiter) HandleConn(context.Context, stats.ConnStats) {}

func (l *recvLimiter) serverOptions() []grpc.ServerOption {
	return []grpc.ServerOption{
		grpc.InTapHandle(l.tapHandle),
		grpc.StatsHandler(l),
	}
}
