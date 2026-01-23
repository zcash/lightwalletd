# PIR vs Trial Decryption: Nullifier Lookup Flow

This document explains how clients should choose between PIR (Private Information Retrieval) and trial decryption when checking if a nullifier has been spent.

> **See also:**
> - [PIR Client Integration Guide](PIR_CLIENT_INTEGRATION.md) - Complete architecture, FFI API, and Swift/Kotlin examples
> - [PIR Fast Balance Flow](PIR_FAST_BALANCE_FLOW.md) - How PIR speeds up balance checking for known notes

## Key Insight

**PIR and trial decryption are mutually exclusive based on block height.**

- Blocks **at or below** `pirCutoffHeight` → Use PIR
- Blocks **above** `pirCutoffHeight` → Use trial decryption (`GetBlockRangeNullifiers`)

There is no scenario where you use both methods for the same block range.

## The Decision Flow

```
┌─────────────────────────────────────────────────────────────────────┐
│                     CLIENT WANTS TO CHECK NULLIFIER                 │
└─────────────────────────────────────────────────────────────────────┘
                                    │
                                    ▼
┌─────────────────────────────────────────────────────────────────────┐
│                      GetPirParams() gRPC call                       │
│                                                                     │
│  Returns:                                                           │
│    - pirCutoffHeight (height PIR database covers)                   │
│    - pirReady (boolean)                                             │
│    - cuckooParams (for bucket calculation)                          │
│    - ypirParams / inspireParams                                     │
└─────────────────────────────────────────────────────────────────────┘
                                    │
                                    ▼
                         ┌──────────────────┐
                         │   pirReady?      │
                         └──────────────────┘
                           │            │
                      No   │            │  Yes
                           ▼            ▼
            ┌──────────────────┐    ┌──────────────────────────┐
            │ Fall back to     │    │ Which blocks to check?   │
            │ trial decryption │    └──────────────────────────┘
            │ for ALL blocks   │              │
            └──────────────────┘              ▼
                                   ┌─────────────────────────────┐
                                   │  For blocks > pirCutoffHeight│
                                   │  (recent blocks)            │
                                   │                             │
                                   │  Use: GetBlockRangeNullifiers│
                                   │  (trial decryption)         │
                                   └─────────────────────────────┘
                                              │
                                              ▼
                                   ┌─────────────────────────────┐
                                   │  For blocks ≤ pirCutoffHeight│
                                   │  (historical blocks)        │
                                   │                             │
                                   │  Use: PIR query             │
                                   │  (InspireQuery / YpirQuery) │
                                   └─────────────────────────────┘
```

## Concrete Example

Assume:
- Current chain tip: block **3,214,362**
- `pirCutoffHeight`: **3,214,357** (PIR database built up to this height)
- Trial decrypt buffer: 5 blocks

```
Block Heights Timeline:
═══════════════════════════════════════════════════════════════════════

    Genesis                                    pirCutoffHeight    Tip
       │                                             │             │
       ▼                                             ▼             ▼
  ┌────────────────────────────────────────────┬─────────────────────┐
  │           PIR DOMAIN                       │  TRIAL DECRYPT      │
  │   (blocks 419,200 - 3,214,357)            │  (3,214,358-362)    │
  │                                            │                     │
  │   ~52 million nullifiers                   │  ~100 nullifiers    │
  │   Use: InspireQuery or YpirQuery           │  Use: GetBlock-     │
  │                                            │  RangeNullifiers    │
  └────────────────────────────────────────────┴─────────────────────┘
```

## Client Pseudocode

```go
// Step 1: Get PIR parameters
params := GetPirParams()

if !params.pirReady {
    // PIR not available - use trial decryption for everything
    return trialDecryptAllBlocks(nullifier)
}

currentTip := GetLatestBlock()
pirCutoff := params.pirCutoffHeight

// Step 2: Check recent blocks via trial decryption
if currentTip > pirCutoff {
    // Check blocks (pirCutoff+1) to currentTip via trial decryption
    recentSpent := checkViaTrialDecryption(nullifier, pirCutoff+1, currentTip)
    if recentSpent {
        return SPENT_IN_RECENT_BLOCK
    }
}

// Step 3: Check historical blocks via PIR
// Calculate Cuckoo bucket indices for the nullifier
bucketIndices := calculateCuckooBuckets(nullifier, params.cuckooParams)

for _, bucketIdx := range bucketIndices {
    // Construct and execute PIR query for this bucket
    bucket := executePirQuery(bucketIdx, params)

    // Check if nullifier exists in retrieved bucket
    if bucketContains(bucket, nullifier) {
        return SPENT_IN_HISTORICAL_BLOCK
    }
}

return NOT_SPENT
```

## Why This Design?

