package transactions

import (
	"fmt"
	"math/big"

	"github.com/google/uuid"
	"github.com/kislikjeka/moontrack/internal/ledger"
	"github.com/kislikjeka/moontrack/internal/platform/rawdata"
	"github.com/kislikjeka/moontrack/pkg/money"
)

// ListFields contains the fields needed for transaction list view
type ListFields struct {
	WalletID  uuid.UUID
	AssetID   string
	Amount    *big.Int
	USDValue  *big.Int
	Direction string // "in", "out", "adjustment", "internal"
	ChainID   string // Zerion chain name, e.g. "ethereum", "base"
}

// DetailFields contains the fields needed for transaction detail view
type DetailFields struct {
	ListFields
	Notes       string
	ExtraFields map[string]interface{} // Type-specific fields for display
}

// TransactionReader defines the interface for parsing type-specific raw_data
type TransactionReader interface {
	// Type returns the transaction type this reader handles
	Type() ledger.TransactionType

	// ReadForList extracts display fields for list view
	ReadForList(raw map[string]interface{}) (*ListFields, error)

	// ReadForDetail extracts all fields for detail view
	ReadForDetail(raw map[string]interface{}) (*DetailFields, error)
}

// ReaderRegistry holds all transaction readers
type ReaderRegistry struct {
	readers map[ledger.TransactionType]TransactionReader
}

// NewReaderRegistry creates a new reader registry with all readers registered
func NewReaderRegistry() *ReaderRegistry {
	r := &ReaderRegistry{
		readers: make(map[ledger.TransactionType]TransactionReader),
	}

	// Register all readers at creation time
	r.register(&TransferInReader{})
	r.register(&TransferOutReader{})
	r.register(&InternalTransferReader{})
	r.register(&AdjustmentReader{})
	r.register(&LPReader{txType: ledger.TxTypeLPDeposit, direction: "out"})
	r.register(&LPReader{txType: ledger.TxTypeLPWithdraw, direction: "in"})
	r.register(&LPReader{txType: ledger.TxTypeLPClaimFees, direction: "in"})

	return r
}

// register adds a reader to the registry
func (r *ReaderRegistry) register(reader TransactionReader) {
	r.readers[reader.Type()] = reader
}

// GetReader retrieves a reader by transaction type
func (r *ReaderRegistry) GetReader(txType ledger.TransactionType) (TransactionReader, bool) {
	reader, ok := r.readers[txType]
	return reader, ok
}

// TransferInReader parses transfer_in transactions
type TransferInReader struct{}

// Type returns the transaction type this reader handles
func (r *TransferInReader) Type() ledger.TransactionType {
	return ledger.TxTypeTransferIn
}

// ReadForList extracts display fields for list view
func (r *TransferInReader) ReadForList(raw map[string]interface{}) (*ListFields, error) {
	transfer, err := rawdata.ParseTransferInFromRawData(raw)
	if err != nil {
		return nil, err
	}

	return &ListFields{
		WalletID:  transfer.WalletID,
		AssetID:   transfer.AssetID,
		Amount:    transfer.GetAmount(),
		USDValue:  money.CalcUSDValue(transfer.GetAmount(), transfer.GetUSDRate(), transfer.Decimals),
		Direction: "in",
		ChainID:   transfer.ChainID,
	}, nil
}

// ReadForDetail extracts all fields for detail view
func (r *TransferInReader) ReadForDetail(raw map[string]interface{}) (*DetailFields, error) {
	transfer, err := rawdata.ParseTransferInFromRawData(raw)
	if err != nil {
		return nil, err
	}

	return &DetailFields{
		ListFields: ListFields{
			WalletID:  transfer.WalletID,
			AssetID:   transfer.AssetID,
			Amount:    transfer.GetAmount(),
			USDValue:  money.CalcUSDValue(transfer.GetAmount(), transfer.GetUSDRate(), transfer.Decimals),
			Direction: "in",
		},
		ExtraFields: map[string]interface{}{
			"tx_hash":          transfer.TxHash,
			"block_number":     transfer.BlockNumber,
			"from_address":     transfer.FromAddress,
			"chain_id":         transfer.ChainID,
			"contract_address": transfer.ContractAddress,
			"occurred_at":      transfer.OccurredAt,
		},
	}, nil
}

// TransferOutReader parses transfer_out transactions
type TransferOutReader struct{}

// Type returns the transaction type this reader handles
func (r *TransferOutReader) Type() ledger.TransactionType {
	return ledger.TxTypeTransferOut
}

