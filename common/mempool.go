package common

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/zcash/lightwalletd/walletrpc"
)

type txid string

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

	// Mutex to protect the above variables.
	g_lock sync.Mutex
)

func GetMempool(ctx context.Context, sendToClient func(*walletrpc.RawTransaction) error) error {
	g_lock.Lock()
	index := 0
	// Stay in this function until the tip block hash changes.
	stayHash := g_lastBlockChainInfo.BestBlockHash

	// Wait for more transactions to be added to the list
	for {
		// Observe client cancel at the top of every iteration. This is
		// load-bearing on the empty-mempool path where sendToClient is never
		// invoked and the loop would otherwise spin until the next tip change
		// (~75s avg on mainnet).
		if err := ctx.Err(); err != nil {
			g_lock.Unlock()
			return err
		}
		// Don't fetch the mempool more often than every 2 seconds.
		now := Time.Now()
		if now.After(g_lastTime.Add(2 * time.Second)) {
			blockChainInfo, err := GetBlockChainInfo()
			if err != nil {
				g_lock.Unlock()
				return err
			}
			if g_lastBlockChainInfo.BestBlockHash != blockChainInfo.BestBlockHash {
				// A new block has arrived
				g_lastBlockChainInfo = blockChainInfo
				// We're the first thread to notice, clear cached state.
				g_txidSeen = map[txid]struct{}{}
				g_txList = []*walletrpc.RawTransaction{}
				g_lastTime = time.Time{}
				break
			}
			if err = refreshMempoolTxns(ctx); err != nil {
				g_lock.Unlock()
				return err
			}
			g_lastTime = now
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
		g_lock.Lock()
		if g_lastBlockChainInfo.BestBlockHash != stayHash {
			break
		}
	}
	g_lock.Unlock()
	return nil
}

// RefreshMempoolTxns gets all new mempool txns and sends any new ones to waiting clients
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

	// Fetch all new mempool txns and add them into `newTxns`
	for _, txidstr := range mempoolList {
		if _, ok := g_txidSeen[txid(txidstr)]; ok {
			// We've already fetched this transaction
			continue
		}

		// We haven't fetched this transaction already.
		g_txidSeen[txid(txidstr)] = struct{}{}
		txidJSON, err := json.Marshal(txidstr)
		if err != nil {
			return err
		}

		params := []json.RawMessage{txidJSON, json.RawMessage("1")}
		result, rpcErr := RawRequest(ctx, "getrawtransaction", params)
		if rpcErr != nil {
			// Not an error; mempool transactions can disappear
			continue
		}

		rawtx, err := ParseRawTransaction(result)
		if err != nil {
			return err
		}

		// Skip any transaction that has been mined since the list of txids
		// was retrieved.
		if rawtx.Height != 0 {
			continue
		}

		g_txList = append(g_txList, rawtx)
	}
	return nil
}
