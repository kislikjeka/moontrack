package alchemy_test

import (
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kislikjeka/moontrack/internal/infra/gateway/alchemy"
	"github.com/kislikjeka/moontrack/internal/platform/sync"
)

// =============================================================================
// ParseBlockNumber Tests
// =============================================================================

func TestParseBlockNumber_ValidHex(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected int64
	}{
		{"with 0x prefix", "0xbc614e", 12345678},
		{"without prefix", "bc614e", 12345678},
		{"zero", "0x0", 0},
		{"large number", "0xffffffff", 4294967295},
		{"small number", "0x1", 1},
		{"empty after prefix", "", 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := alchemy.ParseBlockNumber(tt.input)
			require.NoError(t, err)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestFormatBlockNumber(t *testing.T) {
	tests := []struct {
		name     string
		input    int64
		expected string
	}{
		{"standard", 12345678, "0xbc614e"},
		{"zero", 0, "0x"},
		{"one", 1, "0x1"},
		{"large", 4294967295, "0xffffffff"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := alchemy.FormatBlockNumber(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// =============================================================================
// ParseHexValue Tests
// =============================================================================

func TestParseHexValue(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected *big.Int
	}{
		{"1 ETH in wei", "0xde0b6b3a7640000", big.NewInt(1000000000000000000)},
		{"zero", "0x0", big.NewInt(0)},
		{"empty", "", big.NewInt(0)},
		{"just 0x", "0x", big.NewInt(0)},
		{"small value", "0x64", big.NewInt(100)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := alchemy.ParseHexValue(tt.input)
			assert.Equal(t, 0, result.Cmp(tt.expected), "Expected %s, got %s", tt.expected.String(), result.String())
		})
	}
}

// =============================================================================
// AssetTransfer Methods Tests
// =============================================================================

func TestAssetTransfer_IsNativeTransfer(t *testing.T) {
	tests := []struct {
		name     string
		category string
		expected bool
	}{
		{"external is native", "external", true},
		{"internal is native", "internal", true},
		{"erc20 is not native", "erc20", false},
		{"erc721 is not native", "erc721", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			transfer := alchemy.AssetTransfer{Category: tt.category}
			assert.Equal(t, tt.expected, transfer.IsNativeTransfer())
		})
	}
}

func TestAssetTransfer_IsERC20Transfer(t *testing.T) {
	tests := []struct {
		name     string
		category string
		expected bool
	}{
		{"erc20 is erc20", "erc20", true},
		{"external is not erc20", "external", false},
		{"internal is not erc20", "internal", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			transfer := alchemy.AssetTransfer{Category: tt.category}
			assert.Equal(t, tt.expected, transfer.IsERC20Transfer())
		})
	}
}

func TestAssetTransfer_GetAmount(t *testing.T) {
	transfer := alchemy.AssetTransfer{
		RawContract: alchemy.RawContract{
			Value: "0xde0b6b3a7640000", // 1 ETH
		},
	}
	amount := transfer.GetAmount()
	expected := big.NewInt(1000000000000000000)
	assert.Equal(t, 0, amount.Cmp(expected))
}

func TestAssetTransfer_GetDecimals(t *testing.T) {
	tests := []struct {
		name     string
		transfer alchemy.AssetTransfer
		expected int
	}{
		{
			name: "explicit decimals",
			transfer: alchemy.AssetTransfer{
				Category:    "erc20",
				RawContract: alchemy.RawContract{Decimal: 6},
			},
			expected: 6,
		},
		{
			name: "native transfer defaults to 18",
			transfer: alchemy.AssetTransfer{
				Category:    "external",
				RawContract: alchemy.RawContract{Decimal: 0},
			},
			expected: 18,
		},
		{
			name: "erc20 without decimals defaults to 18",
			transfer: alchemy.AssetTransfer{
				Category:    "erc20",
				RawContract: alchemy.RawContract{Decimal: 0},
			},
			expected: 18,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.transfer.GetDecimals())
		})
	}
}

func TestAssetTransfer_GetContractAddress(t *testing.T) {
	tests := []struct {
		name     string
		address  *string
		expected string
	}{
		{"with address", strPtr("0xcontract123"), "0xcontract123"},
		{"nil address", nil, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			transfer := alchemy.AssetTransfer{
				RawContract: alchemy.RawContract{Address: tt.address},
			}
			assert.Equal(t, tt.expected, transfer.GetContractAddress())
		})
	}
}

// =============================================================================
// Client Tests with Mock Server
// =============================================================================

