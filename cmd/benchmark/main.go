// Copyright (c) 2019-2020 The Zcash developers
// Distributed under the MIT software license, see the accompanying
// file COPYING or https://www.opensource.org/licenses/mit-license.php .

// Package main provides a benchmark tool for comparing trial decryption vs PIR queries.
//
// Key insight: PIR queries must touch EVERY element in the database to preserve privacy.
// With ~52M historical Orchard nullifiers (~2GB), each PIR query processes the full DB.
package main

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/zcash/lightwalletd/pirclient"
	"github.com/zcash/lightwalletd/walletrpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// BenchmarkConfig holds the configuration for a benchmark run.
type BenchmarkConfig struct {
	// PIR service URL
	PirServiceURL string
	// Lightwalletd gRPC address
	LwdAddress string
	// Number of PIR queries to run
	NumPirQueries int
	// Block range for trial decryption test
	StartHeight uint64
	EndHeight   uint64
	// Output format: "text" or "json"
	OutputFormat string
}

// BenchmarkResult holds the results of a benchmark run.
type BenchmarkResult struct {
	Config          BenchmarkConfig        `json:"config"`
	PirStatus       *PirStatusResult       `json:"pir_status,omitempty"`
	TrialDecrypt    *TrialDecryptResult    `json:"trial_decrypt,omitempty"`
	PirQueries      *PirQueryResult        `json:"pir_queries,omitempty"`
	Comparison      *ComparisonResult      `json:"comparison,omitempty"`
}

// PirStatusResult holds PIR service status information.
type PirStatusResult struct {
	Available       bool   `json:"available"`
	Status          string `json:"status"`
	NumNullifiers   int    `json:"num_nullifiers"`
	NumBuckets      int    `json:"num_buckets"`
	PirDbHeight     uint64 `json:"pir_db_height"`
	EstimatedDbSize string `json:"estimated_db_size"`
}

// TrialDecryptResult holds trial decryption benchmark results.
type TrialDecryptResult struct {
	BlocksRequested   int     `json:"blocks_requested"`
	BlocksReceived    int     `json:"blocks_received"`
	NullifiersTotal   int     `json:"nullifiers_total"`
	DownloadTimeMs    float64 `json:"download_time_ms"`
	BandwidthBytes    int64   `json:"bandwidth_bytes"`
	BandwidthPerBlock float64 `json:"bandwidth_per_block_bytes"`
	AvgNullifiersPerBlock float64 `json:"avg_nullifiers_per_block"`
}

// PirQueryResult holds PIR query benchmark results.
type PirQueryResult struct {
	NumQueries       int       `json:"num_queries"`
	QueryTimes       []float64 `json:"query_times_ms"`
	MinTimeMs        float64   `json:"min_time_ms"`
	MaxTimeMs        float64   `json:"max_time_ms"`
	AvgTimeMs        float64   `json:"avg_time_ms"`
	MedianTimeMs     float64   `json:"median_time_ms"`
	TotalTimeMs      float64   `json:"total_time_ms"`
	AvgQuerySizeBytes int64    `json:"avg_query_size_bytes"`
	AvgResponseSizeBytes int64 `json:"avg_response_size_bytes"`
}

// ComparisonResult holds the comparison between approaches.
type ComparisonResult struct {
	// For checking N nullifiers over the block range:
	TrialDecryptTimeMs    float64 `json:"trial_decrypt_time_ms"`
	TrialDecryptBandwidth int64   `json:"trial_decrypt_bandwidth_bytes"`

	// PIR time scales with number of queries, not database size for bandwidth
	PirTimePerQueryMs     float64 `json:"pir_time_per_query_ms"`
	PirBandwidthPerQuery  int64   `json:"pir_bandwidth_per_query_bytes"`

	// Break-even: how many nullifier checks before trial decrypt is faster
	BreakEvenQueries      int     `json:"break_even_queries"`

	// Analysis notes
	Notes                 []string `json:"notes"`
}