// ReadForList extracts display fields for list view
func (r *TransferOutReader) ReadForList(raw map[string]interface{}) (*ListFields, error) {
	transfer, err := rawdata.ParseTransferOutFromRawData(raw)
	if err != nil {
		return nil, err
	}

	return &ListFields{
		WalletID:  transfer.WalletID,
		AssetID:   transfer.AssetID,
		Amount:    transfer.GetAmount(),
		USDValue:  money.CalcUSDValue(transfer.GetAmount(), transfer.GetUSDRate(), transfer.Decimals),
		Direction: "out",
		ChainID:   transfer.ChainID,
	}, nil
}

// ReadForDetail extracts all fields for detail view
func (r *TransferOutReader) ReadForDetail(raw map[string]interface{}) (*DetailFields, error) {
	transfer, err := rawdata.ParseTransferOutFromRawData(raw)
	if err != nil {
		return nil, err
	}

	return &DetailFields{
		ListFields: ListFields{
			WalletID:  transfer.WalletID,
			AssetID:   transfer.AssetID,
			Amount:    transfer.GetAmount(),
			USDValue:  money.CalcUSDValue(transfer.GetAmount(), transfer.GetUSDRate(), transfer.Decimals),
			Direction: "out",
		},
		ExtraFields: map[string]interface{}{
			"tx_hash":          transfer.TxHash,
			"block_number":     transfer.BlockNumber,
			"to_address":       transfer.ToAddress,
			"chain_id":         transfer.ChainID,
			"contract_address": transfer.ContractAddress,
			"occurred_at":      transfer.OccurredAt,
		},
	}, nil
}

// InternalTransferReader parses internal_transfer transactions
type InternalTransferReader struct{}

// Type returns the transaction type this reader handles
func (r *InternalTransferReader) Type() ledger.TransactionType {
	return ledger.TxTypeInternalTransfer
}

// ReadForList extracts display fields for list view
func (r *InternalTransferReader) ReadForList(raw map[string]interface{}) (*ListFields, error) {
	transfer, err := rawdata.ParseInternalTransferFromRawData(raw)
	if err != nil {
		return nil, err
	}

	return &ListFields{
		WalletID:  transfer.SourceWalletID,
		AssetID:   transfer.AssetID,
		Amount:    transfer.GetAmount(),
		USDValue:  money.CalcUSDValue(transfer.GetAmount(), transfer.GetUSDRate(), transfer.Decimals),
		Direction: "internal",
		ChainID:   transfer.ChainID,
	}, nil
}

// ReadForDetail extracts all fields for detail view
func (r *InternalTransferReader) ReadForDetail(raw map[string]interface{}) (*DetailFields, error) {
	transfer, err := rawdata.ParseInternalTransferFromRawData(raw)
	if err != nil {
		return nil, err
	}

	return &DetailFields{
		ListFields: ListFields{
			WalletID:  transfer.SourceWalletID,
			AssetID:   transfer.AssetID,
			Amount:    transfer.GetAmount(),
			USDValue:  money.CalcUSDValue(transfer.GetAmount(), transfer.GetUSDRate(), transfer.Decimals),
			Direction: "internal",
		},
		ExtraFields: map[string]interface{}{
			"source_wallet_id": transfer.SourceWalletID,
			"dest_wallet_id":   transfer.DestWalletID,
			"tx_hash":          transfer.TxHash,
			"block_number":     transfer.BlockNumber,
			"chain_id":         transfer.ChainID,
			"contract_address": transfer.ContractAddress,
			"occurred_at":      transfer.OccurredAt,
		},
	}, nil
}

// AdjustmentReader parses asset_adjustment transactions
type AdjustmentReader struct{}

// Type returns the transaction type this reader handles
func (r *AdjustmentReader) Type() ledger.TransactionType {
	return ledger.TxTypeAssetAdjustment
}

// ReadForList extracts display fields for list view
func (r *AdjustmentReader) ReadForList(raw map[string]interface{}) (*ListFields, error) {
	adj, err := rawdata.ParseAdjustmentFromRawData(raw)
	if err != nil {
		return nil, err
	}

	return &ListFields{
		WalletID:  adj.WalletID,
		AssetID:   adj.AssetID,
		Amount:    adj.GetNewBalance(),
		USDValue:  money.CalcUSDValue(adj.GetNewBalance(), adj.GetUSDRate(), adj.Decimals),
		Direction: "adjustment",
	}, nil
}

