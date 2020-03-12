// Copyright (c) 2019-2020 The Zcash developers
// Distributed under the MIT software license, see the accompanying
// file COPYING or https://www.opensource.org/licenses/mit-license.php .
package common

import (
	"encoding/hex"
	"encoding/json"
	"io/ioutil"
	"testing"

	"github.com/zcash/lightwalletd/parser"
	"github.com/zcash/lightwalletd/walletrpc"
)

func TestCache(t *testing.T) {
	type compactTest struct {
		BlockHeight int    `json:"block"`
		BlockHash   string `json:"hash"`
		PrevHash    string `json:"prev"`
		Full        string `json:"full"`
		Compact     string `json:"compact"`
	}
	var compactTests []compactTest
	var compacts []*walletrpc.CompactBlock

	blockJSON, err := ioutil.ReadFile("../testdata/compact_blocks.json")
	if err != nil {
		t.Fatal(err)
	}

	err = json.Unmarshal(blockJSON, &compactTests)
	if err != nil {
		t.Fatal(err)
	}
	cache := NewBlockCache(4)

	// derive compact blocks from file data (setup, not part of the test)
	for _, test := range compactTests {
		blockData, _ := hex.DecodeString(test.Full)
		block := parser.NewBlock()
		_, err = block.ParseFromSlice(blockData)
		if err != nil {
			t.Fatal(err)
		}
		compacts = append(compacts, block.ToCompact())
	}

	// initially empty cache
	if cache.GetLatestHeight() != -1 {
		t.Fatal("unexpected GetLatestHeight")
	}

	// Test handling an invalid block (nil will do)
	reorg, err := cache.Add(21, nil)
	if err == nil {
		t.Error("expected error:", err)
	}
	if reorg {
		t.Fatal("unexpected reorg")
	}

	// normal, sunny-day case, 6 blocks, add as blocks 10-15
	for i, compact := range compacts {
		reorg, err = cache.Add(10+i, compact)
		if err != nil {
			t.Fatal(err)
		}
		if reorg {
			t.Fatal("unexpected reorg")
		}
		if cache.GetLatestHeight() != 10+i {
			t.Fatal("unexpected GetLatestHeight")
		}
		// The test blocks start at height 289460
		if int(cache.Get(10+i).Height) != 289460+i {
			t.Fatal("unexpected block contents")
		}
	}
	if len(cache.m) != 4 { // max entries is 4
		t.Fatal("unexpected number of cache entries")
	}
	if cache.firstBlock != 16-4 {
		t.Fatal("unexpected firstBlock")
	}
	if cache.nextBlock != 16 {
		t.Fatal("unexpected nextBlock")
	}

	// No entries just before and just after the cache range
	if cache.Get(11) != nil || cache.Get(16) != nil {
		t.Fatal("unexpected Get")
	}

	// We can re-add the last block (with the same data) and
	// that should just replace and not be considered a reorg
	reorg, err = cache.Add(15, compacts[5])
	if err != nil {
		t.Fatal(err)
	}
	if reorg {
		t.Fatal("unexpected reorg")
	}
	if len(cache.m) != 4 {
		t.Fatal("unexpected number of blocks")
	}
	if cache.firstBlock != 16-4 {
		t.Fatal("unexpected firstBlock")
	}
	if cache.nextBlock != 16 {
		t.Fatal("unexpected nextBlock")
	}

	// Simulate a reorg by resubmitting as the next block, 16, any block with
	// the wrote prev-hash (let's use the first, just because it's handy)
	reorg, err = cache.Add(16, compacts[0])
	if err != nil {
		t.Fatal(err)
	}
	if !reorg {
		t.Fatal("unexpected non-reorg")
	}
	// The cache shouldn't have changed in any way
	if cache.Get(16) != nil {
		t.Fatal("unexpected block 16 exists")
	}
	if cache.GetLatestHeight() != 15 {
		t.Fatal("unexpected GetLatestHeight")
	}
	if int(cache.Get(15).Height) != 289460+5 {
		t.Fatal("unexpected Get")
	}
	if len(cache.m) != 4 {
		t.Fatal("unexpected number of cache entries")
	}

	// In response to the reorg being detected, we must back up until we
	// reach a block that's before the reorg (where the chain split).
	// Let's back up one block, to height 15, request it from zcashd,
	// but let's say this block is from the new branch, so we haven't
	// gone back far enough, so this will still be disallowed.
	reorg, err = cache.Add(15, compacts[0])
	if err != nil {
		t.Fatal(err)
	}
	if !reorg {
		t.Fatal("unexpected non-reorg")
	}
	// the cache deleted block 15 (it's definitely wrong)
	if cache.Get(15) != nil {
		t.Fatal("unexpected block 15 exists")
	}
	if cache.GetLatestHeight() != 14 {
		t.Fatal("unexpected GetLatestHeight")
	}
	if int(cache.Get(14).Height) != 289460+4 {
		t.Fatal("unexpected Get")
	}
	// now only 3 entries (12-14)
	if len(cache.m) != 3 {
		t.Fatal("unexpected number of cache entries")
	}

	// Back up a couple more, try to re-add height 13, and suppose
	// that's before the split (for example, there were two 14s).
	// (In this test, we're replacing 13 with the same block; in
	// real life, we'd be replacing it with a different version of
	// 13 that has the same prev-hash).
	reorg, err = cache.Add(13, compacts[3])
	if err != nil {
		t.Fatal(err)
	}
	if reorg {
		t.Fatal("unexpected reorg")
	}
	// 13 was replaced (with the same block), but that means
	// everything after 13 is deleted
	if cache.Get(14) != nil {
		t.Fatal("unexpected block 14 exists")
	}
	if cache.GetLatestHeight() != 13 {
		t.Fatal("unexpected GetLatestHeight")
	}
	if int(cache.Get(13).Height) != 289460+3 {
		t.Fatal("unexpected Get")
	}
	if int(cache.Get(12).Height) != 289460+2 {
		t.Fatal("unexpected Get")
	}
	// down to 2 entries (12-13)
	if len(cache.m) != 2 {
		t.Fatal("unexpected number of cache entries")
	}

	// Now we can continue forward from here
	reorg, err = cache.Add(14, compacts[4])
	if err != nil {
		t.Fatal(err)
	}
	if reorg {
		t.Fatal("unexpected reorg")
	}
	if cache.GetLatestHeight() != 14 {
		t.Fatal("unexpected GetLatestHeight")
	}
	if int(cache.Get(14).Height) != 289460+4 {
		t.Fatal("unexpected Get")
	}
	if len(cache.m) != 3 {
		t.Fatal("unexpected number of cache entries")
	}

	// It's possible, although unlikely, that after a reorg is detected,
	// we back up so much that we're before the start of the cache
	// (especially if the cache is very small). This should remove the
	// entire cache before adding the new entry.
	if cache.firstBlock != 12 {
		t.Fatal("unexpected firstBlock")
	}
	reorg, err = cache.Add(10, compacts[0])
	if err != nil {
		t.Fatal(err)
	}
	if reorg {
		t.Fatal("unexpected reorg")
	}
	if cache.GetLatestHeight() != 10 {
		t.Fatal("unexpected GetLatestHeight")
	}
	if int(cache.Get(10).Height) != 289460+0 {
		t.Fatal("unexpected Get")
	}
	if len(cache.m) != 1 {
		t.Fatal("unexpected number of cache entries")
	}

	// Another weird case (not currently possible) is adding a block at
	// a height that is not one higher than the current latest block.
	// This should remove the entire cache before adding the new entry.
	reorg, err = cache.Add(20, compacts[0])
	if err != nil {
		t.Fatal(err)
	}
	if reorg {
		t.Fatal("unexpected reorg")
	}
	if cache.GetLatestHeight() != 20 {
		t.Fatal("unexpected GetLatestHeight")
	}
	if int(cache.Get(20).Height) != 289460 {
		t.Fatal("unexpected Get")
	}
	if len(cache.m) != 1 {
		t.Fatal("unexpected number of cache entries")
	}
	// the cache deleted block 15 (it's definitely wrong)
}
