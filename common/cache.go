// Copyright (c) 2019-present The Zcash developers
// Distributed under the MIT software license, see the accompanying
// file COPYING or https://www.opensource.org/licenses/mit-license.php .

// Package common contains utilities that are shared by other packages.
package common

import (
	"bytes"
	"encoding/binary"
	"hash/fnv"
	"os"
	"path/filepath"
	"sync"

	"github.com/zcash/lightwalletd/hash32"
	"github.com/zcash/lightwalletd/walletrpc"
	"google.golang.org/protobuf/proto"
)

// BlockCache contains a consecutive set of recent compact blocks in marshalled form.
type BlockCache struct {
	lengthsName, blocksName string // pathnames
	lengthsFile, blocksFile *os.File
	starts                  []int64  // Starting offset of each block within blocksFile
	firstBlock              int      // height of the first block in the cache (usually Sapling activation)
	nextBlock               int      // height of the first block not in the cache
	latestHash              hash32.T // hash of the most recent (highest height) block, for detecting reorgs.
	mutex                   sync.RWMutex
}

// GetNextHeight returns the height of the lowest unobtained block.
func (c *BlockCache) GetNextHeight() int {
	c.mutex.RLock()
	defer c.mutex.RUnlock()
	return c.nextBlock
}

// GetFirstHeight returns the height of the lowest block (usually Sapling activation).
func (c *BlockCache) GetFirstHeight() int {
	c.mutex.RLock()
	defer c.mutex.RUnlock()
	return c.firstBlock
}

// GetLatestHash returns the hash (block ID) of the most recent (highest) known block.
func (c *BlockCache) GetLatestHash() hash32.T {
	c.mutex.RLock()
	defer c.mutex.RUnlock()
	return c.latestHash
}

// HashMatch indicates if the given prev-hash matches the most recent block's hash
// so reorgs can be detected.
func (c *BlockCache) HashMatch(prevhash hash32.T) bool {
	c.mutex.RLock()
	defer c.mutex.RUnlock()
	return c.latestHash == hash32.Nil || c.latestHash == prevhash
}

// Reset the database files to the empty state.
// Caller should hold c.mutex.Lock().
func (c *BlockCache) clearDbFiles() {
	if err := c.lengthsFile.Truncate(0); err != nil {
		Log.Fatal("truncate lengths file failed: ", err)
	}
	if err := c.blocksFile.Truncate(0); err != nil {
		Log.Fatal("truncate blocks file failed: ", err)
	}
	c.Sync()
	c.starts = c.starts[:1]
	c.nextBlock = 0
	c.latestHash = hash32.Nil
}

// Caller should hold c.mutex.Lock().
func (c *BlockCache) recoverFromCorruption() {
	Log.Warning("CORRUPTION detected in db blocks-cache files, redownloading")
	c.clearDbFiles()
}

// not including the checksum
func (c *BlockCache) blockLength(height int) int {
	index := height - c.firstBlock
	return int(c.starts[index+1] - c.starts[index] - 8)
}

// Calculate the 8-byte checksum that precedes each block in the blocks file.
func checksum(height int, b []byte) []byte {
	h := make([]byte, 8)
	binary.LittleEndian.PutUint64(h, uint64(height))
	cs := fnv.New64a()
	cs.Write(h)
	cs.Write(b)
	return cs.Sum(nil)
}

// Caller should hold (at least) c.mutex.RLock().
func (c *BlockCache) readBlock(height int) *walletrpc.CompactBlock {
	blockLen := c.blockLength(height)
	b := make([]byte, blockLen+8)
	offset := c.starts[height-c.firstBlock]
	n, err := c.blocksFile.ReadAt(b, offset)
	if err != nil || n != len(b) {
		Log.Warning("blocks read offset: ", offset, " failed: ", n, err)
		return nil
	}
	diskcs := b[:8]
	b = b[8 : blockLen+8]
	if !bytes.Equal(checksum(height, b), diskcs) {
		Log.Warning("bad block checksum at height: ", height, " offset: ", offset)
		return nil
	}
	block := &walletrpc.CompactBlock{}
	err = proto.Unmarshal(b, block)
	if err != nil {
		// Could be file corruption.
		Log.Warning("blocks unmarshal at offset: ", offset, " failed: ", err)
		return nil
	}
	if int(block.Height) != height {
		// Could be file corruption.
		Log.Warning("block unexpected height at height ", height, " offset: ", offset)
		return nil
	}
	return block
}

