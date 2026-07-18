package zerion

import (
	"context"
	"fmt"
	"math"
	"math/big"
	"strings"
	"time"

	"github.com/kislikjeka/moontrack/internal/platform/sync"
	"github.com/kislikjeka/moontrack/internal/platform/wallet"
)

// SyncAdapter adapts the Zerion client to the sync.TransactionDataProvider interface
type SyncAdapter struct {
	client *Client
}

// Compile-time check that SyncAdapter implements TransactionDataProvider
var _ sync.TransactionDataProvider = (*SyncAdapter)(nil)
var _ sync.PositionDataProvider = (*SyncAdapter)(nil)

// NewSyncAdapter creates a new Zerion sync adapter
func NewSyncAdapter(client *Client) *SyncAdapter {
	return &SyncAdapter{client: client}
}

// GetTransactions fetches decoded transactions and converts them to domain types
func (a *SyncAdapter) GetTransactions(ctx context.Context, address string, since time.Time) ([]sync.DecodedTransaction, error) {
	chainIDs := wallet.GetSupportedChains()

	txs, err := a.client.GetTransactions(ctx, address, chainIDs, since)
	if err != nil {
		return nil, err
	}

	result := make([]sync.DecodedTransaction, 0, len(txs))
	for _, td := range txs {
		// Get chain from relationships
		chain := td.Relationships.Chain.Data.ID
		if chain == "" || !wallet.IsValidChain(chain) {
			continue // skip unsupported chains
		}

		dt, err := convertTransaction(td, chain)
		if err != nil {
			continue // skip individual conversion failures
		}
		result = append(result, dt)
	}

	return result, nil
}

// convertTransaction maps a Zerion TransactionData to a domain DecodedTransaction
func convertTransaction(td TransactionData, chain string) (sync.DecodedTransaction, error) {
	minedAt, err := time.Parse(time.RFC3339, td.Attributes.MinedAt)
	if err != nil {
		return sync.DecodedTransaction{}, fmt.Errorf("invalid mined_at: %w", err)
	}

	transfers := make([]sync.DecodedTransfer, 0, len(td.Attributes.Transfers))
	for _, zt := range td.Attributes.Transfers {
		if zt.FungibleInfo == nil {
			continue // skip NFT transfers — only track fungible assets
		}
		dt := convertTransfer(zt, chain)
		transfers = append(transfers, dt)
	}

	var fee *sync.DecodedFee
	if td.Attributes.Fee != nil {
		fee = convertFee(td.Attributes.Fee, chain)
	}

	var protocol string
	if td.Attributes.ApplicationMD != nil {
		protocol = td.Attributes.ApplicationMD.Name
	}

	// Extract NFT token ID from NFT transfers (e.g., Uniswap V3 position NFT)
	var nftTokenID string
	for _, zt := range td.Attributes.Transfers {
		if zt.NftInfo != nil && zt.NftInfo.TokenID != "" {
			nftTokenID = zt.NftInfo.TokenID
			break
		}
	}

	// Extract act types
	acts := make([]string, 0, len(td.Attributes.Acts))
	for _, act := range td.Attributes.Acts {
		acts = append(acts, act.Type)
	}

	return sync.DecodedTransaction{
		ID:            td.ID,
		TxHash:        td.Attributes.Hash,
		ChainID:       chain,
		OperationType: sync.OperationType(td.Attributes.OperationType),
		Protocol:      protocol,
		Transfers:     transfers,
		Fee:           fee,
		MinedAt:       minedAt,
		Status:        td.Attributes.Status,
		NFTTokenID:    nftTokenID,
		Acts:          acts,
	}, nil
}

// convertTransfer maps a Zerion ZTransfer to a domain DecodedTransfer
func convertTransfer(zt ZTransfer, zerionChain string) sync.DecodedTransfer {
	amount := parseIntString(zt.Quantity.Int)

	var direction sync.TransferDirection
	if zt.Direction == "in" {
		direction = sync.DirectionIn
	} else {
		direction = sync.DirectionOut
	}

	var symbol string
	var contractAddr string
	var decimals int

	var assetName string
	var iconURL string

	if zt.FungibleInfo != nil {
		symbol = zt.FungibleInfo.Symbol
		assetName = zt.FungibleInfo.Name
		if zt.FungibleInfo.Icon != nil {
			iconURL = zt.FungibleInfo.Icon.URL
		}
		// Resolve decimals with an explicit found signal: a found implementation
		// is authoritative even when it reports 0 decimals; only fall back to the
		// quantity's own decimals when no chain implementation exists at all.
		if impl := zt.FungibleInfo.ImplementationByChain(zerionChain); impl != nil {
			contractAddr = strings.ToLower(impl.Address)
			decimals = impl.Decimals
		} else {
			decimals = zt.Quantity.Decimals
		}
	}

	var usdPrice *big.Int
	if zt.Price != nil {
		usdPrice = usdFloatToBigInt(*zt.Price)
	}

	return sync.DecodedTransfer{
		AssetSymbol:     symbol,
		AssetName:       assetName,
		ContractAddress: contractAddr,
		Decimals:        decimals,
		Amount:          amount,
		Direction:       direction,
		Sender:          strings.ToLower(zt.Sender),
		Recipient:       strings.ToLower(zt.Recipient),
		USDPrice:        usdPrice,
		IconURL:         iconURL,
	}
}

