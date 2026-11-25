package common

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/zcash/lightwalletd/walletrpc"
)

type txid string

// mempoolRefreshTimeout bounds a refresh, which no client can cancel.
var mempoolRefreshTimeout = 30 * time.Second

var (
	// Set of mempool txids that have been seen during the current block interval.
	// The zcashd RPC `getrawmempool` returns the entire mempool each time, so
	// this allows us to ignore the txids that we've already seen.
	g_txidSeen map[txid]struct{} = map[txid]struct{}{}

	// List of transactions during current block interval, in order received. Each
	// client thread can keep an index into this slice to record which transactions
	// it's sent back to the client (everything before that index). The g_txidSeen
	// map allows this list to not contain duplicates.
	g_txList []*walletrpc.RawTransaction

	// The most recent absolute time that we fetched the mempool and the latest
	// (tip) block hash (so we know when a new block has been mined).
	g_lastTime time.Time

	// The most recent zcashd getblockchaininfo reply, for height and best block
	// hash (tip) which is used to detect when a new block arrives.
	g_lastBlockChainInfo *ZcashdRpcReplyGetblockchaininfo = &ZcashdRpcReplyGetblockchaininfo{}

	// g_refreshing is set while one caller refreshes the above variables.
	g_refreshing bool

	// g_generation counts resets of g_txList, since a tip hash can recur.
	g_generation uint64

	// Mutex to protect the above variables.
	g_lock sync.Mutex
)

func GetMempool(ctx context.Context, sendToClient func(*walletrpc.RawTransaction) error) error {
	g_lock.Lock()
	index := 0
	// Stay in this function until the list is reset for a new tip.
	stayGeneration := g_generation
	g_lock.Unlock()

	// Wait for more transactions to be added to the list
	for {
		// Observe client cancel at the top of every iteration. This is
		// load-bearing on the empty-mempool path where sendToClient is never
		// invoked and the loop would otherwise spin until the next tip change
		// (~75s avg on mainnet).
		if err := ctx.Err(); err != nil {
			return err
		}
		// Don't fetch the mempool more often than every 2 seconds, and let
		// only one caller at a time do it (GHSA-hp3q-vfr4-v8m6).
		g_lock.Lock()
		if g_generation != stayGeneration {
			// Another stream saw a new block; don't refresh for the old one.
			g_lock.Unlock()
			return nil
		}
		refresh := !g_refreshing && Time.Now().After(g_lastTime.Add(2*time.Second))
		if refresh {
			g_refreshing = true
		}
		g_lock.Unlock()
		if refresh {
			// The refresh is shared, so a caller's cancel mustn't discard it.
			done := make(chan error, 1)
			go func() { done <- refreshMempool(context.WithoutCancel(ctx)) }()
			select {
			case err := <-done:
				if err != nil {
					return err
				}
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		g_lock.Lock()
		if g_generation != stayGeneration {
			g_lock.Unlock()
			return nil
		}
		// Send transactions we haven't sent yet, best to not do so while
		// holding the mutex, since this call may get flow-controlled.
		toSend := g_txList[index:]
		index = len(g_txList)
		g_lock.Unlock()
		for _, tx := range toSend {
			if err := sendToClient(tx); err != nil {
				return err
			}
		}
		// Cancel-aware sleep: replaces the prior non-cancellable Time.Sleep so
		// a client disconnect aborts within 200ms even when sendToClient is
		// never invoked.
		select {
		case <-Time.After(200 * time.Millisecond):
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

// refreshMempool brings the shared state up to date without holding g_lock
// across backend calls. The caller must have set g_refreshing.
func refreshMempool(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, mempoolRefreshTimeout)
	defer cancel()
	defer func() {
		g_lock.Lock()
		g_refreshing = false
		g_lock.Unlock()
	}()
	start := Time.Now()
	blockChainInfo, err := getBlockChainInfo(ctx)
	if err != nil {
		return err
	}
	g_lock.Lock()
	if g_lastBlockChainInfo.BestBlockHash != blockChainInfo.BestBlockHash {
		// A new block has arrived; we're the first thread to notice, clear cached state.
		g_lastBlockChainInfo = blockChainInfo
		g_txidSeen = map[txid]struct{}{}
		g_txList = []*walletrpc.RawTransaction{}
		g_generation++
		g_lastTime = time.Time{}
		g_lock.Unlock()
		return nil
	}
	g_lock.Unlock()
	if err := refreshMempoolTxns(ctx); err != nil {
		return err
	}
	g_lock.Lock()
	g_lastTime = start
	g_lock.Unlock()
	return nil
}

// refreshMempoolTxns fetches new mempool txns and appends them to g_txList.
// Only the g_refreshing holder writes g_txidSeen, so reading it here is safe.
func refreshMempoolTxns(ctx context.Context) error {
	params := []json.RawMessage{}
	result, rpcErr := RawRequest(ctx, "getrawmempool", params)
	if rpcErr != nil {
		return rpcErr
	}
	var mempoolList []string
	err := json.Unmarshal(result, &mempoolList)
	if err != nil {
		return err
	}

	for _, txidstr := range mempoolList {
		if _, ok := g_txidSeen[txid(txidstr)]; ok {
			// We've already fetched this transaction
			continue
		}
		txidJSON, err := json.Marshal(txidstr)
		if err != nil {
			return err
		}

		params := []json.RawMessage{txidJSON, json.RawMessage("1")}
		result, rpcErr := RawRequest(ctx, "getrawtransaction", params)
		if rpcErr != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			// Not an error; mempool transactions can disappear
			continue
		}

		rawtx, err := ParseRawTransaction(result)

		// Marked seen only once fetched, so an aborted refresh can't hide
		// it (GHSA-pj5c-jx29-gmxr).
		g_lock.Lock()
		g_txidSeen[txid(txidstr)] = struct{}{}
		if err != nil {
			g_lock.Unlock()
			Log.WithField("txid", txidstr).Warn("skipping unparseable mempool transaction: ", err)
			continue
		}
		// Skip any transaction that has been mined since the list of txids
		// was retrieved.
		if rawtx.Height == 0 {
			g_txList = append(g_txList, rawtx)
		}
		g_lock.Unlock()
	}
	return nil
}
