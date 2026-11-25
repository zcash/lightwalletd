# Changelog
All notable changes to this library will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this library adheres to Rust's notion of
[Semantic Versioning](https://semver.org/spec/v2.0.0.html).
The most recent changes are listed first.

## [0.6.3] - 2026-11-24

### Added

- Lightwalletd bounds active streaming gRPCs across connections with
  `--max-concurrent-stream-rpcs` (default 400, zero disables it). Both block-range
  RPCs use their existing `--max-concurrent-blockrange` limit. Setting that flag
  to zero disables their server-wide limit. The optional
  `--max-concurrent-stream-rpcs-per-ip` cap defaults to zero (disabled). If
  enabled, it uses the immediate TCP peer IP, so clients behind the same proxy
  or shared address count toward one per-IP limit (#628).

### Fixed

- Transparent balance and UTXO RPCs now report unclassified backend failures as
  `Unknown` instead of returning a successful zero balance or empty UTXO result.
  This applies to unary and streaming variants; the existing `InvalidArgument`
  and `NotFound` mappings are unchanged.

  `GetTransaction` now reserves `NotFound` for the backend's structured
  missing-transaction response. Other backend failures return `Unknown`;
  canceled and timed-out backend requests retain their context status.

- A `GetMempoolStream` call that was waiting on a slow client could panic if
  the mempool list was reset twice meanwhile and the tip hash was the same
  again. In 0.6.1 that ended the process; in 0.6.2 it left a lock held, so
  every later `GetMempoolStream` call blocked until a restart
  (GHSA-fhv5-hm8v-q7fw).

- The limit on repeated request fields checked before decoding now also
  covers `poolTypes` in the `range` of a `TransparentAddressBlockFilter`
  (`GetTaddressTxids`, `GetTaddressTransactions`), which the 0.6.2 fix
  missed (GHSA-jjjw-r66r-mjgf).

- `GetTaddressTransactions` now aborts before sending a transaction that is no
  longer mined in the requested range. Clients can restart the request when
  address-index results and later transaction lookups observe different chain
  state.

## [0.6.2] - 2026-11-17

### Added

- `--client-stream-idle-timeout` (seconds, default 30) cancels a
  client-streaming RPC whose client has sent nothing for that long, whether
  before the first message or between messages. Without it, a client that
  opens `GetTaddressBalanceStream` and sends no address parks a handler
  goroutine indefinitely, so enough open streams exhaust memory. Set it to 0
  to disable (GHSA-5xg7-x58j-m5vr, GHSA-4wjx-vhhv-vc8c).

- `--max-concurrent-recv` bounds how many requests the server receives at
  once across every connection; the default is 32. gRPC reads a whole request
  into memory before any handler runs, so many large requests arriving at
  once could exhaust memory. A request over the limit is refused with
  `UNAVAILABLE` before its body is read, and one whose body hasn't arrived
  within 20 seconds is canceled. Client-streaming calls, including gRPC
  reflection, count against a separate pool of the same size for as long as
  they're open. Set it to 0 for no limit (GHSA-xrx5-jc6q-9h8r).

  When a pool is full, a new request can take the oldest slot of the
  connection holding the most, so one connection can't lock out the rest.
  Clients spread over many connections still can, so a per-IP connection
  limit at a proxy or firewall is a good idea.

- `--max-concurrent-sendtx-mib` bounds the transaction data, in MiB, that
  `SendTransaction` works on at once; the default is 16. Each transaction is
  copied several times on its way to the backend. Requests over the limit are
  refused with `UNAVAILABLE`. Set it to 0 for no limit
  (GHSA-xrx5-jc6q-9h8r).

### Changed

- Repeated `poolTypes` entries in a `GetBlockRange`, `GetBlockRangeNullifiers`
  or `GetMempoolTx` request are now ignored after the first, so the server's
  filtering cost no longer grows with the length of the list
  (GHSA-8vh8-6jg3-8f5v).

- The largest request lightwalletd accepts is now 2 MiB, down from gRPC's
  default of 4 MiB. That still fits the largest transaction
  `SendTransaction` accepts. This doesn't apply in darkside mode, and neither
  does `--max-concurrent-recv`.

- A `GetMempoolTx` mempool refresh that fails no longer holds off other
  callers' refreshes for two seconds. A client that cancels, or stops
  reading, no longer stops the mempool view from being refreshed. A
  transaction that fails to parse is skipped rather than failing every
  refresh until it leaves the mempool (GHSA-w64p-hjp5-3mj7).

- `GetMempoolStream` no longer holds its shared lock while calling the
  backend, so a slow backend no longer stalls every other stream client or
  keeps them from noticing a cancel. A client that cancels no longer stops
  the refresh it started, and a transaction that fails to parse is skipped
  rather than failing every refresh until the next block
  (GHSA-hp3q-vfr4-v8m6, GHSA-pj5c-jx29-gmxr).

### Fixed

- A request with a huge number of list elements is now refused before it is
  decoded. An empty element takes 2 bytes on the wire but 16 to 24 bytes once
  decoded, so a single 4 MiB request could cost almost 90 MiB before the
  per-request limits on addresses or excluded txids could reject it, and a
  few at once could exhaust the server's memory. The limits checked before
  decoding are twice the existing ones, so a request just over a limit still
  gets the same `InvalidArgument` as before (GHSA-p4pq-3cf7-45rv).

- `GetMempoolTx` no longer sorts the shared mempool list in place. Concurrent
  callers each read that list without holding the lock, so the in-place sort let
  one caller reorder it under another, which could send a transaction twice and
  skip another. `MempoolFilter` now works on copies and leaves its arguments
  untouched.

- The Prometheus metrics HTTP listener (`--http-bind-addr`) now has request
  timeouts: 10 seconds for a request's headers to arrive, 30 seconds for the
  whole request and for the response, and 60 seconds idle. Before, a peer
  that sent a partial request held its connection, and a file descriptor,
  for as long as it liked, and enough of them starved the gRPC listener in
  the same process (GHSA-w24w-x7vw-f2w8). A failure to start the listener is
  now logged instead of ignored.

  The listener also uses its own handler mux, so it no longer serves
  `/debug/vars`, which exposed the command line, including an RPC password
  given there (GHSA-j79f-98gc-33jw).

- A block with no transactions, or whose first transaction has no
  transparent input, is now rejected by the parser instead of crashing the
  process when its height is read. Consensus requires a coinbase with one
  input, so only darkside mode or a misbehaving backend could supply such a
  block (GHSA-56hr-g827-v3rj, GHSA-jcrq-vv43-968c).

- Darkside `AddAddressUtxo` now rejects a transaction ID that isn't 32 bytes
  with `InvalidArgument` instead of crashing (GHSA-pp3p-5vmm-3cvw).

- A panic in a gRPC handler now fails that one request with `Internal` and
  logs a stack trace, instead of ending the process.

- Transparent-address validation compiles its regular expression once
  instead of on every address, so a request with many addresses no longer
  costs thousands of compilations (GHSA-x943-ghgq-v675).

- The startup debug log line that prints the parsed options no longer
  includes the backend RPC password (CodeQL alert #4).

### Security

- Built with Go 1.27.4, which fixes an HTTP/1 server crash in `net/http`
  (GO-2026-6859). Only lightwalletd's HTTP listener uses HTTP/1, and it binds
  to localhost by default. Also updated `golang.org/x/net` to v0.62.0, which
  fixes two issues in its `html` package (GO-2026-6860, GO-2026-6866).
  lightwalletd doesn't use that package.

- Updated `golang.org/x/net` to v0.60.0, which fixes five HTTP/2 server
  vulnerabilities reachable through gRPC (GO-2026-6603, GO-2026-6610,
  GO-2026-6611, GO-2026-6612, GO-2026-6617). Two of them let a client crash
  the process or exhaust its memory (GHSA-gq9m-j9cv-6w9p).

## [0.6.1] - 2026-10-27

### Added

- `--grpc-max-concurrent-streams` bounds how many gRPC streams one client
  connection may have open at once; the default is 100. There was previously
  no limit, and gRPC omits `SETTINGS_MAX_CONCURRENT_STREAMS` from the
  connection preface when none is set, announcing to every client that it may
  open as many streams as it likes. Set it to 0 to restore the old unlimited
  behavior (GHSA-xq7v-jj38-pcwr).

- `--max-concurrent-blockrange` bounds how many block-range requests the
  server will work on at once across every connection, where
  `--grpc-max-concurrent-streams` bounds only a single one; the default is
  200. Requests beyond the limit wait for a free slot rather than failing, up
  to the sooner of their own deadline and ten seconds, and are then refused
  with `UNAVAILABLE`. Set it to 0 for no limit (GHSA-xq7v-jj38-pcwr).

- New `GetStandardFee` gRPC exposing the backend's `getstandardfee` JSON-RPC
  method (zebrad, zakura). Returns the ZIP 317 marginal fee in zatoshis per
  logical action and the backend fee-estimator version (0 is the static ZIP
  317 fee, lowered to 1000 zatoshis by ZIP 2009). Backends without the
  method return `Unimplemented`, other backend failures `Unavailable`. Part
  of `lightwallet-protocol` v0.6.0.

- A streaming RPC whose client stops reading for 60 seconds now ends with
  `DEADLINE_EXCEEDED`. Before, a client that opened streams and never read
  them held their resources, including the block-range slots above, for as
  long as it kept the connection open. A wallet that stalls that long
  reconnects and resumes from where it left off.

### Changed

- Where there is a cgroup memory limit (a container run with `--memory`, or a
  systemd unit with `MemoryMax`), lightwalletd now sets Go's soft memory limit
  to 90% of it. The Go runtime does not read cgroup limits by itself, so a
  burst of allocation could be OOM-killed at a heap size the collector was
  content to let grow. Setting `GOMEMLIMIT` in the environment overrides this.

### Fixed

- CI: the release step that mirrors the image to `zodlinc/lightwalletd`
  failed on v0.6.0 with a 401, because the zodlinc token can't read
  `electriccoinco`. It now pulls the image before logging in as zodlinc.
  (#625)

## [0.6.0] - 2026-10-20

### Security

- Built with Go 1.27.3 (module toolchain; Go 1.26.10 or newer required). These Go releases
  distrust the compromised eMudhra intermediate CA keys in `crypto/x509` (CVE-2026-111943,
  https://go.dev/issue/82345). Older lightwalletd binaries accept TLS chains through those
  intermediates and should be replaced with this release. CI and the Docker release build are
  pinned to Go 1.27.3. (#624)

No functional changes since v0.5.5.

## [0.5.5] - 2026-10-10

### Changed

- The block ingestor no longer exits when `getbestblockhash` fails. A single
  failed call (for example HTTP 429 "Too many connections" from a loaded
  zebrad or zakura backend) used to call `Log.Fatal`, dropping every
  in-flight client stream; replicas sharing one backend then exited and
  reconnected together, adding to the overload. It now logs a warning and
  retries with a capped exponential backoff (1s to 30s), raises the log to
  Error after 5 minutes of continuous failures, and logs once when the RPC
  recovers. Malformed replies stay fatal. (#604)
- The Docker image is built in a separate stage and ships only the static
  binary on `debian:13-slim` (plus `ca-certificates`), instead of the full
  `golang:1.25` build image. The image drops from 1.9 GB to 193 MB and from
  1890 to 135 scanner findings. The uid 2002 `lightwalletd` user, data
  directory ownership, `/bin/bash` and entrypoint are unchanged. The runtime
  image no longer contains `curl`, `git`, `perl` or the Go toolchain.

### Added

- Gauge `lightwalletd_getbestblockhash_last_success_timestamp_seconds`, the
  time of the last successful `getbestblockhash`. Since failures are now
  retried instead of exiting, a lightwalletd whose backend keeps failing
  serves a stale tip; this lets operators alert on it. (#605)

### Security

- `golang.org/x/text` to v0.41.0 (CVE-2026-56852, denial of service via
  invalid UTF-8).
- `golang.org/x/net` to v0.57.0 (CVE-2026-46600, denial of service in
  `dns/dnsmessage`).
- `golang.org/x/crypto` to v0.55.0 (CVE-2026-56854). GO-2026-5932
  (`x/crypto/openpgp` unmaintained) remains; it has no fixed version and
  lightwalletd does not import the package directly.
- `google.golang.org/grpc` to v1.83.2.

### Fixed

- CI: the release step that mirrors the image to `zodlinc/lightwalletd`
  had never completed; it now authenticates to the destination only and
  uses `crane copy` correctly.

## [0.5.4] - 2026-08-27

### Changed

- `GetAddressUtxos` and `GetAddressUtxosStream` now pass `startHeight` and
  `maxEntries` to the backend `getaddressutxos` RPC, instead of only applying
  them to the reply. No backend implements the arguments yet, and a backend
  that doesn't implement them ignores the extra JSON keys, so on its own this
  changes nothing: the client-side filter stays, and correctness never depends
  on the backend honoring them. It is the lightwalletd half of moving the
  limits to where the work is done, which is what the unremediated part of
  GHSA-x4m7-3gpp-xc36 needs -- today a request naming a single address with a
  large UTXO set makes the node produce that entire set, however narrow a
  height range or however few entries the client asked for. A request that
  sets neither argument is serialized exactly as it was before.

### Fixed

- `GetSubtreeRoots` now rejects an unrecognized `ShieldedProtocol` with
  `InvalidArgument` instead of `Unknown`. It was the one production handler
  returning a bare Go error, which gRPC surfaces as `codes.Unknown` -- the code
  a client reads as "the server had a problem" and backs off and retries on,
  for a request that cannot succeed until the caller changes it. The darkside
  `Reset` handler's `ChainName` check returned a bare error the same way, four
  lines below its `BranchID` check, which already used `InvalidArgument`.

- `GetTaddressBalance` now rejects an address list longer than the same 10,000
  limit that `GetTaddressBalanceStream`, `GetAddressUtxos` and
  `GetAddressUtxosStream` already enforce. The unary method takes its whole
  list in one protobuf message, so its only bound was gRPC's 4MB
  `MaxRecvMsgSize` — roughly 113,000 addresses at 37 wire bytes each, an order
  of magnitude above the intended cap. A single request could therefore ask
  the backend for eleven times more address-index lookups than any other
  method allows, and return only an 8-byte balance (GHSA-x4m7-3gpp-xc36).

- Report `lightwalletProtocolVersion` in `GetLightdInfo`. The field was added
  to `LightdInfo` in lightwallet-protocol v0.4.0, in the same release as
  `BlockRange.poolTypes`, and is the signal clients are required to check
  before requesting non-default pool types. lightwalletd never populated it,
  so it was always the empty string, and a client following that rule could
  not tell a server that serves transparent and Ironwood data inside compact
  blocks from one that does not. Correctly-behaving clients therefore had to
  fall back to shielded-only scanning, or rely on an out-of-band assertion
  from the operator, against servers that had supported the data since 0.5.0.
  The value is a constant tracking the vendored `lightwallet-protocol`
  subtree, currently v0.5.0, and is not overwritten by the build.

- `GetTaddressBalance` and `GetTaddressBalanceStream` no longer report an
  inflated balance when the client names the same address more than once.
  zcashd's `getaddressbalance` sums over the list entries, so a repeated
  address was counted repeatedly; a doubled address returned exactly twice the
  correct balance. zebrad collapses duplicates before querying, so the same
  request returned different numbers depending on which backend lightwalletd
  was configured with. The address list is now deduplicated before the backend
  call, so the reported balance is the balance of the distinct addresses named,
  on any backend.

- `GetAddressUtxos` and `GetAddressUtxosStream` now collapse repeated
  addresses before calling the backend, so a request naming one address n times
  no longer asks the backend to look it up n times and return n copies of its
  UTXOs. Against zcashd that was a real multiplier — repeating an address is
  free to the caller, since addresses are public and neither a key nor any
  funds are needed to name one, so a single request could multiply the busiest
  address on the chain by the 10,000-address limit. zebrad already collapses
  duplicates before querying its state, so it was never exposed to this;
  lightwalletd no longer depends on the backend to do it. This narrows the
  worst case rather than closing it: `StartHeight` and `MaxEntries` remain
  response filters applied after the whole backend result is materialized, so
  the UTXO set of even a single named address is still fetched in full
  (GHSA-x4m7-3gpp-xc36).

## [0.5.3] - 2026-08-04

### Fixed

- `GetBlockRange` and `GetBlockRangeNullifiers` now check that each block they
  return connects to the one before it, and fail the stream with `Aborted`
  rather than send a block that doesn't (GHSA-m7j5-wvx3-qj6j). Every height in
  the range is resolved independently, from the block cache when it's present
  there and from the backend node otherwise, so while the background ingestor
  was still repairing a reorg, a single request spanning the cache tip could
  return a block from the abandoned fork followed by one from the current
  fork. That pair violates `block[n+1].prevHash == block[n].hash` and
  describes a chain that cannot exist, which a strict wallet will reject and a
  lenient one may briefly index against. Note that this makes each response
  internally consistent; a range served entirely from the not-yet-repaired
  part of the cache still returns blocks from the abandoned fork, until the
  ingestor rewinds past them.

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
