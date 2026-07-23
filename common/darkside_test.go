package common

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/zcash/lightwalletd/hash32"
	"github.com/zcash/lightwalletd/parser"
	"github.com/zcash/lightwalletd/walletrpc"
	"google.golang.org/grpc/metadata"
)

// TestSetPrevhashChainConsistency verifies that after setPrevhash runs,
// each block's prevhash matches the actual hash of the preceding block.
// This is a regression test for issue #552, where setPrevhash computed
// block hashes from stale (pre-update) bytes, causing an infinite
// add/reorg loop in BlockIngestor.
func TestSetPrevhashChainConsistency(t *testing.T) {
	os.RemoveAll(unitTestPath)
	cache = NewBlockCache(unitTestPath, unitTestChain, 100, 0)
	defer os.RemoveAll(unitTestPath)
	defer cache.Close()
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

// subtreeRootStream is a test double for the gRPC stream used by
// DarksideGetSubtreeRoots.
type subtreeRootStream struct {
	roots []*walletrpc.SubtreeRoot
}

func (s *subtreeRootStream) Send(root *walletrpc.SubtreeRoot) error {
	s.roots = append(s.roots, root)
	return nil
}
func (s *subtreeRootStream) SetHeader(metadata.MD) error  { return nil }
func (s *subtreeRootStream) SendHeader(metadata.MD) error { return nil }
func (s *subtreeRootStream) SetTrailer(metadata.MD)       {}
func (s *subtreeRootStream) Context() context.Context     { return context.Background() }
func (s *subtreeRootStream) SendMsg(any) error            { return nil }
func (s *subtreeRootStream) RecvMsg(any) error            { return nil }

// TestDarksideClearAllTreeStatesClearsHashIndex is a regression test for the
// bug where DarksideClearAllTreeStates cleared the by-height map but left
// the by-hash index populated, so tree states remained retrievable by hash.
func TestDarksideClearAllTreeStatesClearsHashIndex(t *testing.T) {
	hash := strings.Repeat("ab", 32)
	cache := NewBlockCache(t.TempDir(), unitTestChain, 100, 0)

	mutex.Lock()
	state.cache = cache
	mutex.Unlock()

	if err := DarksideReset(100, "cafe", "test", 0, 0, 0); err != nil {
		t.Fatal(err)
	}
	if err := DarksideAddTreeState(DarksideTreeState{
		Network:      "test",
		Height:       123,
		Hash:         hash,
		Time:         456,
		SaplingTree:  "sapling",
		OrchardTree:  "orchard",
		IronwoodTree: "ironwood",
	}); err != nil {
		t.Fatal(err)
	}

	hashJSON, err := json.Marshal(hash)
	if err != nil {
		t.Fatal(err)
	}
	result, err := darksideRawRequest("z_gettreestate", []json.RawMessage{hashJSON})
	if err != nil {
		t.Fatal(err)
	}
	var treeState ZcashdRpcReplyGettreestate
	if err := json.Unmarshal(result, &treeState); err != nil {
		t.Fatal(err)
	}
	if treeState.Ironwood.Commitments.FinalState != "ironwood" {
		t.Fatal("ironwood tree state was not returned")
	}

	if err := DarksideClearAllTreeStates(); err != nil {
		t.Fatal(err)
	}
	if _, err := darksideRawRequest("z_gettreestate", []json.RawMessage{hashJSON}); err == nil {
		t.Fatal("tree state should not be available by hash after clearing")
	}
	// Removing by height or hash after clearing should be a no-op, not a crash.
	if err := DarksideRemoveTreeState(&walletrpc.BlockID{Height: 123}); err != nil {
		t.Fatal(err)
	}
	if err := DarksideRemoveTreeState(&walletrpc.BlockID{Hash: bytes.Repeat([]byte{0xab}, 32)}); err != nil {
		t.Fatal(err)
	}
}

// TestDarksideGetSubtreeRootsZeroMaxEntriesReturnsAll is a regression test for
// the bug where MaxEntries == 0 caused DarksideGetSubtreeRoots to return no
// roots, because limit > 0 was always false. After the fix, 0 means unlimited.
func TestDarksideGetSubtreeRootsZeroMaxEntriesReturnsAll(t *testing.T) {
	cache := NewBlockCache(t.TempDir(), unitTestChain, 100, 0)

	mutex.Lock()
	state.cache = cache
	mutex.Unlock()

	if err := DarksideReset(100, "cafe", "test", 0, 0, 0); err != nil {
		t.Fatal(err)
	}
	if err := DarksideSetSubtreeRoots(&walletrpc.DarksideSubtreeRoots{
		ShieldedProtocol: walletrpc.ShieldedProtocol_ironwood,
		StartIndex:       7,
		SubtreeRoots: []*walletrpc.SubtreeRoot{
			{
				RootHash:              []byte{1},
				CompletingBlockHash:   []byte{11},
				CompletingBlockHeight: 101,
			},
			{
				RootHash:              []byte{2},
				CompletingBlockHash:   []byte{12},
				CompletingBlockHeight: 102,
			},
			{
				RootHash:              []byte{3},
				CompletingBlockHash:   []byte{13},
				CompletingBlockHeight: 103,
			},
		},
	}); err != nil {
		t.Fatal(err)
	}

	allRoots := &subtreeRootStream{}
	if err := DarksideGetSubtreeRoots(&walletrpc.GetSubtreeRootsArg{
		ShieldedProtocol: walletrpc.ShieldedProtocol_ironwood,
		StartIndex:       7,
	}, allRoots); err != nil {
		t.Fatal(err)
	}
	if len(allRoots.roots) != 3 {
		t.Fatalf("expected all roots for zero maxEntries, got %d", len(allRoots.roots))
	}

	limitedRoots := &subtreeRootStream{}
	if err := DarksideGetSubtreeRoots(&walletrpc.GetSubtreeRootsArg{
		ShieldedProtocol: walletrpc.ShieldedProtocol_ironwood,
		StartIndex:       8,
		MaxEntries:       1,
	}, limitedRoots); err != nil {
		t.Fatal(err)
	}
	if len(limitedRoots.roots) != 1 || !bytes.Equal(limitedRoots.roots[0].RootHash, []byte{2}) {
		t.Fatal("limited subtree roots response was incorrect")
	}
}
