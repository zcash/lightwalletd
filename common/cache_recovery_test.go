// Copyright (c) 2026 The Zcash developers
// Distributed under the MIT software license, see the accompanying
// file COPYING or https://www.opensource.org/licenses/mit-license.php .
package common

import (
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"testing"

	"github.com/sirupsen/logrus"
	"github.com/zcash/lightwalletd/parser"
	"github.com/zcash/lightwalletd/walletrpc"
	"google.golang.org/protobuf/proto"
)

func TestCacheRecoveryAfterReorgPreservesRemainingBlocks(t *testing.T) {
	cacheRecoveryTestLogger(t)
	cache, dir, blocks := cacheRecoveryTestCache(t, 0)

	cacheRecoveryCorruptTip(t, cache)
	cacheRecoveryAssertTipCorrupt(t, cache)
	cache.Reorg(5)
	cache.recoverIfStillCorrupt(5)

	if got := cache.GetNextHeight(); got != 5 {
		t.Fatalf("recovery after Reorg lost valid blocks: next height %d, want 5", got)
	}
	for height, want := range blocks[:5] {
		if got := cache.Get(height); got == nil || !proto.Equal(got, want) {
			t.Fatalf("block %d is missing after recovery", height)
		}
	}

	cache.Close()
	cache = NewBlockCache(dir, "unittestnet", 0, -1)
	defer cache.Close()
	if got := cache.GetNextHeight(); got != 5 {
		t.Fatalf("reorged cache was not preserved after restart: next height %d, want 5", got)
	}
	for height, want := range blocks[:5] {
		if got := cache.Get(height); got == nil || !proto.Equal(got, want) {
			t.Fatalf("block %d is missing after restart", height)
		}
	}
}

func TestCacheRecoveryStillClearsUnrepairedCorruption(t *testing.T) {
	cacheRecoveryTestLogger(t)
	cache, dir, _ := cacheRecoveryTestCache(t, 0)

	cacheRecoveryCorruptTip(t, cache)
	cacheRecoveryAssertTipCorrupt(t, cache)
	cache.recoverIfStillCorrupt(5)

	if got := cache.GetNextHeight(); got != 0 {
		t.Fatalf("unrepaired corruption did not reset the cache: next height %d", got)
	}
	lengthsPath, blocksPath := DbFileNames(dir, "unittestnet")
	for _, path := range []string{lengthsPath, blocksPath} {
		contents, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if len(contents) != 0 {
			t.Fatalf("unrepaired corruption left %d bytes in %s", len(contents), path)
		}
	}

	cache.Close()
	cache = NewBlockCache(dir, "unittestnet", 0, -1)
	defer cache.Close()
	if got := cache.GetNextHeight(); got != 0 {
		t.Fatalf("reset cache was not empty after restart: next height %d", got)
	}
}

func TestCacheRecoveryAfterReorgAndReAddPreservesAllBlocks(t *testing.T) {
	cacheRecoveryTestLogger(t)
	cache, dir, blocks := cacheRecoveryTestCache(t, 0)

	cacheRecoveryCorruptTip(t, cache)
	cacheRecoveryAssertTipCorrupt(t, cache)
	cache.Reorg(5)
	if err := cache.Add(5, blocks[5]); err != nil {
		t.Fatalf("re-add block 5 after reorg: %v", err)
	}
	cache.recoverIfStillCorrupt(5)

	if got := cache.GetNextHeight(); got != 6 {
		t.Fatalf("recovery cleared the re-added tip: next height %d, want 6", got)
	}
	for height, want := range blocks {
		if got := cache.Get(height); got == nil || !proto.Equal(got, want) {
			t.Fatalf("block %d is missing after recovery", height)
		}
	}

	cache.Close()
	cache = NewBlockCache(dir, "unittestnet", 0, -1)
	defer cache.Close()
	if got := cache.GetNextHeight(); got != 6 {
		t.Fatalf("re-added cache was not preserved after restart: next height %d, want 6", got)
	}
	for height, want := range blocks {
		if got := cache.Get(height); got == nil || !proto.Equal(got, want) {
			t.Fatalf("block %d is missing after restart", height)
		}
	}
}

func cacheRecoveryTestLogger(t *testing.T) {
	t.Helper()
	old := Log
	logger := logrus.New()
	logger.SetOutput(io.Discard)
	Log = logrus.NewEntry(logger)
	t.Cleanup(func() { Log = old })
}

func cacheRecoveryTestCache(t *testing.T, firstHeight int) (*BlockCache, string, []*walletrpc.CompactBlock) {
	t.Helper()
	var fixtures []struct {
		Full string `json:"full"`
	}
	data, err := os.ReadFile("../testdata/compact_blocks.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &fixtures); err != nil {
		t.Fatal(err)
	}
	if len(fixtures) < 6 {
		t.Fatal("need six consecutive compact-block fixtures")
	}

	dir := t.TempDir()
	cache := NewBlockCache(dir, "unittestnet", firstHeight, -1)
	t.Cleanup(cache.Close)
	blocks := make([]*walletrpc.CompactBlock, 0, 6)
	for i, fixture := range fixtures[:6] {
		raw, err := hex.DecodeString(fixture.Full)
		if err != nil {
			t.Fatal(err)
		}
		blockParser := parser.NewBlock()
		rest, err := blockParser.ParseFromSlice(raw)
		if err != nil || len(rest) != 0 {
			t.Fatalf("fixture parse failed: %v; trailing bytes %d", err, len(rest))
		}
		block := blockParser.ToCompact()
		block.Height = uint64(firstHeight + i)
		if err := cache.Add(firstHeight+i, block); err != nil {
			t.Fatal(err)
		}
		blocks = append(blocks, block)
	}
	cache.Sync()
	return cache, dir, blocks
}

func cacheRecoveryCorruptTip(t *testing.T, cache *BlockCache) {
	t.Helper()
	contents, err := os.ReadFile(cache.blocksName)
	if err != nil {
		t.Fatal(err)
	}
	offset := cache.starts[5]
	file, err := os.OpenFile(cache.blocksName, os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if _, err := file.WriteAt([]byte{contents[offset] ^ 1}, offset); err != nil {
		t.Fatal(err)
	}
}

func cacheRecoveryAssertTipCorrupt(t *testing.T, cache *BlockCache) {
	t.Helper()
	cache.mutex.RLock()
	block := cache.readBlock(5)
	cache.mutex.RUnlock()
	if block != nil {
		t.Fatal("corrupted tip passed its checksum check")
	}
}
