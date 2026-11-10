// Copyright (c) 2019-present The Zcash developers
// Distributed under the MIT software license, see the accompanying
// file COPYING or https://www.opensource.org/licenses/mit-license.php .
package frontend

import (
	"sync"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// byteBudget bounds the transaction bytes SendTransaction handles at once.
type byteBudget struct {
	mu   sync.Mutex
	used int
	max  int
}

func newByteBudget(limit uint64) *byteBudget {
	if limit == 0 {
		return nil
	}
	// A smaller budget could never admit a maximum-size transaction.
	return &byteBudget{max: int(max(limit, maxRawTxSize))}
}

// reserve takes n bytes of the budget, or refuses at once rather than queue,
// since a waiting request would hold its decoded transaction the whole time.
func (b *byteBudget) reserve(n int) (func(), error) {
	if b == nil {
		return func() {}, nil
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.used+n > b.max {
		// Unavailable, which wallets retry, as for --max-concurrent-blockrange.
		return nil, status.Error(codes.Unavailable,
			"too many transactions being sent at once; retry shortly")
	}
	b.used += n
	return func() {
		b.mu.Lock()
		b.used -= n
		b.mu.Unlock()
	}, nil
}
