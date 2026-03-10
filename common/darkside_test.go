package common

import (
	"os"
	"testing"

	"github.com/zcash/lightwalletd/hash32"
	"github.com/zcash/lightwalletd/parser"
)

// TestSetPrevhashChainConsistency verifies that after setPrevhash runs,
// each block's prevhash matches the actual hash of the preceding block.
// This is a regression test for issue #552, where setPrevhash computed
// block hashes from stale (pre-update) bytes, causing an infinite
// add/reorg loop in BlockIngestor.
func TestSetPrevhashChainConsistency(t *testing.T) {
	os.RemoveAll(unitTestPath)
	defer os.RemoveAll(unitTestPath)
	cache = NewBlockCache(unitTestPath, unitTestChain, 100, 0)
	DarksideEnabled = true
	defer func() { DarksideEnabled = false }()

	state = darksideState{
		resetted:               true,
		startHeight:            100,
		latestHeight:           -1,
		branchID:               "bad",
		chainName:              "test",
		cache:                  cache,
		activeBlocks:           make([]*activeBlock, 0),
		stagedBlocks:           make([][]byte, 0),
		incomingTransactions:   make([][]byte, 0),
		stagedTransactions:     make([]stagedTx, 0),
		stagedTreeStates:       make(map[uint64]*DarksideTreeState),
		stagedTreeStatesByHash: make(map[string]*DarksideTreeState),
	}

	// Stage 5 empty blocks starting at height 100.
	err := DarksideStageBlocksCreate(100, 0, 5)
	if err != nil {
		t.Fatal("DarksideStageBlocksCreate failed:", err)
	}

	// Move staged blocks to active and apply.
	stagedBlocks := state.stagedBlocks
	state.stagedBlocks = nil
	for _, blockBytes := range stagedBlocks {
		if err := addBlockActive(blockBytes); err != nil {
			t.Fatal("addBlockActive failed:", err)
		}
	}

	if len(state.activeBlocks) != 5 {
		t.Fatal("expected 5 active blocks, got", len(state.activeBlocks))
	}

	// Run setPrevhash to link the chain.
	setPrevhash()

	// for each block after the first, its prevhash field
	// must equal the hash of the preceding block (both computed from
	// the final raw bytes).
	var prevHash hash32.T
	for i, ab := range state.activeBlocks {
		block := parser.NewBlock()
		rest, err := block.ParseFromSlice(ab.bytes)
		if err != nil {
			t.Fatalf("block %d: ParseFromSlice failed: %v", i, err)
		}
		if len(rest) != 0 {
			t.Fatalf("block %d: trailing bytes after parse", i)
		}

		if i > 0 {
			blockPrevHash := block.GetPrevHash()
			if blockPrevHash != prevHash {
				t.Errorf("block %d (height %d): prevhash mismatch\n  got:  %x\n  want: %x",
					i, block.GetHeight(), blockPrevHash, prevHash)
			}
		}
		prevHash = block.GetEncodableHash()
	}
}
