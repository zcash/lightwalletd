- A malformed reply from the backend no longer ends the process. A bad
  `getbestblockhash` reply is retried like a failed call, a verbose
  `getblock` reply that can't be decoded or whose txid list doesn't match
  the raw block is an error, and a mempool txid that isn't 32 bytes is
  skipped.
