// Copyright (c) 2026 The Zcash developers
// Distributed under the MIT software license, see the accompanying
// file COPYING or https://www.opensource.org/licenses/mit-license.php .
package common

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/zcash/lightwalletd/parser"
	"github.com/zcash/lightwalletd/walletrpc"
	"google.golang.org/protobuf/proto"
)

// No production method is replaced. The hook queues a real Reorg, or repairs
// a disk checksum, while Get holds its read lock. A retained reader then lets
// the test inspect the repaired files before deferred recovery can run.
type cacheReliabilityHook struct {
	t              *testing.T
	c              *BlockCache
	repairHeight   int
	repairRecord   bool
	repairErr      error
	reorgDone      chan struct{}
	recoverySeen   chan struct{}
	readerHeld     chan struct{}
	readerRelease  chan struct{}
	readerDone     chan struct{}
	readerStarted  bool
	reorgStarted   bool
	recoveryQueued bool
	drained        bool
	releaseOnce    sync.Once
	scheduleOnce   sync.Once
}

func (h *cacheReliabilityHook) Levels() []logrus.Level { return []logrus.Level{logrus.WarnLevel} }

func (h *cacheReliabilityHook) Fire(e *logrus.Entry) error {
	if strings.HasPrefix(e.Message, "bad block checksum") && h.reorgDone != nil {
		h.scheduleOnce.Do(func() {
			h.reorgStarted = true
			go func() {
				if h.repairRecord {
					// A single prequeued writer repairs the failed record without
					// changing its height. This covers the re-read guard without
					// relying on FIFO ordering between multiple waiting writers.
					h.c.mutex.Lock()
					var data []byte
					data, h.repairErr = os.ReadFile(h.c.blocksName)
					if h.repairErr == nil {
						offset := h.c.starts[5]
						var f *os.File
						f, h.repairErr = os.OpenFile(h.c.blocksName, os.O_WRONLY, 0600)
						if h.repairErr == nil {
							var n int
							n, h.repairErr = f.WriteAt(checksum(h.repairHeight, data[offset+8:]), offset)
							if h.repairErr == nil && n != 8 {
								h.repairErr = io.ErrShortWrite
							}
							f.Close()
						}
						h.c.Sync()
					}
					h.c.mutex.Unlock()
				} else {
					h.c.Reorg(h.repairHeight)
				}
				close(h.reorgDone)
			}()
			// A queued RWMutex writer prevents further readers. Get itself still
			// holds RLock, so false here proves the repair owns the writer mutex and is
			// waiting for that reader; recovery cannot acquire it before the repair.
			deadline := time.Now().Add(3 * time.Second)
			for h.c.mutex.TryRLock() {
				h.c.mutex.RUnlock()
				if time.Now().After(deadline) {
					h.t.Fatal("repair did not queue before checksum hook deadline")
				}
				runtime.Gosched()
			}
			// Queue a reader behind the repair writer. RWMutex wakes queued
			// readers when that writer unlocks, blocking deferred recovery.
			reliabilityQueueReader(h.t, h)
		})
	}
	if strings.HasPrefix(e.Message, "CORRUPTION detected") && h.recoverySeen != nil {
		close(h.recoverySeen)
	}
	return nil
}

func reliabilityWaitBlocked(t *testing.T, method, semaphore string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		stack := make([]byte, 64*1024)
		n := runtime.Stack(stack, true)
		for _, goroutine := range strings.Split(string(stack[:n]), "\n\n") {
			if strings.Contains(goroutine, method) && strings.Contains(goroutine, semaphore) {
				return
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("%s did not queue", method)
		}
		runtime.Gosched()
	}
}

func reliabilityQueueReader(t *testing.T, h *cacheReliabilityHook) {
	t.Helper()
	h.readerStarted = true
	go reliabilityRetainReader(h)
	reliabilityWaitBlocked(t, "reliabilityRetainReader", "SemacquireRWMutexR")
}

func reliabilityNewReader(c *BlockCache) *cacheReliabilityHook {
	return &cacheReliabilityHook{c: c, readerHeld: make(chan struct{}),
		readerRelease: make(chan struct{}), readerDone: make(chan struct{})}
}

func reliabilityReleaseReader(h *cacheReliabilityHook) {
	if h != nil && h.readerRelease != nil {
		h.releaseOnce.Do(func() { close(h.readerRelease) })
	}
}

func reliabilityWaitWriter(t *testing.T, c *BlockCache) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for c.mutex.TryRLock() {
		c.mutex.RUnlock()
		if time.Now().After(deadline) {
			t.Fatal("writer did not queue")
		}
		runtime.Gosched()
	}
}

