# PIR Client Integration Guide

This document describes the complete end-to-end architecture for integrating PIR (Private Information Retrieval) into Zcash wallet clients like Zashi.

## Architecture Overview

```
┌─────────────────────────────────────────────────────────────────────────────────┐
│                           PRODUCTION ARCHITECTURE                                │
└─────────────────────────────────────────────────────────────────────────────────┘

┌──────────────────────────────────────────────────────────────────────────────────┐
│                              WALLET CLIENT (Zashi)                               │
│  ┌─────────────────────────────────────────────────────────────────────────────┐ │
│  │                         Swift / Kotlin App Layer                            │ │
│  │                                                                             │ │
│  │   • UI/UX                                                                   │ │
│  │   • Wallet state management                                                 │ │
│  │   • Sync orchestration                                                      │ │
│  │   • Balance display                                                         │ │
│  └─────────────────────────────────────────────────────────────────────────────┘ │
│                                      │                                           │
│                                      ▼                                           │
│  ┌─────────────────────────────────────────────────────────────────────────────┐ │
│  │                    nullifier-crypto (Rust FFI via UniFFI)                   │ │
│  │                                                                             │ │
│  │   CRYPTO-ONLY OPERATIONS (no network I/O):                                  │ │
│  │   • compute_cuckoo_buckets(nullifier, seed, num_buckets) → (idx1, idx2)     │ │
│  │   • precompute_keys(params) → CryptoState                                   │ │
│  │   • generate_query(state, bucket_idx) → (QueryState, query_bytes)           │ │
│  │   • decrypt_response(query_state, response) → bucket_data                   │ │
│  │   • search_bucket(bucket_data, fingerprint) → Option<SpentInfo>             │ │
│  └─────────────────────────────────────────────────────────────────────────────┘ │
│                                      │                                           │
└──────────────────────────────────────┼───────────────────────────────────────────┘
                                       │
                              gRPC (TLS)│
                                       │
┌──────────────────────────────────────┼───────────────────────────────────────────┐
│                              LIGHTWALLETD                                        │
│                                      │                                           │
│  ┌───────────────────────────────────┴─────────────────────────────────────────┐ │
│  │                           gRPC Service Layer                                │ │
│  │                                                                             │ │
│  │   EXISTING ENDPOINTS:                    NEW PIR ENDPOINTS:                 │ │
│  │   • GetLatestBlock()                     • GetPirParams() ────────┐         │ │
│  │   • GetBlock()                           • GetPirStatus()         │         │ │
│  │   • GetBlockRange()                      • YpirQuery() ───────────┤         │ │
│  │   • GetTransaction()                     • InspireQuery() ────────┤         │ │
│  │   • SendTransaction()                                             │         │ │
│  │   • GetBlockRangeNullifiers() ◄───────── (trial decrypt fallback) │         │ │
│  └───────────────────────────────────────────────────────────────────┼─────────┘ │
│                                                                      │           │
│  ┌───────────────────────────────────────────────────────────────────┼─────────┐ │
│  │                         PIR Client (pirclient)                    │         │ │
│  │                                                                   │         │ │
│  │   HTTP client that proxies PIR requests to the PIR service        │         │ │
│  │   • Handles JSON/binary serialization                             │         │ │
│  │   • Connection pooling                                            │         │ │
│  │   • Timeout management                                            │         │ │
│  └───────────────────────────────────────────────────────────────────┼─────────┘ │
│                                                                      │           │
│  ┌───────────────────────────────────────────────────────────────────┼─────────┐ │
│  │                      Nullifier Extractor                          │         │ │
│  │                                                                   │         │ │
│  │   • Extracts Orchard nullifiers from new blocks                   │         │ │
│  │   • Sends to PIR service for ingestion                            │         │ │
│  │   • Handles reorgs                                                │         │ │
│  └───────────────────────────────────────────────────────────────────┼─────────┘ │
│                                                                      │           │
└──────────────────────────────────────────────────────────────────────┼───────────┘
                                                                       │
                                                              HTTP (TLS)│
                                                                       │
┌──────────────────────────────────────────────────────────────────────┼───────────┐
│                              PIR SERVICE (nullifier-pir)             │           │
│                                                                      │           │
│  ┌───────────────────────────────────────────────────────────────────┴─────────┐ │
│  │                              HTTP API Server                                │ │
│  │                                                                             │ │
│  │   INGESTION:                           QUERY:                               │ │
│  │   POST /nullifiers/ingest              GET  /pir/params/ypir                │ │
│  │   POST /nullifiers/reorg               GET  /pir/params/inspire             │ │
│  │                                        POST /pir/query/ypir                 │ │
│  │   STATUS:                              POST /pir/query/inspire              │ │
│  │   GET  /health                         POST /pir/query/binary               │ │
│  │   GET  /pir/status                                                          │ │
│  └─────────────────────────────────────────────────────────────────────────────┘ │
│                                      │                                           │
│  ┌───────────────────────────────────┴─────────────────────────────────────────┐ │
│  │                           PIR Database Engine                               │ │
│  │                                                                             │ │
│  │   • Cuckoo hash table (2 buckets per nullifier, 8 entries per bucket)       │ │
│  │   • YPIR protocol (zero-setup, ~2MB queries)                                │ │
│  │   • InsPIRe protocol (preprocessed, ~19KB queries)                          │ │
│  │   • Hot-swap rebuild (zero-downtime updates)                                │ │
│  │   • ~52M nullifiers indexed                                                 │ │
│  └─────────────────────────────────────────────────────────────────────────────┘ │
│                                                                                  │
└──────────────────────────────────────────────────────────────────────────────────┘
```