// Caller should hold c.mutex.Lock().
func (c *BlockCache) setLatestHash() {
	c.latestHash = hash32.Nil
	// There is at least one block; get the last block's hash
	if c.nextBlock > c.firstBlock {
		// At least one block remains; get the last block's hash
		block := c.readBlock(c.nextBlock - 1)
		if block == nil {
			c.recoverFromCorruption()
			return
		}
		c.latestHash = hash32.FromSlice(block.Hash)
	}
}

// Reset is used only for darkside testing.
func (c *BlockCache) Reset(startHeight int) {
	c.clearDbFiles() // empty the cache
	c.firstBlock = startHeight
	c.nextBlock = startHeight
}

// NewBlockCache returns an instance of a block cache object.
// (No locking here, we assume this is single-threaded.)
// syncFromHeight < 0 means latest (tip) height.
func NewBlockCache(dbPath string, chainName string, startHeight int, syncFromHeight int) *BlockCache {
	c := &BlockCache{}
	c.firstBlock = startHeight
	c.nextBlock = startHeight
	c.lengthsName, c.blocksName = DbFileNames(dbPath, chainName)
	var err error
	if err := os.MkdirAll(filepath.Join(dbPath, chainName), 0755); err != nil {
		Log.Fatal("mkdir ", dbPath, " failed: ", err)
	}
	c.blocksFile, err = os.OpenFile(c.blocksName, os.O_CREATE|os.O_RDWR|os.O_APPEND, 0644)
	if err != nil {
		Log.Fatal("open ", c.blocksName, " failed: ", err)
	}
	c.lengthsFile, err = os.OpenFile(c.lengthsName, os.O_CREATE|os.O_RDWR|os.O_APPEND, 0644)
	if err != nil {
		Log.Fatal("open ", c.lengthsName, " failed: ", err)
	}
	lengths, err := os.ReadFile(c.lengthsName)
	if err != nil {
		Log.Fatal("read ", c.lengthsName, " failed: ", err)
	}
	// 4 bytes per lengths[] value (block length)
	if syncFromHeight >= 0 {
		if syncFromHeight < startHeight {
			syncFromHeight = startHeight
		}
		if (syncFromHeight-startHeight)*4 < len(lengths) {
			// discard the entries at and beyond (newer than) the specified height
			lengths = lengths[:(syncFromHeight-startHeight)*4]
		}
	}

	// The last entry in starts[] is where to write the next block.
	var offset int64
	c.starts = nil
	c.starts = append(c.starts, 0)
	nBlocks := len(lengths) / 4
	Log.Info("Reading ", nBlocks, " blocks from the cache ...")
	for i := 0; i < nBlocks; i++ {
		if len(lengths[:4]) < 4 {
			Log.Warning("lengths file has a partial entry")
			c.recoverFromCorruption()
			break
		}
		length := binary.LittleEndian.Uint32(lengths[i*4 : (i+1)*4])
		if length < 74 || length > 4*1000*1000 {
			Log.Warning("lengths file has impossible value ", length)
			c.recoverFromCorruption()
			break
		}
		offset += int64(length) + 8
		c.starts = append(c.starts, offset)

		// After the changes that store transparent transaction data, the cache
		// starts at block height zero, not the Sapling activation height.
		// If the first block does not deserialize (the checksum depends on height),
		// we're probably running on an old data (cache) directory, so we must
		// rebuild the cache.
		if i == 0 && chainName != "unittestnet" {
			block := c.readBlock(c.nextBlock)
			if block == nil {
				Log.Warning("first block is incorrect, likely upgrading, recreating the cache")
				Log.Warning("  this will take a few hours but the server is available immediately")
				c.clearDbFiles()
				break
			}
		}
		c.nextBlock++
	}
	Log.Info("Done reading ", c.nextBlock-c.firstBlock, " blocks from disk cache")
	return c
}

func DbFileNames(dbPath string, chainName string) (string, string) {
	return filepath.Join(dbPath, chainName, "lengths"),
		filepath.Join(dbPath, chainName, "blocks")
}