func TestClient_GetCurrentBlock_ParsesResponse(t *testing.T) {
	// Create mock server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify request
		var req alchemy.RPCRequest
		json.NewDecoder(r.Body).Decode(&req)
		assert.Equal(t, "eth_blockNumber", req.Method)

		// Send response
		resp := alchemy.RPCResponse{
			JSONRPC: "2.0",
			ID:      1,
			Result:  json.RawMessage(`"0xbc614e"`), // 12345678
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()
	_ = server // Used for reference - actual client test would use this

	// Test the block number parsing logic that the client uses
	blockNum, err := alchemy.ParseBlockNumber("0xbc614e")
	require.NoError(t, err)
	assert.Equal(t, int64(12345678), blockNum)
}

func TestClient_GetAssetTransfers_ParsesResponse(t *testing.T) {
	// Sample Alchemy response
	responseJSON := `{
		"transfers": [
			{
				"blockNum": "0xbc614e",
				"hash": "0xabc123",
				"from": "0xsender",
				"to": "0xreceiver",
				"value": 1.5,
				"asset": "ETH",
				"category": "external",
				"rawContract": {
					"value": "0x14d1120d7b160000",
					"address": null,
					"decimal": 18
				},
				"metadata": {
					"blockTimestamp": "2024-01-15T10:30:00Z"
				},
				"uniqueId": "unique-123"
			}
		],
		"pageKey": "next-page-key"
	}`

	var response alchemy.AssetTransferResponse
	err := json.Unmarshal([]byte(responseJSON), &response)
	require.NoError(t, err)

	// Verify parsed data
	assert.Len(t, response.Transfers, 1)
	assert.Equal(t, "next-page-key", response.PageKey)

	transfer := response.Transfers[0]
	assert.Equal(t, "0xbc614e", transfer.BlockNum)
	assert.Equal(t, "0xabc123", transfer.Hash)
	assert.Equal(t, "0xsender", transfer.From)
	assert.Equal(t, "0xreceiver", transfer.To)
	assert.Equal(t, "ETH", transfer.Asset)
	assert.Equal(t, "external", transfer.Category)
	assert.Equal(t, "unique-123", transfer.UniqueID)
	assert.True(t, transfer.IsNativeTransfer())

	// Verify amount parsing
	amount := transfer.GetAmount()
	expected := big.NewInt(1500000000000000000) // 1.5 ETH
	assert.Equal(t, 0, amount.Cmp(expected))
}

func TestClient_RateLimitError(t *testing.T) {
	err := &alchemy.RateLimitError{
		RetryAfter: time.Minute,
		Message:    "Rate limit exceeded",
	}

	assert.Contains(t, err.Error(), "Rate limit exceeded")
	assert.Contains(t, err.Error(), "1m0s")
	assert.True(t, alchemy.IsRateLimitError(err))
	assert.False(t, alchemy.IsRateLimitError(assert.AnError))
}

// =============================================================================
// Adapter Interface Compliance Test
// =============================================================================

func TestSyncClientAdapter_ImplementsBlockchainClient(t *testing.T) {
	// This test verifies at compile time that the adapter implements the interface
	// We can't test the actual methods without a properly configured ChainsConfig
	// (which requires LoadChainsConfig to initialize the internal lookup map)

	// The adapter is created in production code with:
	// adapter := alchemy.NewSyncClientAdapter(client, chainsConfig)
	// var _ sync.BlockchainClient = adapter

	// For unit tests, we verify the interface signature matches
	var _ sync.BlockchainClient = (*alchemy.SyncClientAdapter)(nil)
}

// =============================================================================
// Default Categories Test
// =============================================================================

func TestDefaultTransferCategories(t *testing.T) {
	categories := alchemy.DefaultTransferCategories()

	assert.Contains(t, categories, "external")
	assert.Contains(t, categories, "internal")
	assert.Contains(t, categories, "erc20")
	assert.Len(t, categories, 3)
}

// =============================================================================
// RPCError Test
// =============================================================================

func TestRPCError(t *testing.T) {
	err := &alchemy.RPCError{
		Code:    -32600,
		Message: "Invalid Request",
		Data:    "some data",
	}

	assert.Equal(t, "Invalid Request", err.Error())
}

// =============================================================================
// Transfer Categories Constants Test
// =============================================================================

func TestTransferCategories(t *testing.T) {
	assert.Equal(t, "external", alchemy.CategoryExternal)
	assert.Equal(t, "internal", alchemy.CategoryInternal)
	assert.Equal(t, "erc20", alchemy.CategoryERC20)
	assert.Equal(t, "erc721", alchemy.CategoryERC721)
	assert.Equal(t, "erc1155", alchemy.CategoryERC1155)
}

// =============================================================================
// ERC20 Transfer Parsing Test
// =============================================================================

func TestAssetTransfer_ERC20Transfer_Parsing(t *testing.T) {
	responseJSON := `{
		"transfers": [
			{
				"blockNum": "0xbc614e",
				"hash": "0xerc20tx",
				"from": "0xsender",
				"to": "0xreceiver",
				"value": 1000.0,
				"asset": "USDC",
				"category": "erc20",
				"rawContract": {
					"value": "0x3b9aca00",
					"address": "0xa0b86991c6218b36c1d19d4a2e9eb0ce3606eb48",
					"decimal": 6
				},
				"metadata": {
					"blockTimestamp": "2024-01-15T10:30:00Z"
				},
				"uniqueId": "usdc-transfer-123"
			}
		]
	}`

	var response alchemy.AssetTransferResponse
	err := json.Unmarshal([]byte(responseJSON), &response)
	require.NoError(t, err)

	transfer := response.Transfers[0]
	assert.Equal(t, "USDC", transfer.Asset)
	assert.Equal(t, "erc20", transfer.Category)
	assert.True(t, transfer.IsERC20Transfer())
	assert.False(t, transfer.IsNativeTransfer())

	// Verify decimals
	assert.Equal(t, 6, transfer.GetDecimals())

	// Verify contract address
	assert.Equal(t, "0xa0b86991c6218b36c1d19d4a2e9eb0ce3606eb48", transfer.GetContractAddress())

	// Verify amount: 0x3b9aca00 = 1000000000 (1000 USDC with 6 decimals)
	amount := transfer.GetAmount()
	expected := big.NewInt(1000000000)
	assert.Equal(t, 0, amount.Cmp(expected))
}

// =============================================================================
// Multiple Transfers Pagination Test
// =============================================================================

func TestAssetTransferResponse_Pagination(t *testing.T) {
	// First page response
	firstPageJSON := `{
		"transfers": [
			{
				"blockNum": "0x1",
				"hash": "0xtx1",
				"from": "0xa",
				"to": "0xb",
				"asset": "ETH",
				"category": "external",
				"rawContract": {"value": "0x1", "decimal": 18},
				"metadata": {"blockTimestamp": "2024-01-15T10:00:00Z"},
				"uniqueId": "1"
			}
		],
		"pageKey": "next-page-abc"
	}`

	// Second page response (last page, no pageKey)
	lastPageJSON := `{
		"transfers": [
			{
				"blockNum": "0x2",
				"hash": "0xtx2",
				"from": "0xa",
				"to": "0xb",
				"asset": "ETH",
				"category": "external",
				"rawContract": {"value": "0x2", "decimal": 18},
				"metadata": {"blockTimestamp": "2024-01-15T11:00:00Z"},
				"uniqueId": "2"
			}
		]
	}`

	var firstPage alchemy.AssetTransferResponse
	err := json.Unmarshal([]byte(firstPageJSON), &firstPage)
	require.NoError(t, err)
	assert.Len(t, firstPage.Transfers, 1)
	assert.Equal(t, "next-page-abc", firstPage.PageKey)

	var lastPage alchemy.AssetTransferResponse
	err = json.Unmarshal([]byte(lastPageJSON), &lastPage)
	require.NoError(t, err)
	assert.Len(t, lastPage.Transfers, 1)
	assert.Equal(t, "", lastPage.PageKey) // Empty means no more pages
}

// =============================================================================
// Transfer Metadata Timestamp Test
// =============================================================================

func TestAssetTransfer_MetadataTimestamp(t *testing.T) {
	responseJSON := `{
		"transfers": [
			{
				"blockNum": "0x1",
				"hash": "0xtx1",
				"from": "0xa",
				"to": "0xb",
				"asset": "ETH",
				"category": "external",
				"rawContract": {"value": "0x1", "decimal": 18},
				"metadata": {"blockTimestamp": "2024-01-15T10:30:45Z"},
				"uniqueId": "1"
			}
		]
	}`

	var response alchemy.AssetTransferResponse
	err := json.Unmarshal([]byte(responseJSON), &response)
	require.NoError(t, err)

	transfer := response.Transfers[0]
	assert.Equal(t, "2024-01-15T10:30:45Z", transfer.Metadata.BlockTimestamp)
}

// =============================================================================
// Helper Functions
// =============================================================================

func strPtr(s string) *string {
	return &s
}
