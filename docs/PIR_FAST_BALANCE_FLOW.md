# PIR-Enhanced Wallet Sync: Faster Spent Status for Known Notes

This document explains how PIR enables wallets to check spent status of **existing notes** faster than traditional sync.

> **See also:**
> - [PIR Client Integration Guide](PIR_CLIENT_INTEGRATION.md) - Complete architecture and implementation guide for wallet developers
> - [PIR Nullifier Lookup Flow](PIR_NULLIFIER_LOOKUP_FLOW.md) - Decision tree for PIR vs trial decryption

## Scope of This Feature

> **This is an incremental step.** PIR is used strictly to check if notes the wallet
> already knows about have been spent. Eventually, PIR will replace trial decryption
> entirely, but this focused improvement ships first.

**What PIR does in this phase:**
- Check spent status of notes discovered in previous syncs

**What PIR does NOT do (yet):**
- Find new notes (still requires trial decryption)
- Check spent status of notes found during current sync (traditional flow handles this)

## Traditional Zashi Flow (Sequential)

```
┌─────────────────────────────────────────────────────────────────────────┐
│                    TRADITIONAL SYNC (Linear, Slow)                      │
└─────────────────────────────────────────────────────────────────────────┘

Block N        Block N+1      Block N+2                    Block TIP
   │              │              │                            │
   ▼              ▼              ▼                            ▼
┌──────┐      ┌──────┐      ┌──────┐                     ┌──────┐
│Download│    │Download│    │Download│      ...         │Download│
│Compact │    │Compact │    │Compact │                  │Compact │
│Block   │    │Block   │    │Block   │                  │Block   │
└──────┘      └──────┘      └──────┘                     └──────┘
   │              │              │                            │
   ▼              ▼              ▼                            ▼
┌──────┐      ┌──────┐      ┌──────┐                     ┌──────┐
│Trial  │     │Trial  │     │Trial  │       ...         │Trial  │
│Decrypt│     │Decrypt│     │Decrypt│                   │Decrypt│
│Outputs│     │Outputs│     │Outputs│                   │Outputs│
└──────┘      └──────┘      └──────┘                     └──────┘
   │              │              │                            │
   │ Found        │ Nothing     │ Found                      │
   │ Note A       │             │ Note B                     │
   ▼              ▼              ▼                            ▼
┌──────┐      ┌──────┐      ┌──────┐                     ┌──────┐
│Check  │     │Check  │     │Check  │       ...         │Check  │
│Block's│     │Block's│     │Block's│                   │Block's│
│NFs vs │     │NFs vs │     │NFs vs │                   │NFs vs │
│Mine   │     │Mine   │     │Mine   │                   │Mine   │
└──────┘      └──────┘      └──────┘                     └──────┘
   │              │              │                            │
   │              │              │ Note A's                   │
   │              │              │ nullifier!                 │
   ▼              ▼              ▼                            ▼
                            Mark Note A
                            as SPENT
                                                              │
                                                              ▼
                                                    ┌─────────────────┐
                                                    │ BALANCE KNOWN   │
                                                    │ Only after full │
                                                    │ sync complete   │
                                                    └─────────────────┘

Problem: Must process ALL blocks sequentially before knowing which notes are spent.
         User cannot see accurate balance until sync completes.
```

## PIR-Enhanced Flow: Known Notes Only