## Why This Architecture?

### Privacy Requirement: Client-Side Crypto

PIR provides privacy by ensuring the server cannot learn which nullifier the client is looking up. This requires:

1. **Query generation on client** - The encrypted query hides which bucket is being requested
2. **Response decryption on client** - Only the client can decrypt the answer

If lightwalletd generated queries, it would know which nullifiers the client is checking - defeating the privacy purpose.

### Recommended: Crypto-Only FFI Layer

```
┌─────────────────────────────────────────────────────────────────────────────────┐
│                    WHY CRYPTO-ONLY FFI (not full HTTP client)                   │
└─────────────────────────────────────────────────────────────────────────────────┘

OPTION A: Full nullifier-client via FFI
─────────────────────────────────────────
  Wallet ──► nullifier-client (Rust) ──► PIR Service (HTTP direct)

  Problems:
  • Bypasses lightwalletd (inconsistent traffic patterns)
  • Requires separate network configuration
  • Harder to add rate limiting, caching, observability
  • Binary size includes HTTP stack (~2MB extra)


OPTION B: Crypto-only FFI + gRPC transport (RECOMMENDED)
─────────────────────────────────────────────────────────
  Wallet ──► nullifier-crypto (Rust FFI) ──► lightwalletd (gRPC) ──► PIR Service

  Benefits:
  ✓ All traffic through single endpoint (lightwalletd)
  ✓ Consistent with existing wallet architecture
  ✓ Operators can manage PIR service independently
  ✓ Smaller binary (~1MB vs ~3MB)
  ✓ Native Swift/Kotlin networking (better for mobile)
```

## Data Flow: Checking a Nullifier

