package alchemy

import (
	"encoding/json"
	"math/big"
	"strings"
)

// JSON-RPC request/response structures

// RPCRequest represents a JSON-RPC 2.0 request
type RPCRequest struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      int         `json:"id"`
	Method  string      `json:"method"`
	Params  interface{} `json:"params"`
}

// RPCResponse represents a JSON-RPC 2.0 response
type RPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int             `json:"id"`
	Result  json.RawMessage `json:"result"`
	Error   *RPCError       `json:"error,omitempty"`
}

// RPCError represents a JSON-RPC error
type RPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    string `json:"data,omitempty"`
}

func (e *RPCError) Error() string {
	return e.Message
}

// alchemy_getAssetTransfers types

// AssetTransferParams represents parameters for alchemy_getAssetTransfers
type AssetTransferParams struct {
	FromBlock         string   `json:"fromBlock"`
	ToBlock           string   `json:"toBlock"`
	FromAddress       string   `json:"fromAddress,omitempty"`
	ToAddress         string   `json:"toAddress,omitempty"`
	ContractAddresses []string `json:"contractAddresses,omitempty"`
	Category          []string `json:"category"`
	WithMetadata      bool     `json:"withMetadata"`
	ExcludeZeroValue  bool     `json:"excludeZeroValue"`
	MaxCount          string   `json:"maxCount,omitempty"`
	PageKey           string   `json:"pageKey,omitempty"`
	Order             string   `json:"order,omitempty"` // "asc" or "desc"
}

// AssetTransferResponse represents the response from alchemy_getAssetTransfers
type AssetTransferResponse struct {
	Transfers []AssetTransfer `json:"transfers"`
	PageKey   string          `json:"pageKey,omitempty"`
}

// AssetTransfer represents a single asset transfer
type AssetTransfer struct {
	BlockNum        string           `json:"blockNum"`        // Hex block number
	Hash            string           `json:"hash"`            // Transaction hash
	From            string           `json:"from"`            // From address
	To              string           `json:"to"`              // To address
	Value           float64          `json:"value"`           // Transfer value (can be null for some NFTs)
	ERC721TokenID   *string          `json:"erc721TokenId"`   // NFT token ID
	ERC1155Metadata interface{}      `json:"erc1155Metadata"` // ERC1155 metadata
	TokenID         *string          `json:"tokenId"`         // Token ID
	Asset           string           `json:"asset"`           // Asset symbol (ETH, USDC, etc.)
	Category        string           `json:"category"`        // "external", "internal", "erc20", "erc721", "erc1155"
	RawContract     RawContract      `json:"rawContract"`     // Contract details
	Metadata        TransferMetadata `json:"metadata"`        // Block metadata
	UniqueID        string           `json:"uniqueId"`        // Unique transfer identifier
}

// RawContract contains contract information
type RawContract struct {
	Value   string  `json:"value"`   // Hex value in base units
	Address *string `json:"address"` // Contract address (null for native)
	Decimal int     `json:"decimal"` // Token decimals
}

// TransferMetadata contains block metadata
type TransferMetadata struct {
	BlockTimestamp string `json:"blockTimestamp"` // ISO 8601 timestamp
}

// eth_blockNumber response

// BlockNumberResponse represents the response from eth_blockNumber
type BlockNumberResponse string // Hex string

// ParseBlockNumber converts hex block number string to int64
func ParseBlockNumber(hexStr string) (int64, error) {
	// Remove 0x prefix
	hexStr = strings.TrimPrefix(hexStr, "0x")
	if hexStr == "" {
		return 0, nil
	}

	num := new(big.Int)
	num.SetString(hexStr, 16)
	return num.Int64(), nil
}

// FormatBlockNumber converts int64 to hex block number string
func FormatBlockNumber(blockNum int64) string {
	return "0x" + strings.TrimLeft(new(big.Int).SetInt64(blockNum).Text(16), "0")
}

// Transfer categories for Alchemy API
const (
	CategoryExternal  = "external"  // Native token transfers (ETH, MATIC, etc.)
	CategoryInternal  = "internal"  // Internal transactions (contract calls)
	CategoryERC20     = "erc20"     // ERC-20 token transfers
	CategoryERC721    = "erc721"    // ERC-721 NFT transfers
	CategoryERC1155   = "erc1155"   // ERC-1155 multi-token transfers
	CategorySpecialNFT = "specialnft" // Special NFT transfers
)

// DefaultTransferCategories returns the default categories for asset transfers
func DefaultTransferCategories() []string {
	return []string{
		CategoryExternal,
		CategoryInternal,
		CategoryERC20,
	}
}

// chainsWithInternalSupport lists chain IDs where Alchemy supports the "internal" category.
// Per Alchemy docs, only Ethereum and Polygon support internal transfers.
var chainsWithInternalSupport = map[int64]bool{
	1:   true, // Ethereum Mainnet
	137: true, // Polygon
}

// TransferCategoriesForChain returns supported transfer categories for a given chain.
// The "internal" category is only available on Ethereum and Polygon.
func TransferCategoriesForChain(chainID int64) []string {
	if chainsWithInternalSupport[chainID] {
		return DefaultTransferCategories()
	}
	return []string{
		CategoryExternal,
		CategoryERC20,
	}
}

// ParseHexValue converts a hex value string to *big.Int
func ParseHexValue(hexStr string) *big.Int {
	if hexStr == "" || hexStr == "0x" || hexStr == "0x0" {
		return big.NewInt(0)
	}

	// Remove 0x prefix
	hexStr = strings.TrimPrefix(hexStr, "0x")
	if hexStr == "" {
		return big.NewInt(0)
	}

	num := new(big.Int)
	num.SetString(hexStr, 16)
	return num
}

// IsNativeTransfer checks if the transfer is a native token transfer
func (t *AssetTransfer) IsNativeTransfer() bool {
	return t.Category == CategoryExternal || t.Category == CategoryInternal
}

// IsERC20Transfer checks if the transfer is an ERC-20 token transfer
func (t *AssetTransfer) IsERC20Transfer() bool {
	return t.Category == CategoryERC20
}

// GetContractAddress returns the contract address for ERC-20 transfers
func (t *AssetTransfer) GetContractAddress() string {
	if t.RawContract.Address != nil {
		return *t.RawContract.Address
	}
	return ""
}

// GetAmount returns the transfer amount as *big.Int in base units
func (t *AssetTransfer) GetAmount() *big.Int {
	return ParseHexValue(t.RawContract.Value)
}

// GetDecimals returns the token decimals
func (t *AssetTransfer) GetDecimals() int {
	if t.RawContract.Decimal > 0 {
		return t.RawContract.Decimal
	}
	// Default to 18 for native tokens
	if t.IsNativeTransfer() {
		return 18
	}
	return 18
}
