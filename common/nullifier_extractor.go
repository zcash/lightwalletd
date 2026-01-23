// Copyright (c) 2019-2020 The Zcash developers
// Distributed under the MIT software license, see the accompanying
// file COPYING or https://www.opensource.org/licenses/mit-license.php .

package common

import (
	"context"
	"encoding/hex"
	"sync"
	"sync/atomic"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/zcash/lightwalletd/pirclient"
	"github.com/zcash/lightwalletd/walletrpc"
)

const (
	// MaxConcurrentIngests limits the number of concurrent PIR ingestion requests
	// to prevent unbounded goroutine growth during rapid block processing.
	MaxConcurrentIngests = 10

	// DefaultPirRequestTimeout is the default timeout for PIR service requests
	// when no context timeout is provided.
	DefaultPirRequestTimeout = 30 * time.Second
)

// NullifierExtractor extracts Orchard nullifiers from blocks and sends them to the PIR service.
type NullifierExtractor struct {
	pirClient        *pirclient.Client
	mu               sync.Mutex
	enabled          bool
	semaphore        chan struct{} // Limits concurrent goroutines
	pendingIngests   int64         // Atomic counter for monitoring
	failedIngests    int64         // Atomic counter for failed ingestions
	requestTimeout   time.Duration // Timeout for PIR requests
}

// NewNullifierExtractor creates a new NullifierExtractor.
func NewNullifierExtractor(pirClient *pirclient.Client) *NullifierExtractor {
	return NewNullifierExtractorWithTimeout(pirClient, DefaultPirRequestTimeout)
}

// NewNullifierExtractorWithTimeout creates a new NullifierExtractor with a custom request timeout.
func NewNullifierExtractorWithTimeout(pirClient *pirclient.Client, timeout time.Duration) *NullifierExtractor {
	return &NullifierExtractor{
		pirClient:      pirClient,
		enabled:        pirClient != nil && pirClient.IsEnabled(),
		semaphore:      make(chan struct{}, MaxConcurrentIngests),
		requestTimeout: timeout,
	}
}

// GetPendingIngests returns the number of pending ingestion requests.
func (e *NullifierExtractor) GetPendingIngests() int64 {
	return atomic.LoadInt64(&e.pendingIngests)
}

// GetFailedIngests returns the number of failed ingestion requests.
func (e *NullifierExtractor) GetFailedIngests() int64 {
	return atomic.LoadInt64(&e.failedIngests)
}

// IsEnabled returns true if the extractor is enabled.
func (e *NullifierExtractor) IsEnabled() bool {
	return e.enabled
}

