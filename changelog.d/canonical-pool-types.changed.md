Repeated `poolTypes` entries in a `GetBlockRange`, `GetBlockRangeNullifiers`
or `GetMempoolTx` request are now ignored after the first, so the server's
filtering cost no longer grows with the length of the list.