```
┌─────────────────────────────────────────────────────────────────────────────────┐
│                    COMPLETE NULLIFIER CHECK FLOW                                 │
└─────────────────────────────────────────────────────────────────────────────────┘

WALLET STARTUP
══════════════

    ┌────────────────┐
    │ Load wallet DB │
    │ Known notes:   │
    │ • Note A       │
    │ • Note B       │
    │ • Note C       │
    │ lastSync: 900  │
    └───────┬────────┘
            │
            ▼
    ┌────────────────────────────────────────┐
    │ gRPC: GetPirParams()                   │
    │                                        │
    │ Response:                              │
    │   pirReady: true                       │
    │   pirCutoffHeight: 995                 │
    │   cuckooParams:                        │
    │     numBuckets: 6,553,600              │
    │     bucketSize: 8                      │
    │     hashSeed: 0x1234...                │
    │   ypirParams: { numRows, numCols, ... }│
    │   inspireParams: { ... }               │
    └───────┬────────────────────────────────┘
            │
            ▼
    ┌────────────────────────────────────────┐
    │ Decision: lastSyncHeight < pirCutoff?  │
    │           900 < 995? YES               │
    │                                        │
    │ → Use PIR for known notes              │
    └───────┬────────────────────────────────┘
            │
            ▼

STEP 1: PRECOMPUTE KEYS (one-time, ~10-20 seconds)
══════════════════════════════════════════════════

    ┌────────────────────────────────────────┐
    │ FFI: precompute_keys(ypirParams)       │
    │                                        │
    │ This is expensive but done once per    │
    │ session. Can be cached to disk.        │
    │                                        │
    │ Returns: CryptoState (opaque handle)   │
    └───────┬────────────────────────────────┘
            │
            ▼

STEP 2: FOR EACH NULLIFIER TO CHECK
═══════════════════════════════════

    ┌────────────────────────────────────────┐
    │ nullifier = Note A's nullifier         │
    │           = 0x1a2b3c...                │
    └───────┬────────────────────────────────┘
            │
            ▼
    ┌────────────────────────────────────────┐
    │ FFI: compute_cuckoo_buckets(           │
    │        nullifier,                      │
    │        hashSeed,                       │
    │        numBuckets                      │
    │      )                                 │
    │                                        │
    │ Returns: (bucket_idx_1, bucket_idx_2)  │
    │          e.g., (42, 1337)              │
    └───────┬────────────────────────────────┘
            │
            ▼
    ┌────────────────────────────────────────┐
    │ For each bucket index:                 │
    │                                        │
    │ FFI: generate_query(                   │
    │        cryptoState,                    │
    │        bucket_idx_1                    │
    │      )                                 │
    │                                        │
    │ Returns:                               │
    │   queryState: opaque handle for decrypt│
    │   queryBytes: ~2MB (YPIR)              │
    │              or ~19KB (InsPIRe)        │
    └───────┬────────────────────────────────┘
            │
            ▼
    ┌────────────────────────────────────────┐
    │ gRPC: YpirQuery(queryBytes)            │
    │    or InspireQuery(queryBytes)         │
    │                                        │
    │ lightwalletd proxies to PIR service    │
    │                                        │
    │ Returns: responseBytes (~1MB)          │
    └───────┬────────────────────────────────┘
            │
            ▼
    ┌────────────────────────────────────────┐
    │ FFI: decrypt_response(                 │
    │        queryState,                     │
    │        responseBytes                   │
    │      )                                 │
    │                                        │
    │ Returns: bucketData (8 entries × 14B)  │
    └───────┬────────────────────────────────┘
            │
            ▼
    ┌────────────────────────────────────────┐
    │ FFI: search_bucket(                    │
    │        bucketData,                     │
    │        nullifier_fingerprint           │
    │      )                                 │
    │                                        │
    │ fingerprint = first 8 bytes of nullifier│
    │                                        │
    │ Returns: Option<SpentInfo>             │
    │   Some({ block_height: 920,            │
    │          tx_index: 3 })                │
    │   or None                              │
    └───────┬────────────────────────────────┘
            │
            ▼
    ┌────────────────────────────────────────┐
    │ If found in bucket 1: SPENT            │
    │ If not: check bucket 2 same way        │
    │ If neither: NOT SPENT (in PIR range)   │
    └────────────────────────────────────────┘
```

## Protocol Choice: YPIR vs InsPIRe

