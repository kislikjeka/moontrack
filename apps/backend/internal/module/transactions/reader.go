package transactions

import (
	"math/big"

	"github.com/google/uuid"
	"github.com/kislikjeka/moontrack/internal/ledger"
	"github.com/kislikjeka/moontrack/internal/platform/rawdata"
)

// ListFields contains the fields needed for transaction list view
type ListFields struct {
	WalletID  uuid.UUID
	AssetID   string
	Amount    *big.Int
	Direction string // "in", "out", "adjustment"
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
	r.register(&IncomeReader{})
	r.register(&OutcomeReader{})
	r.register(&AdjustmentReader{})

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

// IncomeReader parses manual_income transactions
type IncomeReader struct{}

// Type returns the transaction type this reader handles
func (r *IncomeReader) Type() ledger.TransactionType {
	return ledger.TxTypeManualIncome
}

// ReadForList extracts display fields for list view
func (r *IncomeReader) ReadForList(raw map[string]interface{}) (*ListFields, error) {
	income, err := rawdata.ParseIncomeFromRawData(raw)
	if err != nil {
		return nil, err
	}

	return &ListFields{
		WalletID:  income.WalletID,
		AssetID:   income.AssetID,
		Amount:    income.GetAmount(),
		Direction: "in",
	}, nil
}

// ReadForDetail extracts all fields for detail view
func (r *IncomeReader) ReadForDetail(raw map[string]interface{}) (*DetailFields, error) {
	income, err := rawdata.ParseIncomeFromRawData(raw)
	if err != nil {
		return nil, err
	}

	return &DetailFields{
		ListFields: ListFields{
			WalletID:  income.WalletID,
			AssetID:   income.AssetID,
			Amount:    income.GetAmount(),
			Direction: "in",
		},
		Notes: income.Notes,
		ExtraFields: map[string]interface{}{
			"price_asset_id": income.PriceAssetID,
			"price_source":   income.PriceSource,
			"occurred_at":    income.OccurredAt,
		},
	}, nil
}

// OutcomeReader parses manual_outcome transactions
type OutcomeReader struct{}

// Type returns the transaction type this reader handles
func (r *OutcomeReader) Type() ledger.TransactionType {
	return ledger.TxTypeManualOutcome
}

// ReadForList extracts display fields for list view
func (r *OutcomeReader) ReadForList(raw map[string]interface{}) (*ListFields, error) {
	outcome, err := rawdata.ParseOutcomeFromRawData(raw)
	if err != nil {
		return nil, err
	}

	return &ListFields{
		WalletID:  outcome.WalletID,
		AssetID:   outcome.AssetID,
		Amount:    outcome.GetAmount(),
		Direction: "out",
	}, nil
}

// ReadForDetail extracts all fields for detail view
func (r *OutcomeReader) ReadForDetail(raw map[string]interface{}) (*DetailFields, error) {
	outcome, err := rawdata.ParseOutcomeFromRawData(raw)
	if err != nil {
		return nil, err
	}

	return &DetailFields{
		ListFields: ListFields{
			WalletID:  outcome.WalletID,
			AssetID:   outcome.AssetID,
			Amount:    outcome.GetAmount(),
			Direction: "out",
		},
		Notes: outcome.Notes,
		ExtraFields: map[string]interface{}{
			"price_asset_id": outcome.PriceAssetID,
			"price_source":   outcome.PriceSource,
			"occurred_at":    outcome.OccurredAt,
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