func reliabilityDrain(t *testing.T, h *cacheReliabilityHook) {
	t.Helper()
	if h.drained {
		return
	}
	reliabilityReleaseReader(h)
	if h.reorgStarted {
		reliabilityWait(t, h.reorgDone, "repair cleanup")
	}
	if h.readerStarted {
		reliabilityWait(t, h.readerDone, "reader cleanup")
	}
	if !h.recoveryQueued {
		// Assertion-failure cleanup can run before the normal writer barrier.
		// Get already created its worker before returning. Wait until that
		// worker has exited before closing files/restoring the global logger.
		deadline := time.Now().Add(3 * time.Second)
		for {
			stack := make([]byte, 64*1024)
			n := runtime.Stack(stack, true)
			if !strings.Contains(string(stack[:n]), "(*BlockCache).Get.func1(") {
				break
			}
			if time.Now().After(deadline) {
				t.Fatal("recovery did not finish during cleanup")
			}
			runtime.Gosched()
		}
	}
	// On the normal path the retained reader proved recovery owns the writer
	// mutex. Acquiring behind it drains reset and stale-worker no-op alike.
	h.c.mutex.Lock()
	h.c.mutex.Unlock()
	h.drained = true
}

func reliabilityRetainReader(h *cacheReliabilityHook) {
	h.c.mutex.RLock()
	close(h.readerHeld)
	<-h.readerRelease
	h.c.mutex.RUnlock()
	close(h.readerDone)
}

func reliabilityLogger(t *testing.T, hook logrus.Hook) {
	t.Helper()
	old := Log
	l := logrus.New()
	l.SetOutput(io.Discard)
	if hook != nil {
		l.AddHook(hook)
	}
	Log = logrus.NewEntry(l)
	t.Cleanup(func() { Log = old })
}

func reliabilityFilledCache(t *testing.T, firstHeight int) (*BlockCache, string, []*walletrpc.CompactBlock) {
	t.Helper()
	var fixtures []struct {
		Full string `json:"full"`
	}
	b, err := os.ReadFile("../testdata/compact_blocks.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, &fixtures); err != nil {
		t.Fatal(err)
	}
	if len(fixtures) < 6 {
		t.Fatal("need six consecutive compact-block fixtures")
	}
	dir := t.TempDir()
	c := NewBlockCache(dir, "unittestnet", firstHeight, -1)
	var blocks []*walletrpc.CompactBlock
	for i, fixture := range fixtures[:6] {
		raw, err := hex.DecodeString(fixture.Full)
		if err != nil {
			t.Fatal(err)
		}
		p := parser.NewBlock()
		rest, err := p.ParseFromSlice(raw)
		if err != nil || len(rest) != 0 {
			t.Fatalf("fixture parse: %v; trailing bytes %d", err, len(rest))
		}
		block := p.ToCompact()
		// Keep real fixture contents/hashes at the requested cache heights.
		block.Height = uint64(firstHeight + i)
		if err := c.Add(firstHeight+i, block); err != nil {
			t.Fatal(err)
		}
		blocks = append(blocks, block)
	}
	c.Sync()
	for i, want := range blocks {
		if got := c.Get(firstHeight + i); got == nil || !proto.Equal(got, want) {
			t.Fatalf("initial real disk read at height %d failed", i)
		}
	}
	return c, dir, blocks
}

