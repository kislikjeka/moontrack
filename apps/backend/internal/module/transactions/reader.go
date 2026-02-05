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
	Direction string // "in", "out", "adjustment", "internal"
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
		Direction: "in",
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
		Direction: "out",
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
		Direction: "internal",
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
