- A malformed reply from the backend no longer ends the process. A bad
  `getbestblockhash` reply is retried like a failed call, and a verbose
  `getblock` reply that can't be decoded or whose txid list doesn't match
  the raw block is an error.