func reliabilityReadFile(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func reliabilityCorruptTip(t *testing.T, c *BlockCache) {
	t.Helper()
	f, err := os.OpenFile(c.blocksName, os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	b := reliabilityReadFile(t, c.blocksName)
	offset := c.starts[5]
	// An ordinary damaged record: change one byte of the tip checksum.
	if _, err := f.WriteAt([]byte{b[offset] ^ 1}, offset); err != nil {
		t.Fatal(err)
	}
}

func reliabilityWait(t *testing.T, ch <-chan struct{}, what string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(3 * time.Second):
		t.Fatalf("timed out waiting for %s", what)
	}
}

func TestCacheRecoveryCleanReorgRetainsPrefix(t *testing.T) {
	reliabilityLogger(t, nil)
	c, dir, blocks := reliabilityFilledCache(t, 0)
	c.Reorg(5)
	if c.GetNextHeight() != 5 {
		t.Fatal("clean Reorg did not retain five blocks")
	}
	for i, want := range blocks[:5] {
		if got := c.Get(i); got == nil || !proto.Equal(got, want) {
			t.Fatalf("clean Reorg lost block %d", i)
		}
	}
	c.Close()
	c = NewBlockCache(dir, "unittestnet", 0, -1)
	defer c.Close()
	if c.GetNextHeight() != 5 {
		t.Fatal("clean Reorg prefix not preserved after restart")
	}
	t.Log("control: clean Reorg preserved all five valid blocks after restart")
}

func TestCacheRecoveryUnrepairedCorruptionClearsCache(t *testing.T) {
	reliabilityLogger(t, nil)
	c, dir, _ := reliabilityFilledCache(t, 0)
	defer c.Close()
	h := &cacheReliabilityHook{t: t, c: c, recoverySeen: make(chan struct{})}
	Log.Logger.AddHook(h)
	reliabilityCorruptTip(t, c)
	if got := c.Get(5); got != nil {
		t.Fatal("unrepaired damaged checksum was served")
	}
	reliabilityWait(t, h.recoverySeen, "unrepaired-corruption recovery")
	if c.GetNextHeight() != 0 {
		t.Fatal("unrepaired corruption did not reset the cache")
	}
	lengths, blocks := DbFileNames(dir, "unittestnet")
	if len(reliabilityReadFile(t, lengths)) != 0 || len(reliabilityReadFile(t, blocks)) != 0 {
		t.Fatal("unrepaired-corruption reset left cache data")
	}
	t.Log("control: unrepaired corruption correctly cleared the cache")
}

func TestCacheRecoveryAfterReorgPreservesValidPrefix(t *testing.T) {
	reliabilityRepairedCache(t, 0, false)
}

func TestCacheRecoveryAfterRecordRepairPreservesValidCache(t *testing.T) {
	reliabilityRepairedCache(t, 0, true)
}

func TestCacheRecoveryAfterReorgWithNonzeroStartPreservesValidPrefix(t *testing.T) {
	// This exercises a stale-worker no-op, not the nonzero-start reset-height
	// behavior covered by the separate cache-restart correction.
	reliabilityRepairedCache(t, 289460, false)
}

func reliabilityRepairedCache(t *testing.T, firstHeight int, repairRecord bool) {
	t.Helper()
	reliabilityLogger(t, nil)
	c, dir, fixtures := reliabilityFilledCache(t, firstHeight)
	height := firstHeight + 5
	h := reliabilityNewReader(c)
	h.t, h.repairHeight, h.reorgDone = t, height, make(chan struct{})
	want := append([]*walletrpc.CompactBlock(nil), fixtures[:5]...)
	h.repairRecord = repairRecord
	if repairRecord {
		want = append(want, fixtures[5])
	}
	// Registered after logger restoration so even assertion-failure cleanup
	// releases readers/drains writers before Close or changing the logger.
	t.Cleanup(func() {
		reliabilityDrain(t, h)
		c.Close()
	})
	Log.Logger.AddHook(h)
	lengths, blocks := DbFileNames(dir, "unittestnet")
	wantLengths := reliabilityReadFile(t, lengths)
	wantBlocks := reliabilityReadFile(t, blocks)
	if !repairRecord {
		wantLengths = wantLengths[:20]
		wantBlocks = wantBlocks[:c.starts[5]]
	}
	reliabilityCorruptTip(t, c)

	if got := c.Get(height); got != nil {
		t.Fatal("damaged checksum was served")
	}
	reliabilityWait(t, h.reorgDone, "repair completion")
	if h.repairErr != nil {
		t.Fatal(h.repairErr)
	}
	reliabilityWait(t, h.readerHeld, "retained reader after repair")
	// Recovery is the only remaining writer. It owns the writer mutex and
	// cannot proceed until this reader releases; no writer FIFO is assumed.
	reliabilityWaitWriter(t, c)
	h.recoveryQueued = true
	if !bytes.Equal(reliabilityReadFile(t, lengths), wantLengths) ||
		!bytes.Equal(reliabilityReadFile(t, blocks), wantBlocks) {
		t.Fatal("repair did not restore exactly the original valid cache contents")
	}
	t.Logf("repair completed before recovery: retained %d valid blocks, %d length bytes, %d block bytes",
		len(want), len(wantLengths), len(wantBlocks))
	reliabilityDrain(t, h)
	gotNext := c.GetNextHeight()
	t.Logf("after delayed recovery: nextHeight=%d, lengths=%d bytes, blocks=%d bytes",
		gotNext, len(reliabilityReadFile(t, lengths)), len(reliabilityReadFile(t, blocks)))
	wantNext := firstHeight + len(want)
	if gotNext != wantNext {
		t.Errorf("delayed recovery lost a repaired cache: nextHeight=%d, want %d", gotNext, wantNext)
	} else {
		for i, block := range want {
			if got := c.Get(firstHeight + i); got == nil || !proto.Equal(got, block) {
				t.Errorf("repaired block %d is not readable after recovery", firstHeight+i)
			}
		}
	}
	c.Close()
	restarted := NewBlockCache(dir, "unittestnet", firstHeight, -1)
	t.Cleanup(restarted.Close)
	t.Logf("after restart: nextHeight=%d", restarted.GetNextHeight())
	if restarted.GetNextHeight() != wantNext {
		t.Fatalf("repaired cache lost after restart: nextHeight=%d, want %d", restarted.GetNextHeight(), wantNext)
	}
	for i, block := range want {
		if got := restarted.Get(firstHeight + i); got == nil || !proto.Equal(got, block) {
			t.Errorf("repaired block %d is not readable after restart", firstHeight+i)
		}
	}
}
