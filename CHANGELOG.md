# Changelog
All notable changes to this library will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this library adheres to Rust's notion of
[Semantic Versioning](https://semver.org/spec/v2.0.0.html).
The most recent changes are listed first.

## [0.5.2] - 2026-07-30

### Fixed

- Narrow the `GetMempoolTx` mutex to the cache snapshot it protects, instead
  of holding it across the whole handler (GHSA-f9pw-q493-7qvh,
  GHSA-9p9r-mggr-8q9g). The mempool refresh issues one `getrawmempool` plus a
  `getrawtransaction` per new txid, and the response loop streams to the
  client; both ran inside the critical section, so a single caller's backend
  round-trips — or one client that simply stopped reading its stream — stalled
  every other `GetMempoolTx` caller. The backend calls and the streaming sends
  now run outside the lock, which is taken only to claim a refresh, snapshot
  the cache, and publish the new one.

- A `GetMempoolTx` refresh now stops when the client that triggered it
  disconnects, instead of running to completion. Since the backend calls
  observe the request context, every remaining fetch would otherwise fail and
  be treated as a transaction that had left the mempool, publishing a cache
  missing nearly all of its entries and degrading it for other callers until
  the next refresh.

- A `GetMempoolTx` refresh that fails part-way no longer leaves the cached
  txid list updated while the transaction map still holds the previous
  contents; the two are now published together.

## [0.5.1] - 2026-07-27

### Added

- Recognize `zakura` as a supported backend node.

- Add the `--no-backend-check` option, which skips backend detection entirely
  so that lightwalletd can connect to any node that speaks the expected RPCs.

### Fixed

- Validate the size of client-supplied blobs before expanding them.
  `GetTreeState` now rejects a block hash that isn't 32 bytes
  (GHSA-q2c2-hpp9-58hm), and `SendTransaction` rejects a raw transaction
  larger than the 2,000,000-byte Zcash block size limit
  (GHSA-6ppp-r2gc-9q6v). Both previously hex-encoded the bytes (doubling
  them) and JSON-marshalled the result before forwarding to zcashd, so an
  unauthenticated client could force large allocations in lightwalletd, and
  parsing work in the backend, using input that could only ever be rejected —
  enough in concurrent requests to drive the process into an out-of-memory
  kill.

- Cap the `GetMempoolTx` exclude list, and build it before taking the method
  mutex (GHSA-4hp3-3494-3f2m). Each excluded txid suffix was length-checked,
  but the number of them was not, so a client could send a very large list and
  make the server allocate, hex-encode and sort all of it — while holding the
  mutex, so other `GetMempoolTx` callers stalled behind it, and even when the
  mempool was empty and the response would be empty too.

## [0.5.0] - 2026-07-26

### Added

- Add the `grpc_server_connections_current` Prometheus gauge for active gRPC
  client connections.

- Add Ironwood (NU6.3) support: compact block, tree state, and subtree
  root data for the Ironwood pool, and parsing of ZIP 229 v6 transactions
  using the finalized NU6.3 consensus IDs. Empty `poolTypes` requests now
  include Ironwood shielded data. The darkside test framework tracks
  Ironwood commitment tree sizes and exposes
  `startIronwoodCommitmentTreeSize` via `DarksideMetaState`.

### Changed

- Update to [zcash/lightwallet-protocol v0.5.0](https://github.com/zcash/lightwallet-protocol/releases/tag/v0.5.0),
  which adds the Ironwood fields and removes the (never used)
  `CompactBlock.protoVersion` field; the field number and name are now
  reserved.

### Fixed

- Bound the transparent-address gRPC methods against unbounded per-request
  work (GHSA-x4m7-3gpp-xc36). `GetTaddressBalanceStream` now caps the number of
  streamed addresses and validates each one as it arrives, instead of buffering
  the entire client stream before any validation; `GetAddressUtxos` and
  `GetAddressUtxosStream` cap the number of requested addresses; and
  `GetTaddressTransactions` defaults a missing range `End` to the current chain
  tip instead of scanning open-endedly, bounds the range span, and applies a
  deadline that also covers the backend calls. Previously an unauthenticated
  client could hold a stream open and send `Address` messages indefinitely —
  growing the server's heap until the process was OOM-killed, disconnecting all
  connected wallets — or name enough addresses (or request an open-ended
  address-index scan) to force zcashd into unbounded work.

- Bound element counts in the block and transaction parser against the
  remaining input before allocating. A malformed or truncated serialization
  can declare far more elements (transaction inputs and outputs, Sapling
  spends and outputs, Orchard actions, JoinSplits, and the block's
  transaction count) than the input could possibly contain; sizing a slice
  from such a count previously allocated gigabytes before the first element
  failed to parse. The parser only consumes data supplied by the configured
  backend node (or by the opt-in darkside testing facility), so this is
  defensive hardening rather than a remotely reachable defect (#562).

- Make `common.RawRequest` context-aware so cancelled `GetBlockRange` /
  `GetBlockRangeNullifiers` streams abort in-flight zcashd JSON-RPC calls
  instead of holding a goroutine and RPC connection until the request
  completes.

- Call `setLatestHash()` during startup, to ensure the chain is correct
  if a reorg occurred while we were down (#563).

- `GetMempoolTx` no longer crashes when a transaction leaves the mempool
  between the `getrawmempool` and `getrawtransaction` calls.

- `GetBlockRangeNullifiers` no longer includes transparent inputs and
  outputs (`vin`/`vout`), consistent with `GetBlockNullifiers` and with
  the documented behavior of the nullifier RPCs.

- Darkside: `ClearAllTreeStates` now also clears the by-hash index, so
  cleared tree states are no longer retrievable by block hash;
  `RemoveTreeState` no longer crashes when the requested tree state does
  not exist; `GetSubtreeRoots` with `maxEntries` of 0 now returns all
  remaining roots instead of none.

## [0.4.19] - 2026-03-30

### Added

- Add support for transparent transactions.

- Add `poolType` argument to `GetBlockRange` and `GetMempoolTx`. This
  filtering allows the caller to request specific components (transparent,
  shielded, or a combination) of blocks (`GetBlockRange`) and transactions
  (`GetMempoolTx`).

### Changed

- `GetBlock` result now includes transparent transaction data.

- If corruption is detected in the cache file, the cache is rebuilt
  completely (instead of attempting an incremental correction). The
  `lightwalletd` service remains available during the rebuild.

## [0.4.18] - 2025-06-01

### Added

- Add debug logging to gRPC entry and exit points.

- Add smoke test

- lightwalletd node operators can export a donation address in the
  GetLightdInfo gRPC.

- Add the ability to not create and maintain a compact block cache.

### Changed

- The `RawTransaction` values returned from a call to `GetMempoolStream`
  now report a `Height` value of `0`, in order to be consistent with
  the results of calls to `GetTransaction`. See the documentation of
  `RawTransaction` in `walletrpc/service.proto` for more details on
  the semantics of this field.

### Fixed

- GetLatestBlock should report latest block hash in little-endian
  format, not big-endian.

- Support empty block range end in `getaddresstxids` calls.

- Filter out mined transactions in `refreshMempoolTxns`

- Uniformly return height 0 for mempool `RawTransaction` results.

- Reduce lightwalletd startup time.

- Parsing of `getrawtransaction` results is now platform-independent.
  Previously, values of `-1` returned for the transaction height would
  be converted to different `RawTransaction.Height` values depending
  upon whether `lightwalletd` was being run on a 32-bit or 64-bit 
  platform.

## [Prior Releases]

This changelog was not created until after the release of v0.4.17
