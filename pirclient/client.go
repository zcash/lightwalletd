// Copyright (c) 2019-2020 The Zcash developers
// Distributed under the MIT software license, see the accompanying
// file COPYING or https://www.opensource.org/licenses/mit-license.php .

// Package pirclient provides an HTTP client for communicating with the nullifier-pir service.
package pirclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Client is an HTTP client for the nullifier-pir service.
type Client struct {
	baseURL    string
	httpClient *http.Client
	timeout    time.Duration
}

// NewClient creates a new PIR client.
func NewClient(baseURL string, timeout time.Duration) *Client {
	return &Client{
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: timeout,
		},
		timeout: timeout,
	}
}

// IngestNullifierEntry represents a single nullifier to be ingested.
type IngestNullifierEntry struct {
	Nullifier string `json:"nullifier"` // Hex-encoded 32-byte nullifier
	TxIndex   uint16 `json:"tx_index"`  // Transaction index within block
}

// IngestRequest is the request to ingest nullifiers for a block.
type IngestRequest struct {
	BlockHeight uint64                 `json:"block_height"`
	BlockHash   string                 `json:"block_hash"` // Hex-encoded 32-byte block hash
	Nullifiers  []IngestNullifierEntry `json:"nullifiers"`
}

// IngestResponse is the response from ingesting nullifiers.
type IngestResponse struct {
	Status          string `json:"status"`
	QueuedBlocks    int    `json:"queued_blocks"`
	CurrentPirHeight uint64 `json:"current_pir_height"`
}

// ReorgRequest is the request to handle a chain reorganization.
type ReorgRequest struct {
	ReorgHeight uint64 `json:"reorg_height"` // Keep blocks <= this height
}

// ReorgResponse is the response from a reorg request.
type ReorgResponse struct {
	Status        string `json:"status"`
	BlocksRemoved int    `json:"blocks_removed"`
	NewHeight     uint64 `json:"new_height"`
}

// PirStatus contains the current status of the PIR service.
type PirStatus struct {
	Status            string  `json:"status"`
	PirDbHeight       uint64  `json:"pir_db_height"`
	PendingBlocks     int     `json:"pending_blocks"`
	NumNullifiers     int     `json:"num_nullifiers"`
	NumBuckets        int     `json:"num_buckets"`
	RebuildInProgress bool    `json:"rebuild_in_progress"`
	BuildTimestamp    *string `json:"build_timestamp,omitempty"`
}

// CuckooParams contains the Cuckoo hash table parameters.
type CuckooParams struct {
	Seed       string `json:"seed"` // String to preserve u64 precision
	NumBuckets int    `json:"num_buckets"`
	ValueSize  int    `json:"value_size"`
	EntrySize  int    `json:"entry_size"`
	BucketSize int    `json:"bucket_size"`
}

// YpirParams contains the YPIR PIR parameters.
type YpirParams struct {
	Protocol            string       `json:"protocol"`
	NumRecords          int          `json:"num_records"`
	RecordSize          int          `json:"record_size"`
	SuperItemBits       int          `json:"super_item_bits"`
	SuperItemBytes      int          `json:"super_item_bytes"`
	NumSuperItems       int          `json:"num_super_items"`
	RecordsPerSuperItem int          `json:"records_per_super_item"`
	Instances           int          `json:"instances"`
	CuckooParams        CuckooParams `json:"cuckoo_params"`
	PirDbHeight         uint64       `json:"pir_db_height"`
}

// InspireSetup contains the InsPIRe setup parameters.
type InspireSetup struct {
	PolyLen           int    `json:"poly_len"`
	DbDim1            int    `json:"db_dim_1"`
	Instances         int    `json:"instances"`
	DbRows            int    `json:"db_rows"`
	DbCols            int    `json:"db_cols"`
	Gamma             int    `json:"gamma"`
	InterpolateDegree int    `json:"interpolate_degree"`
	PtModulus         uint64 `json:"pt_modulus"`
	C                 int    `json:"c"`
	TGsw              int    `json:"t_gsw"`
	Q2Bits            int    `json:"q2_bits"`
	TExpLeft          int    `json:"t_exp_left"`
}

// InspireParams contains the InsPIRe PIR parameters.
type InspireParams struct {
	Protocol     string       `json:"protocol"`
	PirSetup     InspireSetup `json:"pir_setup"`
	CuckooParams CuckooParams `json:"cuckoo_params"`
	RecordSize   int          `json:"record_size"`
	Factor       int          `json:"factor"`
	PirDbHeight  uint64       `json:"pir_db_height"`
}

// YpirQueryRequest is the request to perform a YPIR query.
type YpirQueryRequest struct {
	PackedQueryRowB64 string `json:"packed_query_row_b64"`
	PubParamsB64      string `json:"pub_params_b64"`
}

// YpirQueryResponse is the response from a YPIR query.
type YpirQueryResponse struct {
	CtsB64           []string `json:"cts_b64"`
	ProcessingTimeMs float64  `json:"processing_time_ms"`
}