// ExtractAndSend extracts Orchard nullifiers from a CompactBlock and sends them to the PIR service.
// This method is designed to be called during block ingestion.
// Errors are logged but do not block block processing.
// The method uses a semaphore to limit concurrent goroutines and prevent resource exhaustion.
func (e *NullifierExtractor) ExtractAndSend(ctx context.Context, block *walletrpc.CompactBlock) {
	if !e.enabled || block == nil {
		return
	}

	// Extract nullifiers from all transactions in the block (under lock for thread safety)
	e.mu.Lock()
	var nullifiers []pirclient.IngestNullifierEntry

	for txIndex, tx := range block.Vtx {
		// Extract Orchard nullifiers from actions
		for _, action := range tx.Actions {
			if len(action.Nullifier) == 32 {
				nullifiers = append(nullifiers, pirclient.IngestNullifierEntry{
					Nullifier: hex.EncodeToString(action.Nullifier),
					TxIndex:   uint16(txIndex),
				})
			}
		}
	}
	e.mu.Unlock()

	// Only send if there are nullifiers to ingest
	if len(nullifiers) == 0 {
		return
	}

	// Build the request
	req := &pirclient.IngestRequest{
		BlockHeight: block.Height,
		BlockHash:   hex.EncodeToString(block.Hash),
		Nullifiers:  nullifiers,
	}

	// Capture block info for goroutine
	blockHeight := block.Height
	numNullifiers := len(nullifiers)

	// Try to acquire semaphore (non-blocking check first)
	select {
	case e.semaphore <- struct{}{}:
		// Got semaphore, proceed with goroutine
	default:
		// Semaphore full - log warning and skip to avoid blocking
		Log.WithFields(logrus.Fields{
			"height":           blockHeight,
			"nullifiers":       numNullifiers,
			"pending_ingests":  atomic.LoadInt64(&e.pendingIngests),
			"max_concurrent":   MaxConcurrentIngests,
		}).Warn("PIR ingestion queue full, skipping block (will be reingested on next sync)")
		atomic.AddInt64(&e.failedIngests, 1)
		return
	}

	// Track pending ingests
	atomic.AddInt64(&e.pendingIngests, 1)

	// Send to PIR service (non-blocking, errors are logged)
	go func() {
		defer func() {
			<-e.semaphore // Release semaphore
			atomic.AddInt64(&e.pendingIngests, -1)
		}()

		// Create context with timeout if the provided context doesn't have one
		reqCtx := ctx
		if _, hasDeadline := ctx.Deadline(); !hasDeadline {
			var cancel context.CancelFunc
			reqCtx, cancel = context.WithTimeout(ctx, e.requestTimeout)
			defer cancel()
		}

		resp, err := e.pirClient.IngestNullifiers(reqCtx, req)
		if err != nil {
			atomic.AddInt64(&e.failedIngests, 1)
			Log.WithFields(logrus.Fields{
				"height":     blockHeight,
				"nullifiers": numNullifiers,
				"error":      err,
			}).Error("Failed to ingest nullifiers to PIR service")
			return
		}

		Log.WithFields(logrus.Fields{
			"height":        blockHeight,
			"nullifiers":    numNullifiers,
			"queued_blocks": resp.QueuedBlocks,
			"pir_height":    resp.CurrentPirHeight,
		}).Debug("Nullifiers ingested to PIR service")
	}()
}

// HandleReorg notifies the PIR service of a chain reorganization.
// This should be called when the block cache detects a reorg.
// Reorg notifications are critical, so this method will retry on failure.
func (e *NullifierExtractor) HandleReorg(ctx context.Context, reorgHeight uint64) {
	if !e.enabled {
		return
	}

	// Try to acquire semaphore
	select {
	case e.semaphore <- struct{}{}:
		// Got semaphore
	default:
		// Semaphore full - reorg is critical, log error
		Log.WithFields(logrus.Fields{
			"reorg_height":    reorgHeight,
			"pending_ingests": atomic.LoadInt64(&e.pendingIngests),
		}).Error("PIR ingestion queue full, reorg notification delayed")
		// For reorgs, we wait for semaphore since this is critical
		e.semaphore <- struct{}{}
	}

	atomic.AddInt64(&e.pendingIngests, 1)

	// Send reorg notification (non-blocking)
	go func() {
		defer func() {
			<-e.semaphore
			atomic.AddInt64(&e.pendingIngests, -1)
		}()

		// Create context with timeout if needed
		reqCtx := ctx
		if _, hasDeadline := ctx.Deadline(); !hasDeadline {
			var cancel context.CancelFunc
			reqCtx, cancel = context.WithTimeout(ctx, e.requestTimeout)
			defer cancel()
		}

		resp, err := e.pirClient.HandleReorg(reqCtx, reorgHeight)
		if err != nil {
			atomic.AddInt64(&e.failedIngests, 1)
			Log.WithFields(logrus.Fields{
				"reorg_height": reorgHeight,
				"error":        err,
			}).Error("Failed to notify PIR service of reorg")
			return
		}

		Log.WithFields(logrus.Fields{
			"reorg_height":   reorgHeight,
			"blocks_removed": resp.BlocksRemoved,
			"new_height":     resp.NewHeight,
		}).Info("PIR service notified of reorg")
	}()
}

// Global extractor instance (set during startup)
var NullifierExtractorInstance *NullifierExtractor

// SetNullifierExtractor sets the global nullifier extractor instance.
func SetNullifierExtractor(extractor *NullifierExtractor) {
	NullifierExtractorInstance = extractor
}

// GetNullifierExtractor returns the global nullifier extractor instance.
func GetNullifierExtractor() *NullifierExtractor {
	return NullifierExtractorInstance
}