func main() {
	pirURL := flag.String("pir-url", "", "PIR service URL (required for PIR benchmarks)")
	lwdAddr := flag.String("lwd-addr", "", "Lightwalletd gRPC address (required for trial decrypt benchmarks)")
	numQueries := flag.Int("queries", 5, "Number of PIR queries to benchmark")
	startHeight := flag.Uint64("start-height", 0, "Start block height for trial decryption")
	endHeight := flag.Uint64("end-height", 0, "End block height for trial decryption")
	outputFormat := flag.String("format", "text", "Output format: text or json")

	// Convenience flags
	lastNBlocks := flag.Uint64("last-blocks", 0, "Benchmark last N blocks (alternative to start/end height)")

	flag.Parse()

	if *pirURL == "" && *lwdAddr == "" {
		fmt.Fprintln(os.Stderr, "Error: must specify at least one of --pir-url or --lwd-addr")
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "Usage:")
		fmt.Fprintln(os.Stderr, "  # Benchmark PIR queries against real service")
		fmt.Fprintln(os.Stderr, "  go run ./cmd/benchmark --pir-url http://localhost:8080 --queries 5")
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "  # Benchmark trial decryption via lightwalletd")
		fmt.Fprintln(os.Stderr, "  go run ./cmd/benchmark --lwd-addr localhost:9067 --last-blocks 1008")
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "  # Full comparison")
		fmt.Fprintln(os.Stderr, "  go run ./cmd/benchmark --pir-url http://localhost:8080 --lwd-addr localhost:9067 --last-blocks 1008 --queries 5")
		os.Exit(1)
	}

	cfg := BenchmarkConfig{
		PirServiceURL: *pirURL,
		LwdAddress:    *lwdAddr,
		NumPirQueries: *numQueries,
		StartHeight:   *startHeight,
		EndHeight:     *endHeight,
		OutputFormat:  *outputFormat,
	}

	// Handle --last-blocks convenience flag
	if *lastNBlocks > 0 && cfg.LwdAddress != "" {
		// Get current height from lightwalletd
		conn, err := grpc.Dial(cfg.LwdAddress, grpc.WithTransportCredentials(insecure.NewCredentials()))
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to connect to lightwalletd: %v\n", err)
			os.Exit(1)
		}
		client := walletrpc.NewCompactTxStreamerClient(conn)
		latestBlock, err := client.GetLatestBlock(context.Background(), &walletrpc.ChainSpec{})
		conn.Close()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to get latest block: %v\n", err)
			os.Exit(1)
		}
		cfg.EndHeight = latestBlock.Height
		if latestBlock.Height > *lastNBlocks {
			cfg.StartHeight = latestBlock.Height - *lastNBlocks + 1
		} else {
			cfg.StartHeight = 1
		}
	}

	result, err := RunBenchmark(cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Benchmark failed: %v\n", err)
		os.Exit(1)
	}

	PrintResults(result, cfg.OutputFormat)
}

// RunBenchmark runs the full benchmark suite.
func RunBenchmark(cfg BenchmarkConfig) (*BenchmarkResult, error) {
	ctx := context.Background()
	result := &BenchmarkResult{
		Config: cfg,
	}

	// PIR benchmarks
	if cfg.PirServiceURL != "" {
		pirClient := pirclient.NewClient(cfg.PirServiceURL, 5*time.Minute) // Long timeout for PIR queries

		// Get PIR status
		fmt.Println("Fetching PIR service status...")
		status, err := pirClient.GetStatus(ctx)
		if err != nil {
			fmt.Printf("Warning: Could not get PIR status: %v\n", err)
		} else {
			// Estimate DB size: each nullifier is 32 bytes, plus Cuckoo overhead (~1.25x)
			estimatedBytes := int64(status.NumNullifiers) * 32 * 125 / 100
			result.PirStatus = &PirStatusResult{
				Available:       true,
				Status:          status.Status,
				NumNullifiers:   status.NumNullifiers,
				NumBuckets:      status.NumBuckets,
				PirDbHeight:     status.PirDbHeight,
				EstimatedDbSize: formatBytes(estimatedBytes),
			}
			fmt.Printf("  Status: %s\n", status.Status)
			fmt.Printf("  Nullifiers: %d\n", status.NumNullifiers)
			fmt.Printf("  Estimated DB size: %s\n", result.PirStatus.EstimatedDbSize)
		}

		// Run PIR query benchmarks
		if status != nil && status.Status == "ready" {
			fmt.Printf("\nRunning %d PIR queries...\n", cfg.NumPirQueries)
			pirResult, err := BenchmarkPirQueries(ctx, pirClient, cfg.NumPirQueries)
			if err != nil {
				fmt.Printf("Warning: PIR query benchmark failed: %v\n", err)
			} else {
				result.PirQueries = pirResult
			}
		}
	}

	// Trial decryption benchmarks
	if cfg.LwdAddress != "" && cfg.StartHeight > 0 && cfg.EndHeight > 0 {
		fmt.Printf("\nBenchmarking trial decryption for blocks %d-%d...\n", cfg.StartHeight, cfg.EndHeight)

		conn, err := grpc.Dial(cfg.LwdAddress, grpc.WithTransportCredentials(insecure.NewCredentials()))
		if err != nil {
			fmt.Printf("Warning: Could not connect to lightwalletd: %v\n", err)
		} else {
			defer conn.Close()
			tdResult, err := BenchmarkTrialDecrypt(ctx, conn, cfg.StartHeight, cfg.EndHeight)
			if err != nil {
				fmt.Printf("Warning: Trial decryption benchmark failed: %v\n", err)
			} else {
				result.TrialDecrypt = tdResult
			}
		}
	}

	// Generate comparison if we have both
	if result.PirQueries != nil && result.TrialDecrypt != nil {
		result.Comparison = GenerateComparison(result.TrialDecrypt, result.PirQueries)
	}

	return result, nil
}

