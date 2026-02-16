package zerion

import (
	"context"
	"fmt"
	"math"
	"math/big"
	"strings"
	"time"

	"github.com/kislikjeka/moontrack/internal/platform/sync"
)

// SyncAdapter adapts the Zerion client to the sync.TransactionDataProvider interface
type SyncAdapter struct {
	client *Client
}

// Compile-time check that SyncAdapter implements TransactionDataProvider
var _ sync.TransactionDataProvider = (*SyncAdapter)(nil)

// NewSyncAdapter creates a new Zerion sync adapter
func NewSyncAdapter(client *Client) *SyncAdapter {
	return &SyncAdapter{client: client}
}

// GetTransactions fetches decoded transactions and converts them to domain types
func (a *SyncAdapter) GetTransactions(ctx context.Context, address string, chainID int64, since time.Time) ([]sync.DecodedTransaction, error) {
	zerionChain, ok := IDToZerionChain[chainID]
	if !ok {
		return nil, ErrUnsupportedChain
	}

	txs, err := a.client.GetTransactions(ctx, address, zerionChain, since)
	if err != nil {
		return nil, err
	}

	result := make([]sync.DecodedTransaction, 0, len(txs))
	for _, td := range txs {
		dt, err := convertTransaction(td, chainID, zerionChain)
		if err != nil {
			continue // skip individual conversion failures
		}
		result = append(result, dt)
	}

	return result, nil
}

// convertTransaction maps a Zerion TransactionData to a domain DecodedTransaction
func convertTransaction(td TransactionData, chainID int64, zerionChain string) (sync.DecodedTransaction, error) {
	minedAt, err := time.Parse(time.RFC3339, td.Attributes.MinedAt)
	if err != nil {
		return sync.DecodedTransaction{}, fmt.Errorf("invalid mined_at: %w", err)
	}

	transfers := make([]sync.DecodedTransfer, 0, len(td.Attributes.Transfers))
	for _, zt := range td.Attributes.Transfers {
		dt := convertTransfer(zt, zerionChain)
		transfers = append(transfers, dt)
	}

	var fee *sync.DecodedFee
	if td.Attributes.Fee != nil {
		fee = convertFee(td.Attributes.Fee, zerionChain)
	}

	var protocol string
	if td.Attributes.ApplicationMD != nil {
		protocol = td.Attributes.ApplicationMD.Name
	}

	return sync.DecodedTransaction{
		ID:            td.ID,
		TxHash:        td.Attributes.Hash,
		ChainID:       chainID,
		OperationType: sync.OperationType(td.Attributes.OperationType),
		Protocol:      protocol,
		Transfers:     transfers,
		Fee:           fee,
		MinedAt:       minedAt,
		Status:        td.Attributes.Status,
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

	if zt.FungibleInfo != nil {
		symbol = zt.FungibleInfo.Symbol
		if impl := zt.FungibleInfo.ImplementationByChain(zerionChain); impl != nil {
			contractAddr = strings.ToLower(impl.Address)
			decimals = impl.Decimals
		}
		// Fallback decimals from Quantity if not in implementations
		if decimals == 0 {
			decimals = zt.Quantity.Decimals
		}
	}

	var usdPrice *big.Int
	if zt.Price != nil {
		usdPrice = usdFloatToBigInt(*zt.Price)
	}

	return sync.DecodedTransfer{
		AssetSymbol:     symbol,
		ContractAddress: contractAddr,
		Decimals:        decimals,
		Amount:          amount,
		Direction:       direction,
		Sender:          strings.ToLower(zt.Sender),
		Recipient:       strings.ToLower(zt.Recipient),
		USDPrice:        usdPrice,
	}
}

// convertFee maps a Zerion Fee to a domain DecodedFee. Returns nil if fee is nil.
func convertFee(fee *Fee, zerionChain string) *sync.DecodedFee {
	if fee == nil {
		return nil
	}

	amount := parseIntString(fee.Quantity.Int)

	var symbol string
	var decimals int
	if fee.FungibleInfo != nil {
		symbol = fee.FungibleInfo.Symbol
		if impl := fee.FungibleInfo.ImplementationByChain(zerionChain); impl != nil {
			decimals = impl.Decimals
		}
		if decimals == 0 {
			decimals = fee.Quantity.Decimals
		}
	}

	var usdPrice *big.Int
	if fee.Price != nil {
		usdPrice = usdFloatToBigInt(*fee.Price)
	}

	return &sync.DecodedFee{
		AssetSymbol: symbol,
		Amount:      amount,
		Decimals:    decimals,
		USDPrice:    usdPrice,
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

// usdFloatToBigInt converts a USD float64 price to *big.Int scaled by 1e8.
// Example: 3500.12 → 350012000000.
// Note: safe for prices up to ~$92 billion (int64 max / 1e8).
func usdFloatToBigInt(price float64) *big.Int {
	scaled := math.Round(price * 1e8)
	return big.NewInt(int64(scaled))
}
