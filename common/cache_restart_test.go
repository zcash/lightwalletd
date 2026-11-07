// Copyright (c) 2019-present The Zcash developers
// Distributed under the MIT software license, see the accompanying
// file COPYING or https://www.opensource.org/licenses/mit-license.php .
package common

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"os"
	"testing"

	"github.com/zcash/lightwalletd/parser"
	"github.com/zcash/lightwalletd/walletrpc"
	"google.golang.org/protobuf/proto"
)

// These tests restart the cache (NewBlockCache on the same directory) the way
// lightwalletd does, and check that the files on disk stay consistent with the
// in-memory index. Blocks are appended with O_APPEND, so any stale bytes left
// at the end of either file misplace every block written after the restart.

const restartFirst = 289460

func loadRestartCompacts(t *testing.T) []*walletrpc.CompactBlock {
	t.Helper()
	var tests []struct {
		Full string `json:"full"`
	}
	blockJSON, err := os.ReadFile("../testdata/compact_blocks.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(blockJSON, &tests); err != nil {
		t.Fatal(err)
	}
	var out []*walletrpc.CompactBlock
	for _, test := range tests {
		data, _ := hex.DecodeString(test.Full)
		block := parser.NewBlock()
		if _, err := block.ParseFromSlice(data); err != nil {
			t.Fatal(err)
		}
		out = append(out, block.ToCompact())
	}
	return out
}

func newFilledCache(t *testing.T, dir string, blocks []*walletrpc.CompactBlock) *BlockCache {
	t.Helper()
	c := NewBlockCache(dir, unitTestChain, restartFirst, -1)
	for i, b := range blocks {
		if err := c.Add(restartFirst+i, b); err != nil {
			t.Fatal(err)
		}
	}
	return c
}

// requireBlocks checks the cache holds exactly want, readable at the
// expected heights.
func requireBlocks(t *testing.T, c *BlockCache, want []*walletrpc.CompactBlock) {
	t.Helper()
	if got := c.GetNextHeight(); got != restartFirst+len(want) {
		t.Fatalf("nextBlock = %d, want %d", got, restartFirst+len(want))
	}
	for i, w := range want {
		height := restartFirst + i
		c.mutex.RLock()
		got := c.readBlock(height)
		c.mutex.RUnlock()
		if got == nil {
			t.Fatalf("block %d unreadable", height)
		}
		if !bytes.Equal(got.Hash, w.Hash) {
			t.Fatalf("block %d has hash %x, want %x", height, got.Hash, w.Hash)
		}
	}
}

// A block that replaces compacts[3] at the same height, as after a reorg,
// with a different encoded length.
func replacementBlock(orig *walletrpc.CompactBlock) *walletrpc.CompactBlock {
	b := proto.Clone(orig).(*walletrpc.CompactBlock)
	b.Hash = bytes.Repeat([]byte{0xaa}, 32)
	b.Vtx = nil
	return b
}

func TestCacheSyncFromHeightDiscardsBlocksOnDisk(t *testing.T) {
	compacts := loadRestartCompacts(t)
	dir := t.TempDir()
	newFilledCache(t, dir, compacts).Close()

	// Restart with --sync-from-height 289463: blocks 289463.. are refetched,
	// and the node now has a different block at 289463.
	c := NewBlockCache(dir, unitTestChain, restartFirst, restartFirst+3)
	if c.GetNextHeight() != restartFirst+3 {
		t.Fatalf("nextBlock = %d, want %d", c.GetNextHeight(), restartFirst+3)
	}
	refetched := []*walletrpc.CompactBlock{compacts[0], compacts[1], compacts[2],
		replacementBlock(compacts[3]), compacts[4], compacts[5]}
	for i := 3; i < len(refetched); i++ {
		if err := c.Add(restartFirst+i, refetched[i]); err != nil {
			t.Fatal(err)
		}
	}
	requireBlocks(t, c, refetched)
	c.Close()

	// And the files must still describe the same chain after another restart.
	c = NewBlockCache(dir, unitTestChain, restartFirst, -1)
	defer c.Close()
	requireBlocks(t, c, refetched)
}

func TestCacheRedownloadDiscardsBlocksOnDisk(t *testing.T) {
	compacts := loadRestartCompacts(t)
	dir := t.TempDir()
	newFilledCache(t, dir, compacts).Close()

	// --redownload is --sync-from-height 0.
	c := NewBlockCache(dir, unitTestChain, restartFirst, 0)
	if c.GetNextHeight() != restartFirst {
		t.Fatalf("nextBlock = %d, want %d", c.GetNextHeight(), restartFirst)
	}
	for i, b := range compacts {
		if err := c.Add(restartFirst+i, b); err != nil {
			t.Fatal(err)
		}
	}
	c.Close()

	c = NewBlockCache(dir, unitTestChain, restartFirst, -1)
	defer c.Close()
	requireBlocks(t, c, compacts)
}

func TestCacheDropsBlockWrittenWithoutItsLength(t *testing.T) {
	compacts := loadRestartCompacts(t)
	dir := t.TempDir()
	newFilledCache(t, dir, compacts[:3]).Close()

	// Add() writes the block, then its length. Simulate a crash in between:
	// block 289463 is on disk but the lengths file does not list it.
	data, err := proto.Marshal(compacts[3])
	if err != nil {
		t.Fatal(err)
	}
	_, blocksName := DbFileNames(dir, unitTestChain)
	f, err := os.OpenFile(blocksName, os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Write(append(checksum(restartFirst+3, data), data...)); err != nil {
		t.Fatal(err)
	}
	f.Close()

	c := NewBlockCache(dir, unitTestChain, restartFirst, -1)
	defer c.Close()
	requireBlocks(t, c, compacts[:3])
	for i := 3; i < len(compacts); i++ {
		if err := c.Add(restartFirst+i, compacts[i]); err != nil {
			t.Fatal(err)
		}
	}
	requireBlocks(t, c, compacts)
}
