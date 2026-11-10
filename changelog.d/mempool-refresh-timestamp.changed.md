A `GetMempoolTx` mempool refresh that fails no longer holds off other
callers' refreshes for two seconds. A client that cancels, or stops
reading, no longer stops the mempool view from being refreshed. A
transaction that fails to parse is skipped rather than failing every
refresh until it leaves the mempool.