// Add adds the given block to the cache at the given height, returning true
// if a reorg was detected.
func (c *BlockCache) Add(height int, block *walletrpc.CompactBlock) error {
	// Invariant: m[firstBlock..nextBlock) are valid.
	c.mutex.Lock()
	defer c.mutex.Unlock()

	if height > c.nextBlock {
		// Cache has been reset (for example, checksum error)
		return nil
	}
	if height < c.firstBlock {
		// Should never try to add a block before Sapling activation height
		Log.Fatal("cache.Add height below Sapling: ", height)
		return nil
	}
	if height < c.nextBlock {
		// Should never try to "backup" (call Reorg() instead).
		Log.Fatal("cache.Add height going backwards: ", height)
		return nil
	}
	bheight := int(block.Height)

	if bheight != height {
		// This could only happen if zcashd returned the wrong
		// block (not the height we requested).
		Log.Fatal("cache.Add wrong height: ", bheight, " expecting: ", height)
		return nil
	}

	// Add the new block and its length to the db files.
	data, err := proto.Marshal(block)
	if err != nil {
		return err
	}
	b := append(checksum(height, data), data...)
	n, err := c.blocksFile.Write(b)
	if err != nil {
		Log.Fatal("blocks write failed: ", err)
	}
	if n != len(b) {
		Log.Fatal("blocks write incorrect length: expected: ", len(b), "written: ", n)
	}
	b = make([]byte, 4)
	binary.LittleEndian.PutUint32(b, uint32(len(data)))
	n, err = c.lengthsFile.Write(b)
	if err != nil {
		Log.Fatal("lengths write failed: ", err)
	}
	if n != len(b) {
		Log.Fatal("lengths write incorrect length: expected: ", len(b), "written: ", n)
	}

	// update the in-memory variables
	offset := c.starts[len(c.starts)-1]
	c.starts = append(c.starts, offset+int64(len(data)+8))

	c.latestHash = hash32.FromSlice(block.Hash)
	c.nextBlock++
	// Invariant: m[firstBlock..nextBlock) are valid.
	return nil
}

// Reorg resets nextBlock (the block that should be Add()ed next)
// downward to the given height.
func (c *BlockCache) Reorg(height int) {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	// Allow the caller not to have to worry about Sapling start height.
	if height < c.firstBlock {
		height = c.firstBlock
	}
	if height >= c.nextBlock {
		// Timing window, ignore this request
		return
	}
	// Remove the end of the cache.
	c.nextBlock = height
	newCacheLen := height - c.firstBlock
	c.starts = c.starts[:newCacheLen+1]

	if err := c.lengthsFile.Truncate(int64(4 * newCacheLen)); err != nil {
		Log.Fatal("truncate failed: ", err)
	}
	if err := c.blocksFile.Truncate(c.starts[newCacheLen]); err != nil {
		Log.Fatal("truncate failed: ", err)
	}
	c.setLatestHash()
}

// Get returns the compact block at the requested height if it's
// in the cache, else nil.
func (c *BlockCache) Get(height int) *walletrpc.CompactBlock {
	c.mutex.RLock()
	defer c.mutex.RUnlock()

	if height < c.firstBlock || height >= c.nextBlock {
		return nil
	}
	block := c.readBlock(height)
	if block == nil {
		go func() {
			// We hold only the read lock, need the exclusive lock.
			c.mutex.Lock()
			c.recoverFromCorruption()
			c.mutex.Unlock()
		}()
		return nil
	}
	return block
}

// GetLatestHeight returns the height of the most recent block, or -1
// if the cache is empty.
func (c *BlockCache) GetLatestHeight() int {
	c.mutex.RLock()
	defer c.mutex.RUnlock()
	if c.firstBlock == c.nextBlock {
		return -1
	}
	return c.nextBlock - 1
}

// Sync ensures that the db files are flushed to disk, can be called unnecessarily.
func (c *BlockCache) Sync() {
	c.lengthsFile.Sync()
	c.blocksFile.Sync()
}

// Close is Currently used only for testing.
func (c *BlockCache) Close() {
	// Some operating system require you to close files before you can remove them.
	if c.lengthsFile != nil {
		c.lengthsFile.Close()
		c.lengthsFile = nil
	}
	if c.blocksFile != nil {
		c.blocksFile.Close()
		c.blocksFile = nil
	}
}
