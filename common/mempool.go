package common

import (
	"encoding/hex"
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

func GetMempool(sendToClient func(*walletrpc.RawTransaction) error) error {
	g_lock.Lock()
	index := 0
	// Stay in this function until the tip block hash changes.
	stayHash := g_lastBlockChainInfo.BestBlockHash

	// Wait for more transactions to be added to the list
	for {
		// Don't fetch the mempool more often than every 2 seconds.
		if time.Since(g_lastTime) > 2*time.Second {
			blockChainInfo, err := getLatestBlockChainInfo()
			if err != nil {
				g_lock.Unlock()
				return err
			}
			if g_lastBlockChainInfo.BestBlockHash != blockChainInfo.BestBlockHash {
				// A new block has arrived
				g_lastBlockChainInfo = blockChainInfo
				Log.Infoln("Latest Block changed, clearing everything")
				// We're the first thread to notice, clear cached state.
				g_txidSeen = map[txid]struct{}{}
				g_txList = []*walletrpc.RawTransaction{}
				g_lastTime = time.Time{}
				break
			}
			if err = refreshMempoolTxns(); err != nil {
				g_lock.Unlock()
				return err
			}
			g_lastTime = time.Now()
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
		time.Sleep(200 * time.Millisecond)
		g_lock.Lock()
		if g_lastBlockChainInfo.BestBlockHash != stayHash {
			break
		}
	}
	g_lock.Unlock()
	return nil
}

// RefreshMempoolTxns gets all new mempool txns and sends any new ones to waiting clients
func refreshMempoolTxns() error {
	Log.Infoln("Refreshing mempool")

	params := []json.RawMessage{}
	result, rpcErr := RawRequest("getrawmempool", params)
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
		g_txidSeen[txid(txidstr)] = struct{}{}
		// We haven't fetched this transaction already.
		txidJSON, err := json.Marshal(txidstr)
		if err != nil {
			return err
		}
		// The "0" is because we only need the raw hex, which is returned as
		// just a hex string, and not even a json string (with quotes).
		params := []json.RawMessage{txidJSON, json.RawMessage("0")}
		result, rpcErr := RawRequest("getrawtransaction", params)
		if rpcErr != nil {
			// Not an error; mempool transactions can disappear
			continue
		}
		// strip the quotes
		var txStr string
		err = json.Unmarshal(result, &txStr)
		if err != nil {
			return err
		}
		txBytes, err := hex.DecodeString(txStr)
		if err != nil {
			return err
		}
		Log.Infoln("appending", txidstr)
		newRtx := &walletrpc.RawTransaction{
			Data:   txBytes,
			Height: uint64(g_lastBlockChainInfo.Blocks),
		}
		g_txList = append(g_txList, newRtx)
	}
	return nil
}

func getLatestBlockChainInfo() (*ZcashdRpcReplyGetblockchaininfo, error) {
	result, rpcErr := RawRequest("getblockchaininfo", []json.RawMessage{})
	if rpcErr != nil {
		return nil, rpcErr
	}
	var getblockchaininfoReply ZcashdRpcReplyGetblockchaininfo
	err := json.Unmarshal(result, &getblockchaininfoReply)
	if err != nil {
		return nil, err
	}
	return &getblockchaininfoReply, nil
}