// InspirePackingKeys contains the InsPIRe packing keys.
type InspirePackingKeys struct {
	YBodyData []uint64 `json:"y_body_data"`
	ZBodyData []uint64 `json:"z_body_data,omitempty"`
	FullKey   bool     `json:"full_key"`
	TExpLeft  int      `json:"t_exp_left"`
	PolyLen   int      `json:"poly_len"`
}

// InspireQueryRequest is the request to perform an InsPIRe query.
type InspireQueryRequest struct {
	PackedQueryRow  []uint64           `json:"packed_query_row"`
	CtGswBodyData   []uint64           `json:"ct_gsw_body_data"`
	PackingKeys     InspirePackingKeys `json:"packing_keys"`
}

// InspireQueryResponse is the response from an InsPIRe query.
type InspireQueryResponse struct {
	PackedResponse [][]byte `json:"packed_response"`
}

// BinaryInspireQueryResponse is the response from a binary InsPIRe query.
type BinaryInspireQueryResponse struct {
	PackedResponseB64 []string `json:"packed_response_b64"`
	ProcessingTimeMs  float64  `json:"processing_time_ms"`
}

// IsEnabled returns true if the client is configured with a service URL.
func (c *Client) IsEnabled() bool {
	return c != nil && c.baseURL != ""
}

// GetStatus retrieves the current PIR service status.
func (c *Client) GetStatus(ctx context.Context) (*PirStatus, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", c.baseURL+"/pir/status", nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("sending request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("unexpected status %d: %s", resp.StatusCode, string(body))
	}

	var status PirStatus
	if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}

	return &status, nil
}

// WaitForReady waits for the PIR service to be ready, with a timeout.
func (c *Client) WaitForReady(ctx context.Context, pollInterval time.Duration) error {
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			status, err := c.GetStatus(ctx)
			if err != nil {
				// Log but continue waiting
				continue
			}
			if status.Status == "ready" {
				return nil
			}
		}
	}
}

// IngestNullifiers sends a batch of nullifiers to the PIR service.
func (c *Client) IngestNullifiers(ctx context.Context, req *IngestRequest) (*IngestResponse, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshaling request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/nullifiers/ingest", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("sending request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("unexpected status %d: %s", resp.StatusCode, string(respBody))
	}

	var ingestResp IngestResponse
	if err := json.NewDecoder(resp.Body).Decode(&ingestResp); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}

	return &ingestResp, nil
}

// HandleReorg notifies the PIR service of a chain reorganization.
func (c *Client) HandleReorg(ctx context.Context, reorgHeight uint64) (*ReorgResponse, error) {
	req := ReorgRequest{ReorgHeight: reorgHeight}
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshaling request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/nullifiers/reorg", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("sending request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("unexpected status %d: %s", resp.StatusCode, string(respBody))
	}

	var reorgResp ReorgResponse
	if err := json.NewDecoder(resp.Body).Decode(&reorgResp); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}

	return &reorgResp, nil
}

// GetYpirParams retrieves the YPIR parameters from the PIR service.
func (c *Client) GetYpirParams(ctx context.Context) (*YpirParams, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", c.baseURL+"/pir/params/ypir", nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("sending request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("unexpected status %d: %s", resp.StatusCode, string(body))
	}

	var params YpirParams
	if err := json.NewDecoder(resp.Body).Decode(&params); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}

	return &params, nil
}

// GetInspireParams retrieves the InsPIRe parameters from the PIR service.
func (c *Client) GetInspireParams(ctx context.Context) (*InspireParams, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", c.baseURL+"/pir/params/inspire", nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("sending request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("unexpected status %d: %s", resp.StatusCode, string(body))
	}

	var params InspireParams
	if err := json.NewDecoder(resp.Body).Decode(&params); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}

	return &params, nil
}

// QueryYpir performs a YPIR query against the PIR service.
func (c *Client) QueryYpir(ctx context.Context, req *YpirQueryRequest) (*YpirQueryResponse, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshaling request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/pir/query/ypir", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("sending request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("unexpected status %d: %s", resp.StatusCode, string(respBody))
	}

	var queryResp YpirQueryResponse
	if err := json.NewDecoder(resp.Body).Decode(&queryResp); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}

	return &queryResp, nil
}

// QueryInspire performs an InsPIRe query against the PIR service.
func (c *Client) QueryInspire(ctx context.Context, req *InspireQueryRequest) (*InspireQueryResponse, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshaling request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/pir/query/inspire", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("sending request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("unexpected status %d: %s", resp.StatusCode, string(respBody))
	}

	var queryResp InspireQueryResponse
	if err := json.NewDecoder(resp.Body).Decode(&queryResp); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}

	return &queryResp, nil
}

// QueryInspireBinary performs a binary-encoded InsPIRe query against the PIR service.
func (c *Client) QueryInspireBinary(ctx context.Context, queryData []byte) (*BinaryInspireQueryResponse, error) {
	httpReq, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/pir/query/binary", bytes.NewReader(queryData))
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/octet-stream")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("sending request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("unexpected status %d: %s", resp.StatusCode, string(respBody))
	}

	var queryResp BinaryInspireQueryResponse
	if err := json.NewDecoder(resp.Body).Decode(&queryResp); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}

	return &queryResp, nil
}