// convertFee maps a Zerion Fee to a domain DecodedFee. Returns nil if fee is nil.
func convertFee(fee *Fee, zerionChain string) *sync.DecodedFee {
	if fee == nil {
		return nil
	}

	amount := parseIntString(fee.Quantity.Int)

	var symbol string
	var assetName string
	var iconURL string
	var decimals int
	if fee.FungibleInfo != nil {
		symbol = fee.FungibleInfo.Symbol
		assetName = fee.FungibleInfo.Name
		if fee.FungibleInfo.Icon != nil {
			iconURL = fee.FungibleInfo.Icon.URL
		}
		// A found implementation is authoritative (even at 0 decimals); only fall
		// back to the quantity's decimals when no chain implementation exists.
		if impl := fee.FungibleInfo.ImplementationByChain(zerionChain); impl != nil {
			decimals = impl.Decimals
		} else {
			decimals = fee.Quantity.Decimals
		}
	}

	var usdPrice *big.Int
	if fee.Price != nil {
		usdPrice = usdFloatToBigInt(*fee.Price)
	}

	return &sync.DecodedFee{
		AssetSymbol: symbol,
		AssetName:   assetName,
		Amount:      amount,
		Decimals:    decimals,
		USDPrice:    usdPrice,
		IconURL:     iconURL,
	}
}

// parseIntString safely parses a decimal integer string into *big.Int.
// Returns big.NewInt(0) on empty or invalid input.
func parseIntString(s string) *big.Int {
	if s == "" {
		return big.NewInt(0)
	}
	n := new(big.Int)
	if _, ok := n.SetString(s, 10); !ok {
		return big.NewInt(0)
	}
	return n
}

// maxUSDPrice bounds a per-unit USD price we will accept. Above this the scaled
// value risks precision/serialization issues; we treat it as bad data.
const maxUSDPrice = 1e12 // $1 trillion / unit — implausible, reject as bad data

// usdFloatToBigInt converts a USD float64 price to *big.Int scaled by 1e8.
// Example: 3500.12 → 350012000000.
// Returns nil (interpreted by callers as "price unknown") for non-finite,
// negative, or implausibly large prices instead of silently saturating: the
// old int64 conversion wrapped to a garbage-but-plausible rate above ~$9.2e10.
func usdFloatToBigInt(price float64) *big.Int {
	if math.IsNaN(price) || math.IsInf(price, 0) || price < 0 || price > maxUSDPrice {
		return nil
	}
	// Scale by 1e8 via big.Float to avoid int64 overflow/rounding on large prices.
	scaled := new(big.Float).Mul(big.NewFloat(price), big.NewFloat(1e8))
	result, _ := scaled.Int(nil) // truncates toward zero; acceptable at 1e-8 granularity
	return result
}

// GetPositions fetches on-chain positions and converts them to domain types
func (a *SyncAdapter) GetPositions(ctx context.Context, address string) ([]sync.OnChainPosition, error) {
	chainIDs := wallet.GetSupportedChains()

	positions, err := a.client.GetPositions(ctx, address, chainIDs)
	if err != nil {
		return nil, err
	}

	result := make([]sync.OnChainPosition, 0, len(positions))
	for _, pd := range positions {
		chain := pd.Relationships.Chain.Data.ID
		if chain == "" || !wallet.IsValidChain(chain) {
			continue
		}

		var symbol string
		var assetName string
		var iconURL string
		var contractAddr string
		var decimals int
		if pd.Attributes.FungibleInfo != nil {
			symbol = pd.Attributes.FungibleInfo.Symbol
			assetName = pd.Attributes.FungibleInfo.Name
			if pd.Attributes.FungibleInfo.Icon != nil {
				iconURL = pd.Attributes.FungibleInfo.Icon.URL
			}
			// A found implementation is authoritative (even at 0 decimals); only
			// fall back to the quantity's decimals when no chain implementation exists.
			if impl := pd.Attributes.FungibleInfo.ImplementationByChain(chain); impl != nil {
				contractAddr = strings.ToLower(impl.Address)
				decimals = impl.Decimals
			} else {
				decimals = pd.Attributes.Quantity.Decimals
			}
		}

		quantity := parseIntString(pd.Attributes.Quantity.Int)

		var usdPrice *big.Int
		if pd.Attributes.Price > 0 {
			usdPrice = usdFloatToBigInt(pd.Attributes.Price)
		}

		result = append(result, sync.OnChainPosition{
			ChainID:         chain,
			AssetSymbol:     symbol,
			AssetName:       assetName,
			ContractAddress: contractAddr,
			Decimals:        decimals,
			Quantity:        quantity,
			USDPrice:        usdPrice,
			IconURL:         iconURL,
		})
	}

	return result, nil
}