// ReadForDetail extracts all fields for detail view
func (r *AdjustmentReader) ReadForDetail(raw map[string]interface{}) (*DetailFields, error) {
	adj, err := rawdata.ParseAdjustmentFromRawData(raw)
	if err != nil {
		return nil, err
	}

	return &DetailFields{
		ListFields: ListFields{
			WalletID:  adj.WalletID,
			AssetID:   adj.AssetID,
			Amount:    adj.GetNewBalance(),
			USDValue:  money.CalcUSDValue(adj.GetNewBalance(), adj.GetUSDRate(), adj.Decimals),
			Direction: "adjustment",
		},
		Notes: adj.Notes,
		ExtraFields: map[string]interface{}{
			"new_balance":  adj.GetNewBalance().String(),
			"price_source": adj.PriceSource,
			"occurred_at":  adj.OccurredAt,
		},
	}, nil
}

// LPReader parses LP deposit/withdraw/claim_fees transactions.
// LP raw data has a "transfers" array; the reader picks the primary transfer
// based on direction (out for deposits, in for withdraws/claims).
type LPReader struct {
	txType    ledger.TransactionType
	direction string // primary direction to display: "in" or "out"
}

func (r *LPReader) Type() ledger.TransactionType {
	return r.txType
}

func (r *LPReader) ReadForList(raw map[string]interface{}) (*ListFields, error) {
	walletIDStr, _ := raw["wallet_id"].(string)
	walletID, err := uuid.Parse(walletIDStr)
	if err != nil {
		return nil, fmt.Errorf("invalid wallet_id in LP transaction: %w", err)
	}

	assetSymbol, amount, usdValue := r.primaryTransfer(raw)

	return &ListFields{
		WalletID:  walletID,
		AssetID:   assetSymbol,
		Amount:    amount,
		USDValue:  usdValue,
		Direction: r.direction,
	}, nil
}

func (r *LPReader) ReadForDetail(raw map[string]interface{}) (*DetailFields, error) {
	fields, err := r.ReadForList(raw)
	if err != nil {
		return nil, err
	}

	extras := map[string]interface{}{}
	if v, ok := raw["tx_hash"]; ok {
		extras["tx_hash"] = v
	}
	if v, ok := raw["chain_id"]; ok {
		extras["chain_id"] = v
	}
	if v, ok := raw["protocol"]; ok {
		extras["protocol"] = v
	}
	if v, ok := raw["nft_token_id"]; ok {
		extras["nft_token_id"] = v
	}
	if v, ok := raw["occurred_at"]; ok {
		extras["occurred_at"] = v
	}

	return &DetailFields{
		ListFields:  *fields,
		ExtraFields: extras,
	}, nil
}

// primaryTransfer finds the first transfer matching the reader's direction
// and returns its symbol, amount, and USD value.
func (r *LPReader) primaryTransfer(raw map[string]interface{}) (string, *big.Int, *big.Int) {
	transfers, ok := raw["transfers"].([]map[string]interface{})
	if !ok {
		// Try type assertion for []interface{} (JSON roundtrip)
		if arr, ok2 := raw["transfers"].([]interface{}); ok2 {
			for _, item := range arr {
				if m, ok3 := item.(map[string]interface{}); ok3 {
					dir, _ := m["direction"].(string)
					if dir == r.direction {
						return r.extractTransferFields(m)
					}
				}
			}
		}
		return "", big.NewInt(0), nil
	}

	for _, t := range transfers {
		dir, _ := t["direction"].(string)
		if dir == r.direction {
			return r.extractTransferFields(t)
		}
	}

	// Fallback: first transfer regardless of direction
	if len(transfers) > 0 {
		return r.extractTransferFields(transfers[0])
	}
	return "", big.NewInt(0), nil
}

func (r *LPReader) extractTransferFields(t map[string]interface{}) (string, *big.Int, *big.Int) {
	symbol, _ := t["asset_symbol"].(string)
	amount := big.NewInt(0)
	if amtStr, ok := t["amount"].(string); ok {
		amount, _ = new(big.Int).SetString(amtStr, 10)
		if amount == nil {
			amount = big.NewInt(0)
		}
	}

	var usdValue *big.Int
	decimals := 0
	if d, ok := t["decimals"].(float64); ok {
		decimals = int(d)
	}
	if priceStr, ok := t["usd_price"].(string); ok && priceStr != "0" {
		price, ok := new(big.Int).SetString(priceStr, 10)
		if ok {
			usdValue = money.CalcUSDValue(amount, price, decimals)
		}
	}

	return symbol, amount, usdValue
}
