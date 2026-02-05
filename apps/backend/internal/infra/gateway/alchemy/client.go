package alchemy

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/kislikjeka/moontrack/pkg/config"
)

const (
	requestTimeout      = 30 * time.Second
	maxTransfersPerPage = "0x3e8" // 1000 in hex
)

// Client represents an Alchemy API client
type Client struct {
	apiKey       string
	httpClient   *http.Client
	chainsConfig *config.ChainsConfig
}

// NewClient creates a new Alchemy API client
func NewClient(apiKey string, chainsConfig *config.ChainsConfig) *Client {
	return &Client{
		apiKey: apiKey,
		httpClient: &http.Client{
			Timeout: requestTimeout,
		},
		chainsConfig: chainsConfig,
	}
}

// getBaseURL returns the Alchemy JSON-RPC endpoint for a chain
func (c *Client) getBaseURL(chainID int64) (string, error) {
	network, ok := c.chainsConfig.GetAlchemyNetwork(chainID)
	if !ok {
		return "", fmt.Errorf("unsupported chain ID: %d", chainID)
	}
	return fmt.Sprintf("https://%s.g.alchemy.com/v2/%s", network, c.apiKey), nil
}

// doRequest performs a JSON-RPC request
func (c *Client) doRequest(ctx context.Context, url string, req *RPCRequest) (*RPCResponse, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to execute request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusTooManyRequests {
		return nil, &RateLimitError{
			RetryAfter: time.Minute,
			Message:    "Alchemy API rate limit exceeded",
		}
	}

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("unexpected status code %d: %s", resp.StatusCode, string(respBody))
	}

	var rpcResp RPCResponse
	if err := json.NewDecoder(resp.Body).Decode(&rpcResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	if rpcResp.Error != nil {
		return nil, rpcResp.Error
	}

	return &rpcResp, nil
}

// GetCurrentBlock gets the current block number for a chain
func (c *Client) GetCurrentBlock(ctx context.Context, chainID int64) (int64, error) {
	url, err := c.getBaseURL(chainID)
	if err != nil {
		return 0, err
	}

	req := &RPCRequest{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "eth_blockNumber",
		Params:  []interface{}{},
	}

	resp, err := c.doRequest(ctx, url, req)
	if err != nil {
		return 0, fmt.Errorf("eth_blockNumber failed: %w", err)
	}

	var blockHex string
	if err := json.Unmarshal(resp.Result, &blockHex); err != nil {
		return 0, fmt.Errorf("failed to parse block number: %w", err)
	}

	return ParseBlockNumber(blockHex)
}

// GetAssetTransfersParams defines parameters for GetAssetTransfers
type GetAssetTransfersParams struct {
	ChainID    int64
	Address    string   // The wallet address
	FromBlock  int64    // Start block (0 for genesis)
	ToBlock    int64    // End block (0 for latest)
	Direction  string   // "from" or "to" (who initiated)
	Categories []string // Transfer categories to include
	PageKey    string   // Pagination key
}

// GetAssetTransfers retrieves asset transfers for an address
func (c *Client) GetAssetTransfers(ctx context.Context, params GetAssetTransfersParams) (*AssetTransferResponse, error) {
	url, err := c.getBaseURL(params.ChainID)
	if err != nil {
		return nil, err
	}

	// Build transfer params
	transferParams := AssetTransferParams{
		Category:         params.Categories,
		WithMetadata:     true,
		ExcludeZeroValue: true,
		MaxCount:         maxTransfersPerPage,
		Order:            "asc",
	}

	// Set block range
	if params.FromBlock > 0 {
		transferParams.FromBlock = FormatBlockNumber(params.FromBlock)
	} else {
		transferParams.FromBlock = "0x0"
	}

	if params.ToBlock > 0 {
		transferParams.ToBlock = FormatBlockNumber(params.ToBlock)
	} else {
		transferParams.ToBlock = "latest"
	}

	// Set address direction
	if params.Direction == "from" {
		transferParams.FromAddress = params.Address
	} else {
		transferParams.ToAddress = params.Address
	}

	// Set pagination
	if params.PageKey != "" {
		transferParams.PageKey = params.PageKey
	}

	// Default categories if not specified
	if len(transferParams.Category) == 0 {
		transferParams.Category = DefaultTransferCategories()
	}

	req := &RPCRequest{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "alchemy_getAssetTransfers",
		Params:  []interface{}{transferParams},
	}

	resp, err := c.doRequest(ctx, url, req)
	if err != nil {
		return nil, fmt.Errorf("alchemy_getAssetTransfers failed: %w", err)
	}

	var result AssetTransferResponse
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		return nil, fmt.Errorf("failed to parse transfers: %w", err)
	}

	return &result, nil
}

// GetAllAssetTransfers retrieves all asset transfers for an address with pagination
func (c *Client) GetAllAssetTransfers(ctx context.Context, params GetAssetTransfersParams) ([]AssetTransfer, error) {
	var allTransfers []AssetTransfer
	pageKey := params.PageKey

	for {
		params.PageKey = pageKey
		resp, err := c.GetAssetTransfers(ctx, params)
		if err != nil {
			return nil, err
		}

		allTransfers = append(allTransfers, resp.Transfers...)

		// Check if there are more pages
		if resp.PageKey == "" {
			break
		}
		pageKey = resp.PageKey
	}

	return allTransfers, nil
}

// GetIncomingTransfers retrieves all incoming transfers for an address
func (c *Client) GetIncomingTransfers(ctx context.Context, chainID int64, address string, fromBlock, toBlock int64) ([]AssetTransfer, error) {
	return c.GetAllAssetTransfers(ctx, GetAssetTransfersParams{
		ChainID:    chainID,
		Address:    address,
		FromBlock:  fromBlock,
		ToBlock:    toBlock,
		Direction:  "to",
		Categories: DefaultTransferCategories(),
	})
}

// GetOutgoingTransfers retrieves all outgoing transfers for an address
func (c *Client) GetOutgoingTransfers(ctx context.Context, chainID int64, address string, fromBlock, toBlock int64) ([]AssetTransfer, error) {
	return c.GetAllAssetTransfers(ctx, GetAssetTransfersParams{
		ChainID:    chainID,
		Address:    address,
		FromBlock:  fromBlock,
		ToBlock:    toBlock,
		Direction:  "from",
		Categories: DefaultTransferCategories(),
	})
}

// RateLimitError represents a rate limit error from Alchemy API
type RateLimitError struct {
	RetryAfter time.Duration
	Message    string
}

func (e *RateLimitError) Error() string {
	return fmt.Sprintf("%s (retry after %s)", e.Message, e.RetryAfter)
}

// IsRateLimitError checks if an error is a rate limit error
func IsRateLimitError(err error) bool {
	_, ok := err.(*RateLimitError)
	return ok
}