```
┌─────────────────────────────────────────────────────────────────────────────────┐
│                    PROTOCOL COMPARISON                                           │
└─────────────────────────────────────────────────────────────────────────────────┘

                        YPIR                          InsPIRe
                        ────                          ───────
Query size:             ~2 MB                         ~19 KB
Response size:          ~1 MB                         ~43 KB
Query generation:       ~10 ms                        ~600 ms
Decryption time:        ~50 ms                        ~14 ms
Total E2E:              ~60 ms                        ~614 ms
Preprocessing:          None (zero-setup)             Required (silent setup)

MOBILE RECOMMENDATION:
──────────────────────
• WiFi: Either protocol acceptable
• Cellular: InsPIRe strongly preferred (100x smaller uploads)
• Metered data: InsPIRe essential

DEFAULT: InsPIRe for mobile clients
        YPIR as fallback if InsPIRe unavailable
```

## FFI API Design

The `nullifier-crypto` crate exposes these functions via UniFFI:

```rust
// Rust FFI interface (nullifier-crypto)

/// Parameters received from GetPirParams() gRPC call
pub struct PirParams {
    pub cuckoo_seed: u64,
    pub num_buckets: usize,
    pub bucket_size: usize,
    pub ypir_params: Option<YpirParams>,
    pub inspire_params: Option<InspireParams>,
}

/// Spent transaction info
pub struct SpentInfo {
    pub block_height: u64,
    pub tx_index: u16,
}

/// Opaque crypto state (holds precomputed keys)
pub struct CryptoState { /* internal */ }

/// Opaque query state (needed for decryption)
pub struct QueryState { /* internal */ }

// ═══════════════════════════════════════════════════════════════════════════════
// PURE FUNCTIONS (no state, no I/O)
// ═══════════════════════════════════════════════════════════════════════════════

/// Compute Cuckoo hash bucket indices for a nullifier
/// Returns two bucket indices that may contain this nullifier
pub fn compute_cuckoo_buckets(
    nullifier: &[u8; 32],
    hash_seed: u64,
    num_buckets: usize,
) -> (usize, usize);

/// Extract fingerprint from nullifier (first 8 bytes)
pub fn compute_fingerprint(nullifier: &[u8; 32]) -> [u8; 8];

/// Search bucket data for a matching fingerprint
/// Returns SpentInfo if found, None otherwise
pub fn search_bucket(
    bucket_data: &[u8],
    fingerprint: &[u8; 8],
    bucket_size: usize,
) -> Option<SpentInfo>;

// ═══════════════════════════════════════════════════════════════════════════════
// STATEFUL OPERATIONS (crypto state management)
// ═══════════════════════════════════════════════════════════════════════════════

/// Initialize crypto state and precompute keys
/// This is expensive (~10-20 seconds) - do once per session
pub fn precompute_keys(params: &PirParams) -> Result<CryptoState, Error>;

/// Generate a PIR query for a specific bucket
/// Returns query bytes and state needed for decryption
pub fn generate_query(
    state: &mut CryptoState,
    bucket_idx: usize,
) -> Result<(QueryState, Vec<u8>), Error>;

/// Decrypt PIR response using query state
/// Returns raw bucket data
pub fn decrypt_response(
    query_state: QueryState,
    response: &[u8],
) -> Result<Vec<u8>, Error>;

// ═══════════════════════════════════════════════════════════════════════════════
// CONVENIENCE WRAPPER
// ═══════════════════════════════════════════════════════════════════════════════

/// High-level: Check if a nullifier is spent
/// Handles both bucket queries internally
/// Returns SpentInfo if spent, None if not found in PIR database
pub fn check_nullifier(
    state: &mut CryptoState,
    nullifier: &[u8; 32],
    // Callback to execute gRPC query (provided by Swift/Kotlin)
    execute_query: impl Fn(&[u8]) -> Result<Vec<u8>, Error>,
) -> Result<Option<SpentInfo>, Error>;
```

## Swift Integration Example