```
┌─────────────────────────────────────────────────────────────────────────┐
│     PIR ENHANCEMENT: Fast Spent Check for Previously Discovered Notes   │
└─────────────────────────────────────────────────────────────────────────┘

                              ┌──────────────────────────┐
                              │     WALLET STARTUP       │
                              └──────────────────────────┘
                                          │
                                          ▼
                              ┌──────────────────────────┐
                              │  Load notes from local   │
                              │  database (previous sync)│
                              │                          │
                              │  lastSyncHeight = 900    │
                              │  Note A: 1.5 ZEC         │
                              │  Note B: 0.3 ZEC         │
                              │  Note C: 2.1 ZEC         │
                              └──────────────────────────┘
                                          │
                                          ▼
                              ┌──────────────────────────┐
                              │  GetPirParams()          │
                              │  pirCutoffHeight = 995   │
                              │  currentTip = 1000       │
                              └──────────────────────────┘
                                          │
                                          ▼
                    ┌─────────────────────────────────────────┐
                    │  Is PIR useful for known notes?         │
                    │                                         │
                    │  lastSyncHeight < pirCutoffHeight ?     │
                    │  (Has user already synced past PIR?)    │
                    └─────────────────────────────────────────┘
                                          │
                         ┌────────────────┴────────────────┐
                         │                                 │
                   YES (900 < 995)                   NO (e.g., 998 > 995)
                   PIR covers unseen                 Already synced past
                   blocks                            PIR cutoff
                         │                                 │
                         ▼                                 │
              ┌─────────────────────┐                      │
              │                     │                      │
              │  PIR SPENT CHECK    │                      │
              │  (for known notes)  │                      │
              │                     │                      │
              │  Checks blocks      │                      │
              │  901 → 995 via PIR  │                      │
              │                     │                      │
              │  Time: ~4s/note     │                      │
              │  Total: ~10s        │                      │
              │                     │                      │
              └─────────────────────┘                      │
                         │                                 │
                         ▼                                 │
              ┌─────────────────────┐                      │
              │ KNOWN NOTE BALANCE  │                      │
              │ AVAILABLE FAST      │                      │
              │                     │                      │
              │ Note A: UNSPENT ✓   │                      │
              │ Note B: SPENT   ✗   │                      │
              │ Note C: UNSPENT ✓   │                      │
              │                     │                      │
              │ Balance: 3.6 ZEC    │                      │
              │ (of known notes)    │                      │
              └─────────────────────┘                      │
                         │                                 │
                         └────────────┬────────────────────┘
                                      │
                                      ▼
              ┌───────────────────────────────────────────────────────┐
              │                                                       │
              │              TRADITIONAL SYNC CONTINUES               │
              │                                                       │
              │   Download blocks → Trial decrypt → Find new notes    │
              │   Check each block's nullifiers against known notes   │
              │                                                       │
              │   Sync range: lastSyncHeight+1 → currentTip           │
              │   (900→1000 in PIR case, or 998→1000 in skip case)   │
              │                                                       │
              │   • New notes found during sync use traditional flow  │
              │   • No PIR queries for newly discovered notes         │
              │   • Balance updates as new notes are found            │
              │                                                       │
              └───────────────────────────────────────────────────────┘
```

## The PIR Decision: When to Skip

```
┌─────────────────────────────────────────────────────────────────────────┐
│  pirCutoffHeight is NOT "stop syncing here"                             │
│  It's "would PIR help check known notes faster?"                        │
└─────────────────────────────────────────────────────────────────────────┘

pirCutoffHeight = 995
currentTip = 1000

CASE A: lastSyncHeight = 900
─────────────────────────────────────────────────────────────────────────
  User hasn't seen blocks 901-1000
  PIR covers 901-995 (95 blocks of unseen data!)

  → DO PIR FIRST: Check known notes in ~10 seconds
  → Then sync 901-1000 traditionally for new notes

CASE B: lastSyncHeight = 998
─────────────────────────────────────────────────────────────────────────
  User already synced through block 998
  User already knows spent status through block 998
  PIR covers up to 995... but user is PAST that already!

  → SKIP PIR: It tells us nothing new
  → Just sync 999-1000 traditionally (2 blocks, ~1 second)

CASE C: lastSyncHeight = 990
─────────────────────────────────────────────────────────────────────────
  User hasn't seen blocks 991-1000
  PIR covers 991-995 (5 blocks of unseen data)
  Traditional sync needed for 996-1000 anyway

  → SKIP PIR: Only 5 blocks of PIR-covered gap
  → Traditional sync of 10 blocks is faster than PIR query

  (Optional optimization: threshold check, e.g., skip if gap < 100 blocks)
```

## Nullifier Checking During Sync: Avoiding Redundant Work

```
┌─────────────────────────────────────────────────────────────────────────┐
│  After PIR: What happens during traditional sync?                       │
└─────────────────────────────────────────────────────────────────────────┘

Scenario:
- Known nullifiers: A, B, C, D
- PIR result: All unspent (checked through block 995)
- Now syncing blocks 900 → 1000

FOR BLOCKS 900-995 (covered by PIR):
─────────────────────────────────────────────────────────────────────────
  PIR already told us A, B, C, D are NOT in these blocks.

  Naive approach: Check each block's nullifiers against A, B, C, D
                  → Redundant! PIR already answered this.

  Optimized approach: Skip nullifier comparison for A, B, C, D
                      since PIR verified them through block 995.

  Note: We still trial decrypt outputs to find NEW notes.
        Only the nullifier comparison is skippable.


FOR BLOCKS 996-1000 (NOT covered by PIR):
─────────────────────────────────────────────────────────────────────────
  PIR tells us NOTHING about these blocks.

  We MUST check if A, B, C, D were spent in blocks 996-1000.
  This is done via traditional nullifier comparison during trial decrypt.

  ┌─────────────────────────────────────────────────────────────────┐
  │  Block 996: Check nullifiers against A, B, C, D                 │
  │  Block 997: Check nullifiers against A, B, C, D                 │
  │  Block 998: Check nullifiers against A, B, C, D  ← Found A!     │
  │  Block 999: Check nullifiers against B, C, D     (A now spent)  │
  │  Block 1000: Check nullifiers against B, C, D                   │
  └─────────────────────────────────────────────────────────────────┘
```

