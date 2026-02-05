package alchemy

import (
	"context"
	"math/big"
	"strings"
	"time"

	"github.com/kislikjeka/moontrack/internal/platform/sync"
	"github.com/kislikjeka/moontrack/pkg/config"
)

// SyncClientAdapter adapts Alchemy client to sync.BlockchainClient interface
type SyncClientAdapter struct {
	client       *Client
	chainsConfig *config.ChainsConfig
}

// NewSyncClientAdapter creates a new adapter
func NewSyncClientAdapter(client *Client, chainsConfig *config.ChainsConfig) *SyncClientAdapter {
	return &SyncClientAdapter{
		client:       client,
		chainsConfig: chainsConfig,
	}
}

// Ensure adapter implements the interface
var _ sync.BlockchainClient = (*SyncClientAdapter)(nil)

// GetCurrentBlock returns the current block number for a chain
func (a *SyncClientAdapter) GetCurrentBlock(ctx context.Context, chainID int64) (int64, error) {
	return a.client.GetCurrentBlock(ctx, chainID)
}

// GetTransfers retrieves all transfers for an address within a block range
func (a *SyncClientAdapter) GetTransfers(ctx context.Context, chainID int64, address string, fromBlock, toBlock int64) ([]sync.Transfer, error) {
	// Get incoming transfers
	incoming, err := a.client.GetIncomingTransfers(ctx, chainID, address, fromBlock, toBlock)
	if err != nil {
		return nil, err
	}

	// Get outgoing transfers
	outgoing, err := a.client.GetOutgoingTransfers(ctx, chainID, address, fromBlock, toBlock)
	if err != nil {
		return nil, err
	}

	// Combine and convert to sync.Transfer
	allTransfers := make([]sync.Transfer, 0, len(incoming)+len(outgoing))

	for _, t := range incoming {
		transfer, err := a.convertTransfer(chainID, address, t, sync.DirectionIn)
		if err != nil {
			continue // Skip invalid transfers
		}
		allTransfers = append(allTransfers, transfer)
	}

	for _, t := range outgoing {
		transfer, err := a.convertTransfer(chainID, address, t, sync.DirectionOut)
		if err != nil {
			continue // Skip invalid transfers
		}
		allTransfers = append(allTransfers, transfer)
	}

	return allTransfers, nil
}

// convertTransfer converts an Alchemy transfer to a sync.Transfer
func (a *SyncClientAdapter) convertTransfer(chainID int64, walletAddress string, t AssetTransfer, direction sync.TransferDirection) (sync.Transfer, error) {
	// Parse block number
	blockNum, err := ParseBlockNumber(t.BlockNum)
	if err != nil {
		return sync.Transfer{}, err
	}

	// Parse timestamp
	timestamp, err := time.Parse(time.RFC3339, t.Metadata.BlockTimestamp)
	if err != nil {
		// Try alternative format
		timestamp = time.Now()
	}

	// Get amount in base units
	amount := t.GetAmount()
	if amount == nil {
		amount = big.NewInt(0)
	}

	// Determine asset info
	assetSymbol := strings.ToUpper(t.Asset)
	contractAddress := t.GetContractAddress()
	decimals := t.GetDecimals()

	// For native transfers, use chain's native asset
	if t.IsNativeTransfer() && assetSymbol == "" {
		if chain, ok := a.chainsConfig.GetChain(chainID); ok {
			assetSymbol = chain.NativeAsset
			decimals = chain.NativeDecimals
		}
	}

	// Determine transfer type
	transferType := a.classifyTransferType(t.Category)

	return sync.Transfer{
		TxHash:          t.Hash,
		BlockNumber:     blockNum,
		Timestamp:       timestamp,
		From:            strings.ToLower(t.From),
		To:              strings.ToLower(t.To),
		Amount:          amount,
		AssetSymbol:     assetSymbol,
		ContractAddress: strings.ToLower(contractAddress),
		Decimals:        decimals,
		ChainID:         chainID,
		Direction:       direction,
		TransferType:    transferType,
		UniqueID:        t.UniqueID,
	}, nil
}

// classifyTransferType converts Alchemy category to sync transfer type
func (a *SyncClientAdapter) classifyTransferType(category string) sync.TransferType {
	switch category {
	case CategoryExternal:
		return sync.TransferTypeExternal
	case CategoryInternal:
		return sync.TransferTypeInternal
	case CategoryERC20:
		return sync.TransferTypeERC20
	default:
		return sync.TransferTypeExternal
	}
}

// GetNativeAssetInfo returns native asset info for a chain
func (a *SyncClientAdapter) GetNativeAssetInfo(chainID int64) (string, int, error) {
	chain, ok := a.chainsConfig.GetChain(chainID)
	if !ok {
		return "", 0, ErrUnsupportedChain
	}
	return chain.NativeAsset, chain.NativeDecimals, nil
}

// IsSupported checks if a chain is supported
func (a *SyncClientAdapter) IsSupported(chainID int64) bool {
	return a.chainsConfig.IsSupported(chainID)
}

// ErrUnsupportedChain is returned when a chain is not supported
var ErrUnsupportedChain = &UnsupportedChainError{}

// UnsupportedChainError represents an unsupported chain error
type UnsupportedChainError struct{}

func (e *UnsupportedChainError) Error() string {
	return "unsupported chain"
}