```swift
import NullifierCrypto  // UniFFI-generated bindings
import GRPC

class PirNullifierChecker {
    private var cryptoState: CryptoState?
    private let grpcClient: LightwalletServiceClient

    init(grpcClient: LightwalletServiceClient) {
        self.grpcClient = grpcClient
    }

    /// Initialize PIR (call once on wallet startup)
    func initialize() async throws {
        // 1. Fetch parameters from lightwalletd
        let params = try await grpcClient.getPirParams(GetPirParamsRequest())

        guard params.pirReady else {
            throw PirError.notReady
        }

        // 2. Convert gRPC params to FFI params
        let pirParams = PirParams(
            cuckooSeed: params.cuckooParams.hashSeed,
            numBuckets: Int(params.cuckooParams.numBuckets),
            bucketSize: Int(params.cuckooParams.bucketSize),
            ypirParams: params.hasYpirParams ? convertYpirParams(params.ypirParams) : nil,
            inspireParams: params.hasInspireParams ? convertInspireParams(params.inspireParams) : nil
        )

        // 3. Precompute keys (expensive - ~10-20 seconds)
        // Consider running in background with progress indicator
        self.cryptoState = try await Task.detached(priority: .userInitiated) {
            try precomputeKeys(params: pirParams)
        }.value
    }

    /// Check if a nullifier is spent
    func checkNullifier(_ nullifier: [UInt8]) async throws -> SpentInfo? {
        guard let state = cryptoState else {
            throw PirError.notInitialized
        }

        // 1. Compute bucket indices
        let (bucket1, bucket2) = computeCuckooBuckets(
            nullifier: nullifier,
            hashSeed: state.params.cuckooSeed,
            numBuckets: state.params.numBuckets
        )

        // 2. Check first bucket
        if let spent = try await checkBucket(bucket1, nullifier: nullifier) {
            return spent
        }

        // 3. Check second bucket
        return try await checkBucket(bucket2, nullifier: nullifier)
    }

    private func checkBucket(_ bucketIdx: Int, nullifier: [UInt8]) async throws -> SpentInfo? {
        guard let state = cryptoState else { throw PirError.notInitialized }

        // Generate query locally (crypto operation)
        let (queryState, queryBytes) = try generateQuery(state: state, bucketIdx: bucketIdx)

        // Send query via gRPC
        let request = InspireQueryRequest.with { $0.query = Data(queryBytes) }
        let response = try await grpcClient.inspireQuery(request)

        // Decrypt response locally (crypto operation)
        let bucketData = try decryptResponse(queryState: queryState, response: Array(response.response))

        // Search bucket for nullifier
        let fingerprint = computeFingerprint(nullifier: nullifier)
        return searchBucket(
            bucketData: bucketData,
            fingerprint: fingerprint,
            bucketSize: state.params.bucketSize
        )
    }

    /// Batch check multiple nullifiers
    func checkNullifiers(_ nullifiers: [[UInt8]]) async throws -> [SpentInfo?] {
        // Run in parallel for better performance
        try await withThrowingTaskGroup(of: (Int, SpentInfo?).self) { group in
            for (index, nullifier) in nullifiers.enumerated() {
                group.addTask {
                    let result = try await self.checkNullifier(nullifier)
                    return (index, result)
                }
            }

            var results = [SpentInfo?](repeating: nil, count: nullifiers.count)
            for try await (index, result) in group {
                results[index] = result
            }
            return results
        }
    }
}
```

## Kotlin Integration Example