// BenchmarkPirQueries benchmarks actual PIR queries against the service.
func BenchmarkPirQueries(ctx context.Context, client *pirclient.Client, numQueries int) (*PirQueryResult, error) {
	result := &PirQueryResult{
		NumQueries: numQueries,
		QueryTimes: make([]float64, 0, numQueries),
	}

	// Get YPIR params first
	params, err := client.GetYpirParams(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get YPIR params: %w", err)
	}

	fmt.Printf("  Database has %d records\n", params.NumRecords)

	var totalQuerySize, totalResponseSize int64

	for i := 0; i < numQueries; i++ {
		// Generate a random query (in practice, this would be a real bucket index)
		// For benchmarking, we just need to measure the server processing time
		queryData := generateRandomQuery(params)

		queryBytes, _ := json.Marshal(&pirclient.YpirQueryRequest{
			PackedQueryRowB64: base64.StdEncoding.EncodeToString(queryData),
			PubParamsB64:      "", // Empty for benchmark - server will use defaults
		})
		totalQuerySize += int64(len(queryBytes))

		start := time.Now()
		resp, err := client.QueryYpir(ctx, &pirclient.YpirQueryRequest{
			PackedQueryRowB64: base64.StdEncoding.EncodeToString(queryData),
			PubParamsB64:      "",
		})
		elapsed := time.Since(start)

		if err != nil {
			fmt.Printf("  Query %d failed: %v\n", i+1, err)
			continue
		}

		queryTimeMs := float64(elapsed.Milliseconds())
		result.QueryTimes = append(result.QueryTimes, queryTimeMs)

		// Estimate response size
		for _, ct := range resp.CtsB64 {
			totalResponseSize += int64(len(ct))
		}

		fmt.Printf("  Query %d: %.0f ms (server reported: %.0f ms)\n", i+1, queryTimeMs, resp.ProcessingTimeMs)
	}

	if len(result.QueryTimes) == 0 {
		return nil, fmt.Errorf("all queries failed")
	}

	// Calculate statistics
	result.MinTimeMs = result.QueryTimes[0]
	result.MaxTimeMs = result.QueryTimes[0]
	var sum float64
	for _, t := range result.QueryTimes {
		sum += t
		if t < result.MinTimeMs {
			result.MinTimeMs = t
		}
		if t > result.MaxTimeMs {
			result.MaxTimeMs = t
		}
	}
	result.AvgTimeMs = sum / float64(len(result.QueryTimes))
	result.TotalTimeMs = sum
	result.MedianTimeMs = result.QueryTimes[len(result.QueryTimes)/2]
	result.AvgQuerySizeBytes = totalQuerySize / int64(len(result.QueryTimes))
	result.AvgResponseSizeBytes = totalResponseSize / int64(len(result.QueryTimes))

	return result, nil
}

// generateRandomQuery generates random query data for benchmarking.
func generateRandomQuery(params *pirclient.YpirParams) []byte {
	// Generate random bytes - the actual query construction would be more complex
	// but for benchmarking server processing time, random data works
	querySize := params.NumSuperItems * 8 // Rough estimate
	if querySize < 1024 {
		querySize = 1024
	}
	if querySize > 1024*1024 {
		querySize = 1024 * 1024
	}

	data := make([]byte, querySize)
	rand.Read(data)
	return data
}