### 1. PIR Requires Database Rebuild
The PIR database (using InsPIRe cryptographic structure) cannot be incrementally updated. Every time new blocks are added, the entire database must be rebuilt. This takes significant time (~4+ minutes for 52M nullifiers).

### 2. Trial Decryption is Fast for Small Sets
For a small number of recent blocks (5 by default), trial decryption is efficient and doesn't require the overhead of PIR.

### 3. Hybrid Approach Optimizes Both Privacy and Performance
- **Historical lookups** (vast majority): Use PIR for maximum privacy
- **Recent lookups** (small window): Use trial decryption while PIR catches up

## Configuration

The "5 blocks" buffer is configurable on the server side:

```
lightwalletd --pir-trial-decrypt-blocks=5
```

This determines how many recent blocks are kept available for trial decryption while the PIR database rebuilds. The actual `pirCutoffHeight` will typically be `(currentTip - trialDecryptBlocks)`.

## Server-Side Flow

```
New Block Arrives
       │
       ▼
┌──────────────────────────────────────┐
│ 1. Add block to cache                │
│ 2. Extract Orchard nullifiers        │
│ 3. Send to PIR service via HTTP POST │
│ 4. PIR service queues rebuild        │
│ 5. Background rebuild starts         │
│ 6. On completion: hot-swap database  │
│ 7. Update pirCutoffHeight            │
└──────────────────────────────────────┘
```

During rebuild, `pirCutoffHeight` stays at the previous value until the new database is ready. Clients automatically use trial decryption for the gap.

## Summary

| Block Range | Method | Privacy | Performance |
|-------------|--------|---------|-------------|
| `> pirCutoffHeight` | Trial Decryption (`GetBlockRangeNullifiers`) | Server sees which blocks you're interested in | Fast, O(n) for n recent nullifiers |
| `≤ pirCutoffHeight` | PIR Query (`InspireQuery`/`YpirQuery`) | Server learns nothing | ~3.7s per nullifier |

**Your intuition is correct**: when checking nullifiers, you use PIR for all historical blocks (those covered by the PIR database), and trial decrypt for recent blocks not yet in PIR. These are mutually exclusive ranges based on the `pirCutoffHeight` threshold.

## Client-Side Architecture

For detailed implementation guidance, see [PIR_CLIENT_INTEGRATION.md](PIR_CLIENT_INTEGRATION.md).

```
┌─────────────────────────────────────────────────────────────────────────────────┐
│                    CLIENT-SIDE PIR ARCHITECTURE                                  │
└─────────────────────────────────────────────────────────────────────────────────┘

    ┌───────────────────────────────────────────────────────────────────────────┐
    │                        WALLET (Swift / Kotlin)                            │
    │                                                                           │
    │   1. GetPirParams() ──────────────────────────────► lightwalletd (gRPC)   │
    │                                                                           │
    │   2. Crypto operations (local, via FFI):                                  │
    │      • compute_cuckoo_buckets() → bucket indices                          │
    │      • precompute_keys() → crypto state                                   │
    │      • generate_query() → query bytes                                     │
    │                                                                           │
    │   3. YpirQuery(queryBytes) ───────────────────────► lightwalletd (gRPC)   │
    │      or InspireQuery(queryBytes)                          │               │
    │                                                           ▼               │
    │   4. Decrypt response (local, via FFI):          ┌──────────────────┐     │
    │      • decrypt_response() → bucket data          │   PIR Service    │     │
    │      • search_bucket() → SpentInfo               │   (HTTP proxy)   │     │
    │                                                  └──────────────────┘     │
    └───────────────────────────────────────────────────────────────────────────┘

WHY CLIENT-SIDE CRYPTO?
───────────────────────
PIR provides privacy by ensuring the server cannot learn which nullifier
is being queried. This requires:

  • Query generation on client (encrypts which bucket to fetch)
  • Response decryption on client (only client can read answer)

If lightwalletd generated queries, it would know your nullifiers!

RECOMMENDED: nullifier-crypto FFI library
─────────────────────────────────────────
  • Rust library exposed via UniFFI
  • Handles all cryptographic operations
  • ~1MB binary size (optimized)
  • Swift and Kotlin bindings available
```

## Protocol Selection

```
┌─────────────────────────────────────────────────────────────────────────────────┐
│                    YPIR vs InsPIRe DECISION                                      │
└─────────────────────────────────────────────────────────────────────────────────┘

                        YPIR                          InsPIRe
                        ────                          ───────
Query size:             ~2 MB                         ~19 KB
Response size:          ~1 MB                         ~43 KB
E2E latency:            ~60 ms                        ~614 ms

MOBILE RECOMMENDATION:
──────────────────────
• WiFi: Either acceptable, YPIR faster
• Cellular: InsPIRe required (100x smaller)
• Background sync: InsPIRe (battery friendly)

DEFAULT: InsPIRe for mobile, YPIR for desktop
```