```kotlin
import com.zcash.nullifier.NullifierCrypto  // UniFFI-generated bindings
import cash.z.wallet.sdk.rpc.LightwalletGrpcKt

class PirNullifierChecker(
    private val grpcClient: LightwalletGrpcKt.LightwalletServiceCoroutineStub
) {
    private var cryptoState: CryptoState? = null

    /** Initialize PIR (call once on wallet startup) */
    suspend fun initialize() {
        // 1. Fetch parameters from lightwalletd
        val params = grpcClient.getPirParams(GetPirParamsRequest.getDefaultInstance())

        require(params.pirReady) { "PIR not ready" }

        // 2. Convert gRPC params to FFI params
        val pirParams = PirParams(
            cuckooSeed = params.cuckooParams.hashSeed,
            numBuckets = params.cuckooParams.numBuckets.toInt(),
            bucketSize = params.cuckooParams.bucketSize.toInt(),
            ypirParams = if (params.hasYpirParams()) convertYpirParams(params.ypirParams) else null,
            inspireParams = if (params.hasInspireParams()) convertInspireParams(params.inspireParams) else null
        )

        // 3. Precompute keys (expensive - ~10-20 seconds)
        cryptoState = withContext(Dispatchers.Default) {
            precomputeKeys(pirParams)
        }
    }

    /** Check if a nullifier is spent */
    suspend fun checkNullifier(nullifier: ByteArray): SpentInfo? {
        val state = cryptoState ?: throw IllegalStateException("Not initialized")

        // 1. Compute bucket indices
        val (bucket1, bucket2) = computeCuckooBuckets(
            nullifier = nullifier,
            hashSeed = state.params.cuckooSeed,
            numBuckets = state.params.numBuckets
        )

        // 2. Check first bucket
        checkBucket(bucket1, nullifier)?.let { return it }

        // 3. Check second bucket
        return checkBucket(bucket2, nullifier)
    }

    private suspend fun checkBucket(bucketIdx: Int, nullifier: ByteArray): SpentInfo? {
        val state = cryptoState ?: throw IllegalStateException("Not initialized")

        // Generate query locally (crypto operation)
        val (queryState, queryBytes) = generateQuery(state, bucketIdx)

        // Send query via gRPC
        val request = InspireQueryRequest.newBuilder()
            .setQuery(ByteString.copyFrom(queryBytes))
            .build()
        val response = grpcClient.inspireQuery(request)

        // Decrypt response locally (crypto operation)
        val bucketData = decryptResponse(queryState, response.response.toByteArray())

        // Search bucket for nullifier
        val fingerprint = computeFingerprint(nullifier)
        return searchBucket(bucketData, fingerprint, state.params.bucketSize)
    }

    /** Batch check multiple nullifiers */
    suspend fun checkNullifiers(nullifiers: List<ByteArray>): List<SpentInfo?> {
        return coroutineScope {
            nullifiers.map { nullifier ->
                async { checkNullifier(nullifier) }
            }.awaitAll()
        }
    }
}
```

## Wallet Integration Checklist

### Phase 1: Basic Integration

- [ ] Add `nullifier-crypto` FFI dependency (Swift Package / AAR)
- [ ] Implement `GetPirParams()` gRPC call
- [ ] Implement `YpirQuery()` / `InspireQuery()` gRPC calls
- [ ] Create `PirNullifierChecker` wrapper class
- [ ] Add key precomputation on wallet startup (with progress UI)
- [ ] Integrate with existing sync logic

### Phase 2: Sync Optimization

- [ ] Check `lastSyncHeight < pirCutoffHeight` before using PIR
- [ ] Use PIR for known notes at wallet startup
- [ ] Track "PIR-verified through height X" per nullifier
- [ ] Skip redundant nullifier checks for PIR-verified blocks
- [ ] Fall back to trial decryption for recent blocks

### Phase 3: Error Handling

- [ ] Handle `pirReady = false` (fall back to trial decryption)
- [ ] Handle network errors during PIR query
- [ ] Implement retry logic with exponential backoff
- [ ] Add timeout handling (suggest 30s for PIR queries)
- [ ] Cache PIR params to avoid repeated fetches

### Phase 4: Performance

- [ ] Precompute keys in background thread
- [ ] Consider caching precomputed keys to disk
- [ ] Batch nullifier checks where possible
- [ ] Prefer InsPIRe on cellular networks
- [ ] Add progress indicators for long operations