// BenchmarkTrialDecrypt benchmarks downloading nullifiers via GetBlockRangeNullifiers.
func BenchmarkTrialDecrypt(ctx context.Context, conn *grpc.ClientConn, startHeight, endHeight uint64) (*TrialDecryptResult, error) {
	client := walletrpc.NewCompactTxStreamerClient(conn)
	result := &TrialDecryptResult{
		BlocksRequested: int(endHeight - startHeight + 1),
	}

	start := time.Now()

	stream, err := client.GetBlockRangeNullifiers(ctx, &walletrpc.BlockRange{
		Start: &walletrpc.BlockID{Height: startHeight},
		End:   &walletrpc.BlockID{Height: endHeight},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to start stream: %w", err)
	}

	for {
		block, err := stream.Recv()
		if err != nil {
			break
		}

		result.BlocksReceived++

		for _, tx := range block.Vtx {
			for _, action := range tx.Actions {
				result.NullifiersTotal++
				result.BandwidthBytes += int64(len(action.Nullifier))
			}
		}

		// Progress indicator
		if result.BlocksReceived%100 == 0 {
			fmt.Printf("  Processed %d blocks, %d nullifiers...\n", result.BlocksReceived, result.NullifiersTotal)
		}
	}

	result.DownloadTimeMs = float64(time.Since(start).Milliseconds())

	if result.BlocksReceived > 0 {
		result.BandwidthPerBlock = float64(result.BandwidthBytes) / float64(result.BlocksReceived)
		result.AvgNullifiersPerBlock = float64(result.NullifiersTotal) / float64(result.BlocksReceived)
	}

	return result, nil
}

// GenerateComparison generates a comparison between the two approaches.
func GenerateComparison(td *TrialDecryptResult, pir *PirQueryResult) *ComparisonResult {
	result := &ComparisonResult{
		TrialDecryptTimeMs:    td.DownloadTimeMs,
		TrialDecryptBandwidth: td.BandwidthBytes,
		PirTimePerQueryMs:     pir.AvgTimeMs,
		PirBandwidthPerQuery:  pir.AvgQuerySizeBytes + pir.AvgResponseSizeBytes,
		Notes:                 make([]string, 0),
	}

	// Break-even: when does trial decrypt become faster?
	// Trial decrypt: fixed cost to download all nullifiers
	// PIR: cost per query
	// Break-even when: td.DownloadTimeMs = N * pir.AvgTimeMs
	if pir.AvgTimeMs > 0 {
		result.BreakEvenQueries = int(td.DownloadTimeMs / pir.AvgTimeMs)
	}

	// Add analysis notes
	result.Notes = append(result.Notes,
		fmt.Sprintf("Trial decryption downloads %d nullifiers in %.1f seconds",
			td.NullifiersTotal, td.DownloadTimeMs/1000))

	result.Notes = append(result.Notes,
		fmt.Sprintf("Each PIR query takes %.1f seconds (must scan entire %d-nullifier database)",
			pir.AvgTimeMs/1000, td.NullifiersTotal))

	if result.BreakEvenQueries > 0 {
		result.Notes = append(result.Notes,
			fmt.Sprintf("Break-even point: %d queries", result.BreakEvenQueries))

		if result.BreakEvenQueries <= 1 {
			result.Notes = append(result.Notes,
				"PIR is slower than trial decryption even for a single query")
		} else {
			result.Notes = append(result.Notes,
				fmt.Sprintf("PIR is faster when checking fewer than %d nullifiers", result.BreakEvenQueries))
		}
	}

	// Privacy note
	result.Notes = append(result.Notes,
		"Note: PIR provides privacy (server learns nothing about which nullifier you're checking)")
	result.Notes = append(result.Notes,
		"Trial decryption reveals which blocks you're interested in to the server")

	return result
}

// PrintResults prints the benchmark results.
func PrintResults(result *BenchmarkResult, format string) {
	if format == "json" {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		enc.Encode(result)
		return
	}

	fmt.Println("")
	fmt.Println("════════════════════════════════════════════════════════════")
	fmt.Println("                    BENCHMARK RESULTS")
	fmt.Println("════════════════════════════════════════════════════════════")

	if result.PirStatus != nil {
		fmt.Println("\n▸ PIR SERVICE STATUS")
		fmt.Printf("  Status:          %s\n", result.PirStatus.Status)
		fmt.Printf("  Nullifiers:      %d\n", result.PirStatus.NumNullifiers)
		fmt.Printf("  Buckets:         %d\n", result.PirStatus.NumBuckets)
		fmt.Printf("  Database height: %d\n", result.PirStatus.PirDbHeight)
		fmt.Printf("  Est. DB size:    %s\n", result.PirStatus.EstimatedDbSize)
	}

	if result.PirQueries != nil {
		fmt.Println("\n▸ PIR QUERY PERFORMANCE")
		fmt.Printf("  Queries run:     %d\n", result.PirQueries.NumQueries)
		fmt.Printf("  Min time:        %.0f ms (%.1f s)\n", result.PirQueries.MinTimeMs, result.PirQueries.MinTimeMs/1000)
		fmt.Printf("  Max time:        %.0f ms (%.1f s)\n", result.PirQueries.MaxTimeMs, result.PirQueries.MaxTimeMs/1000)
		fmt.Printf("  Avg time:        %.0f ms (%.1f s)\n", result.PirQueries.AvgTimeMs, result.PirQueries.AvgTimeMs/1000)
		fmt.Printf("  Median time:     %.0f ms (%.1f s)\n", result.PirQueries.MedianTimeMs, result.PirQueries.MedianTimeMs/1000)
		fmt.Printf("  Query size:      %s\n", formatBytes(result.PirQueries.AvgQuerySizeBytes))
		fmt.Printf("  Response size:   %s\n", formatBytes(result.PirQueries.AvgResponseSizeBytes))
	}

	if result.TrialDecrypt != nil {
		fmt.Println("\n▸ TRIAL DECRYPTION PERFORMANCE")
		fmt.Printf("  Blocks:          %d (requested %d)\n", result.TrialDecrypt.BlocksReceived, result.TrialDecrypt.BlocksRequested)
		fmt.Printf("  Nullifiers:      %d (%.1f/block avg)\n", result.TrialDecrypt.NullifiersTotal, result.TrialDecrypt.AvgNullifiersPerBlock)
		fmt.Printf("  Download time:   %.0f ms (%.1f s)\n", result.TrialDecrypt.DownloadTimeMs, result.TrialDecrypt.DownloadTimeMs/1000)
		fmt.Printf("  Bandwidth:       %s (%.0f bytes/block)\n", formatBytes(result.TrialDecrypt.BandwidthBytes), result.TrialDecrypt.BandwidthPerBlock)

		if result.TrialDecrypt.DownloadTimeMs > 0 {
			throughput := float64(result.TrialDecrypt.NullifiersTotal) / (result.TrialDecrypt.DownloadTimeMs / 1000)
			fmt.Printf("  Throughput:      %.0f nullifiers/sec\n", throughput)
		}
	}

	if result.Comparison != nil {
		fmt.Println("\n────────────────────────────────────────────────────────────")
		fmt.Println("                      COMPARISON")
		fmt.Println("────────────────────────────────────────────────────────────")

		fmt.Println("\n  To check membership of ONE nullifier:")
		fmt.Printf("    Trial decrypt: %.1f s (download all %d nullifiers)\n",
			result.Comparison.TrialDecryptTimeMs/1000,
			result.TrialDecrypt.NullifiersTotal)
		fmt.Printf("    PIR query:     %.1f s (scan full database)\n",
			result.Comparison.PirTimePerQueryMs/1000)

		if result.Comparison.PirTimePerQueryMs < result.Comparison.TrialDecryptTimeMs {
			speedup := result.Comparison.TrialDecryptTimeMs / result.Comparison.PirTimePerQueryMs
			fmt.Printf("\n  ✓ PIR is %.1fx FASTER for single lookups\n", speedup)
		} else {
			slowdown := result.Comparison.PirTimePerQueryMs / result.Comparison.TrialDecryptTimeMs
			fmt.Printf("\n  ✗ PIR is %.1fx SLOWER for single lookups\n", slowdown)
		}

		fmt.Println("\n  Bandwidth per check:")
		fmt.Printf("    Trial decrypt: %s\n", formatBytes(result.Comparison.TrialDecryptBandwidth))
		fmt.Printf("    PIR query:     %s\n", formatBytes(result.Comparison.PirBandwidthPerQuery))

		if result.Comparison.BreakEvenQueries > 0 {
			fmt.Printf("\n  Break-even point: %d queries\n", result.Comparison.BreakEvenQueries)
		}

		fmt.Println("\n  Notes:")
		for _, note := range result.Comparison.Notes {
			fmt.Printf("    • %s\n", note)
		}
	}

	fmt.Println("\n════════════════════════════════════════════════════════════")
}

func formatBytes(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(bytes)/float64(div), "KMGTPE"[exp])
}

// Additional utility for generating test nullifiers
func GenerateTestNullifier() string {
	var nf [32]byte
	rand.Read(nf[:])
	return hex.EncodeToString(nf[:])
}
