- A malformed txid in a backend reply (invalid hex, or hex that is not 32
  bytes) no longer crashes the server: `GetTaddressTransactions`,
  `GetMempoolTx`, and `GetAddressUtxos` now reject it with an `Internal`
  error instead of panicking inside the hash32 conversion. This matters when
  connecting to a nonstandard backend (`--no-backend-check`).