## Error Handling

```
┌─────────────────────────────────────────────────────────────────────────────────┐
│                           ERROR HANDLING STRATEGY                                │
└─────────────────────────────────────────────────────────────────────────────────┘

ERROR: PIR not ready (pirReady = false)
───────────────────────────────────────
Action: Fall back to trial decryption for ALL nullifiers
Reason: PIR service is rebuilding or unavailable
User experience: Sync proceeds normally, just slower


ERROR: GetPirParams() fails
───────────────────────────────────────
Action: Retry with exponential backoff (1s, 2s, 4s, max 30s)
After 3 failures: Fall back to trial decryption
User experience: "Checking balance..." (no change to user)


ERROR: PIR query times out (> 30s)
───────────────────────────────────────
Action: Cancel query, fall back to trial decryption for this nullifier
Reason: PIR service may be overloaded
User experience: Continue sync, this nullifier checked via trial decrypt


ERROR: PIR query returns invalid response
───────────────────────────────────────
Action: Log error, fall back to trial decryption
Reason: Protocol mismatch or corrupted response
User experience: Continue sync normally


IMPORTANT: PIR is an optimization, not a requirement.
          Always have trial decryption as fallback.
          Never block wallet functionality on PIR availability.
```

## Security Considerations

```
┌─────────────────────────────────────────────────────────────────────────────────┐
│                           SECURITY PROPERTIES                                    │
└─────────────────────────────────────────────────────────────────────────────────┘

WHAT PIR PROTECTS:
──────────────────
✓ Server cannot learn which nullifier you're looking up
✓ Server cannot link multiple queries to same wallet
✓ Server cannot determine your balance or transaction history
✓ Query pattern reveals nothing about your notes

WHAT PIR DOES NOT PROTECT:
──────────────────────────
✗ Timing correlation (query frequency may reveal activity patterns)
✗ IP address (use Tor if concerned)
✗ TLS metadata (connection timing to lightwalletd)
✗ Trial decryption queries (for recent blocks)

TRUST MODEL:
────────────
• Client trusts FFI library (compiled from audited source)
• Client trusts TLS connection to lightwalletd
• lightwalletd trusts connection to PIR service
• PIR service is honest-but-curious (follows protocol but may log)

RECOMMENDATIONS:
────────────────
• Always use TLS for gRPC connections
• Consider Tor for additional IP privacy
• Batch queries to reduce timing correlation
• Don't log nullifier values client-side
```

## Performance Expectations

| Operation | Time | Notes |
|-----------|------|-------|
| GetPirParams() | ~50ms | Single gRPC call |
| precompute_keys() | 10-20s | One-time per session |
| compute_cuckoo_buckets() | <1ms | Pure computation |
| generate_query() (YPIR) | ~10ms | Crypto operation |
| generate_query() (InsPIRe) | ~600ms | More complex |
| YpirQuery() network | ~100ms | Depends on latency |
| InspireQuery() network | ~50ms | Smaller payload |
| decrypt_response() | ~50ms | Crypto operation |
| **Total per nullifier** | **~200ms (YPIR)** | End-to-end |
| **Total per nullifier** | **~700ms (InsPIRe)** | End-to-end |

For a wallet with 10 known notes:
- YPIR: ~2 seconds total
- InsPIRe: ~7 seconds total
- Traditional sync: ~25 minutes (for 1 week of blocks)

**Speedup: 150-750x faster for known notes**

## Future Enhancements

### Phase 2: PIR for Output Scanning
- Use PIR to check if outputs are addressed to you
- Eliminate need for trial decryption entirely
- Only download blocks containing your transactions

### Phase 3: Preprocessing Cache
- Cache InsPIRe preprocessing data on device
- Reduce query generation from 600ms to ~10ms
- Significant UX improvement for repeated checks

### Phase 4: Parallel PIR Servers
- Support multiple PIR servers for redundancy
- Load balancing across servers
- Geographic distribution for lower latency
