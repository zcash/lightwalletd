package common

import (
	"bytes"
	"sync"

	"github.com/golang/protobuf/proto"
	"github.com/zcash-hackworks/lightwalletd/walletrpc"
)

type BlockCacheEntry struct {
	data []byte
	hash []byte
}

type BlockCache struct {
	MaxEntries int

	// m[firstBlock..nextBlock) are valid
	m          map[int]*BlockCacheEntry
	firstBlock int
	nextBlock  int

	mutex sync.RWMutex
}

func NewBlockCache(maxEntries int) *BlockCache {
	return &BlockCache{
		MaxEntries: maxEntries,
		m:          make(map[int]*BlockCacheEntry),
	}
}

func (c *BlockCache) Add(height int, block *walletrpc.CompactBlock) (error, bool) {
	// Invariant: m[firstBlock..nextBlock) are valid.
	c.mutex.Lock()
	defer c.mutex.Unlock()

	if height > c.nextBlock {
		// restarting the cache (never happens currently), or first time
		for i := c.firstBlock; i < c.nextBlock; i++ {
			delete(c.m, i)
		}
		c.firstBlock = height
		c.nextBlock = height
	}
	// Invariant: m[firstBlock..nextBlock) are valid.

	// If we already have this block, a reorg must have occurred;
	// this block (and all higher) must be re-added.
	h := height
	if h < c.firstBlock {
		h = c.firstBlock
	}
	for i := h; i < c.nextBlock; i++ {
		delete(c.m, i)
	}
	c.nextBlock = height
	if c.firstBlock > c.nextBlock {
		c.firstBlock = c.nextBlock
	}
	// Invariant: m[firstBlock..nextBlock) are valid.

	// Detect reorg, ingestor needs to handle it
	if height > c.firstBlock && !bytes.Equal(block.PrevHash, c.m[height-1].hash) {
		return nil, true
	}

	// Add the entry and update the counters
	data, err := proto.Marshal(block)
	if err != nil {
		println("Error marshalling block!")
		return err, false
	}
	c.m[height] = &BlockCacheEntry{
		data: data,
		hash: block.GetHash(),
	}
	c.nextBlock++
	// Invariant: m[firstBlock..nextBlock) are valid.

	// remove any blocks that are older than the capacity of the cache
	for c.firstBlock < c.nextBlock-c.MaxEntries {
		// Invariant: m[firstBlock..nextBlock) are valid.
		delete(c.m, c.firstBlock)
		c.firstBlock++
	}
	// Invariant: m[firstBlock..nextBlock) are valid.

	return nil, false
}

func (c *BlockCache) Get(height int) *walletrpc.CompactBlock {
	c.mutex.RLock()
	defer c.mutex.RUnlock()

	if height < c.firstBlock || height >= c.nextBlock {
		return nil
	}

	serialized := &walletrpc.CompactBlock{}
	err := proto.Unmarshal(c.m[height].data, serialized)
	if err != nil {
		println("Error unmarshalling compact block")
		return nil
	}

	return serialized
}

func (c *BlockCache) GetLatestBlock() int {
	c.mutex.RLock()
	defer c.mutex.RUnlock()
	if c.firstBlock == c.nextBlock {
		return -1
	}
	return c.nextBlock - 1
}