## Wallet Implementation Note

```
┌─────────────────────────────────────────────────────────────────────────┐
│  This optimization is WALLET-SIDE, not lightwalletd                     │
└─────────────────────────────────────────────────────────────────────────┘

Lightwalletd provides:
  • GetPirParams() → returns pirCutoffHeight
  • InspireQuery() / YpirQuery() → PIR query endpoints

Wallet implements:
  • Decision: lastSyncHeight < pirCutoffHeight? → Do PIR first
  • Optimization: Track "PIR-verified through height X" per nullifier
  • Skip redundant checks for PIR-verified nullifiers in blocks ≤ X
  • Resume checking all nullifiers for blocks > pirCutoffHeight
```

## Why Not PIR for Newly Found Notes?

When syncing from block 900 to block 1000 and you find a new note at block 950:

```
┌─────────────────────────────────────────────────────────────────────────┐
│  Scenario: New note found at block 950 during sync to block 1000       │
└─────────────────────────────────────────────────────────────────────────┘

Option A: PIR Query                    Option B: Continue Sync
─────────────────────                  ─────────────────────
• Query takes ~4 seconds               • Already processing blocks 950-1000
• Covers blocks up to ~995             • Will catch spend naturally
• Still need blocks 996-1000           • Takes ~1-2 seconds for 50 blocks

                    PIR is SLOWER for this case!

┌─────────────────────────────────────────────────────────────────────────┐
│  Decision: Notes found during sync use traditional flow.               │
│            We're already committed to processing those blocks.         │
└─────────────────────────────────────────────────────────────────────────┘
```

## Detailed Flow: Returning User (PIR Useful)

```
┌─────────────────────────────────────────────────────────────────────────┐
│          RETURNING USER: lastSyncHeight (900) < pirCutoffHeight (995)   │
│                         → PIR IS USEFUL                                 │
└─────────────────────────────────────────────────────────────────────────┘

Last sync: block 900
Current tip: block 1000
pirCutoffHeight: 995
Known notes: A, B, C (from blocks before 900)

═══════════════════════════════════════════════════════════════════════════
STEP 1: Immediate PIR check for known notes (FAST PATH)
═══════════════════════════════════════════════════════════════════════════

    ┌─────────────────────────────────────┐
    │ PIR Query for Note A nullifier      │──► UNSPENT
    │ PIR Query for Note B nullifier      │──► SPENT (in block 920)
    │ PIR Query for Note C nullifier      │──► UNSPENT
    │                                     │
    │ Time: ~10 seconds                   │
    └─────────────────────────────────────┘
              │
              ▼
    ┌─────────────────────────────────────┐
    │ DISPLAY TO USER:                    │
    │                                     │
    │ "Balance of known notes: 3.6 ZEC"   │
    │ "Syncing for new transactions..."   │
    └─────────────────────────────────────┘


═══════════════════════════════════════════════════════════════════════════
STEP 2: Traditional sync for ALL missed blocks (901 → 1000)
═══════════════════════════════════════════════════════════════════════════

    ┌─────────────────────────────────────┐
    │ Download & process blocks 901-1000  │
    │                                     │
    │ • Trial decrypt each block          │
    │ • Found Note D at block 950!        │
    │   └─► Add to known notes            │
    │   └─► Continue sync normally        │
    │   └─► NO PIR query (not needed)     │
    │                                     │
    │ • Check each block's nullifiers     │
    │   └─► Note B spent in block 920?    │
    │       Already knew from PIR!        │
    │                                     │
    │ Time: ~25 minutes                   │
    └─────────────────────────────────────┘
              │
              ▼
    ┌─────────────────────────────────────┐
    │ FINAL STATE:                        │
    │                                     │
    │ Note A: 1.5 ZEC - UNSPENT          │
    │ Note C: 2.1 ZEC - UNSPENT          │
    │ Note D: 0.8 ZEC - UNSPENT (new!)   │
    │                                     │
    │ Balance: 4.4 ZEC                    │
    └─────────────────────────────────────┘
```

