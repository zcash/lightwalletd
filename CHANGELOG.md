# Changelog
All notable changes to this library will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this library adheres to Rust's notion of
[Semantic Versioning](https://semver.org/spec/v2.0.0.html).
The most recent changes are listed first.

## [Unreleased]

### Fixed

- Bound the transparent-address gRPC methods against unbounded per-request
  work (GHSA-x4m7-3gpp-xc36). `GetTaddressBalanceStream` now caps the number of
  streamed addresses and validates each one as it arrives, instead of buffering
  the entire client stream before any validation; `GetAddressUtxos` and
  `GetAddressUtxosStream` cap the number of requested addresses. Previously an
  unauthenticated client could hold a stream open and send `Address` messages
  indefinitely — growing the server's heap until the process was OOM-killed,
  disconnecting all connected wallets — or name enough addresses to force
  zcashd to materialize an unbounded result set.

- Make `common.RawRequest` context-aware so cancelled `GetBlockRange` /
  `GetBlockRangeNullifiers` streams abort in-flight zcashd JSON-RPC calls
  instead of holding a goroutine and RPC connection until the request
  completes.

### Added

- Add debug logging to gRPC entry and exit points.

- Add the `grpc_server_connections_current` Prometheus gauge for active gRPC
  client connections.

- Add smoke test

- lightwalletd node operators can export a donation address in the
  GetLightdInfo gRPC.

- Add the ability to not create and maintain a compact block cache.

- Add support for transparent transactions.

- Add `poolType` argument to `GetBlockRange` and `GetMempoolTx`. This
  filtering allows the caller to request specific components (transparent,
  shielded, or a combination) of blocks (`GetBlockRange`) and transactions
  (`GetMempoolTx`).

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

- The `RawTransaction` values returned from a call to `GetMempoolStream`
  now report a `Height` value of `0`, in order to be consistent with
  the results of calls to `GetTransaction`. See the documentation of
  `RawTransaction` in `walletrpc/service.proto` for more details on
  the semantics of this field.

- `GetBlock` result now includes transparent transaction data.

- If corruption is detected in the cache file, the cache is rebuilt
  completely (instead of attempting an incremental correction). The
  `lightwalletd` service remains available during the rebuild.

### Fixed

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