## Detailed Flow: Returning User (PIR Skipped)

```
┌─────────────────────────────────────────────────────────────────────────┐
│          RETURNING USER: lastSyncHeight (998) > pirCutoffHeight (995)   │
│                         → SKIP PIR                                      │
└─────────────────────────────────────────────────────────────────────────┘

Last sync: block 998
Current tip: block 1000
pirCutoffHeight: 995
Known notes: A, B, C

═══════════════════════════════════════════════════════════════════════════
NO PIR STEP - User already synced past pirCutoffHeight
═══════════════════════════════════════════════════════════════════════════

    User already knows spent status of notes A, B, C through block 998.
    PIR would only tell us about blocks up to 995 - we're past that!

═══════════════════════════════════════════════════════════════════════════
ONLY STEP: Traditional sync for missed blocks (999 → 1000)
═══════════════════════════════════════════════════════════════════════════

    ┌─────────────────────────────────────┐
    │ Download & process blocks 999-1000  │
    │                                     │
    │ • Trial decrypt each block          │
    │ • Check nullifiers vs A, B, C       │
    │                                     │
    │ Time: ~1 second                     │
    └─────────────────────────────────────┘
              │
              ▼
    ┌─────────────────────────────────────┐
    │ BALANCE KNOWN                       │
    │ (via normal sync, PIR not needed)   │
    └─────────────────────────────────────┘
```

## Time Comparison

```
┌─────────────────────────────────────────────────────────────────────────┐
│      TIME TO KNOW SPENT STATUS OF EXISTING NOTES                        │
└─────────────────────────────────────────────────────────────────────────┘

Scenario: User has 10 notes from previous sync, opens wallet after 1 week

TRADITIONAL APPROACH (must sync all missed blocks first)
────────────────────────────────────────────────────────────────────────
│ Download    │ Trial decrypt │ Check nullifiers │ Spent status│
│ 10,080 blks │ each block    │ against my notes │ known       │
│ ~5 min      │ ~20 min       │ (during decrypt) │             │
├─────────────┴───────────────┴──────────────────┴─────────────┤
│                        ~25 minutes                            │
└───────────────────────────────────────────────────────────────┘


PIR-ENHANCED APPROACH (check known notes immediately)
────────────────────────────────────────────────────────────────────────
│ Load local DB │ PIR queries for │ Spent status │
│ ~100ms        │ 10 known notes  │ known!       │
│               │ ~10 seconds     │              │
├───────────────┴─────────────────┴──────────────┤
│              ~10 seconds                        │
└─────────────────────────────────────────────────┘

         Background: Continue sync to find new incoming notes
         (balance may increase, but known notes' status is accurate)

SPEEDUP: ~150x faster to know if your existing notes are spent
```

## Future: Full PIR Replacement

```
┌─────────────────────────────────────────────────────────────────────────┐
│                         ROADMAP                                         │
└─────────────────────────────────────────────────────────────────────────┘

PHASE 1 (Current): PIR for spent status of known notes
───────────────────────────────────────────────────────
✓ Check if existing notes are spent via PIR
✓ Trial decryption still required to find new notes
✓ Traditional flow for notes discovered during sync


PHASE 2 (Future): PIR for output scanning
───────────────────────────────────────────────────────
○ Use PIR to check if outputs are addressed to you
○ Eliminate need for trial decryption entirely
○ Only download blocks containing your transactions
○ Massive bandwidth and computation savings


PHASE 3 (Future): Full privacy-preserving sync
───────────────────────────────────────────────────────
○ PIR for both outputs (receiving) and nullifiers (spending)
○ Server learns nothing about your transaction history
○ Complete privacy with minimal sync time
```

## Summary: Current Phase Scope

| Action | Method | Notes |
|--------|--------|-------|
| Check if KNOWN note is spent | **PIR** | Fast, privacy-preserving |
| Find new notes (receiving) | Trial Decrypt | Must scan all blocks |
| Notes found during sync | Traditional | Continue sync, no PIR needed |
| Recent blocks (above pirCutoff) | Trial Decrypt | PIR not yet rebuilt |
| Live block processing | Traditional | Real-time, one block at a time |

**Key Point:** This feature is strictly for checking spent status of notes the wallet already knows about. Finding new notes still requires trial decryption. This is an incremental improvement that delivers immediate value while the full PIR integration continues.
