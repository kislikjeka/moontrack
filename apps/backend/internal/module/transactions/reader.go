package transactions

import (
	"fmt"
	"math/big"

	"github.com/google/uuid"
	"github.com/kislikjeka/moontrack/internal/ledger"
	"github.com/kislikjeka/moontrack/internal/platform/rawdata"
	"github.com/kislikjeka/moontrack/pkg/money"
)

// Direction constants for transaction display.
const (
	DirectionIn         = "in"
	DirectionOut        = "out"
	DirectionAdjustment = "adjustment"
	DirectionInternal   = "internal"
)

// ListFields contains the fields needed for transaction list view.
//
// AssetID and AssetSymbol are separated by #59: the UUID is the registry
// identity the API returns as a stable key, the symbol is what a human reads.
// They used to be one string, which is why the list view could show a ticker
// and mean whichever same-ticker token happened to be written.
type ListFields struct {
	WalletID    uuid.UUID
	AssetID     uuid.UUID
	AssetSymbol string
	Amount      *big.Int
	USDValue    *big.Int
	Direction   string // DirectionIn, DirectionOut, DirectionAdjustment, DirectionInternal
	ChainID     string // Zerion chain name, e.g. "ethereum", "base"
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

	// Transfer readers
	r.register(&TransferInReader{})
	r.register(&TransferOutReader{})
	r.register(&InternalTransferReader{})
	r.register(&AdjustmentReader{})

	// LP readers
	r.register(&LPReader{txType: ledger.TxTypeLPDeposit, direction: DirectionOut})
	r.register(&LPReader{txType: ledger.TxTypeLPWithdraw, direction: DirectionIn})
	r.register(&LPReader{txType: ledger.TxTypeLPClaimFees, direction: DirectionIn})

	// DeFi readers (same raw_data structure as LP)
	r.register(&LPReader{txType: ledger.TxTypeDefiDeposit, direction: DirectionOut})
	r.register(&LPReader{txType: ledger.TxTypeDefiWithdraw, direction: DirectionIn})
	r.register(&LPReader{txType: ledger.TxTypeDefiClaim, direction: DirectionIn})

	// Lending readers
	r.register(&LendingReader{txType: ledger.TxTypeLendingSupply, direction: DirectionOut})
	r.register(&LendingReader{txType: ledger.TxTypeLendingWithdraw, direction: DirectionIn})
	r.register(&LendingReader{txType: ledger.TxTypeLendingBorrow, direction: DirectionIn})
	r.register(&LendingReader{txType: ledger.TxTypeLendingRepay, direction: DirectionOut})
	r.register(&LendingReader{txType: ledger.TxTypeLendingClaim, direction: DirectionIn})

	// Swap reader
	r.register(&SwapReader{})

	// Genesis reader
	r.register(&GenesisReader{})

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

// --- Transfer readers ---

// TransferInReader parses transfer_in transactions
type TransferInReader struct{}

func (r *TransferInReader) Type() ledger.TransactionType {
	return ledger.TxTypeTransferIn
}

func (r *TransferInReader) ReadForList(raw map[string]interface{}) (*ListFields, error) {
	transfer, err := rawdata.ParseTransferInFromRawData(raw)
	if err != nil {
		return nil, err
	}

	return &ListFields{
		WalletID:    transfer.WalletID,
		AssetID:     transfer.AssetID,
		AssetSymbol: transfer.AssetSymbol,
		Amount:      transfer.GetAmount(),
		USDValue:    money.CalcUSDValue(transfer.GetAmount(), transfer.GetUSDRate(), transfer.Decimals),
		Direction:   DirectionIn,
		ChainID:     transfer.ChainID,
	}, nil
}

func (r *TransferInReader) ReadForDetail(raw map[string]interface{}) (*DetailFields, error) {
	transfer, err := rawdata.ParseTransferInFromRawData(raw)
	if err != nil {
		return nil, err
	}

	return &DetailFields{
		ListFields: ListFields{
			WalletID:    transfer.WalletID,
			AssetID:     transfer.AssetID,
			AssetSymbol: transfer.AssetSymbol,
			Amount:      transfer.GetAmount(),
			USDValue:    money.CalcUSDValue(transfer.GetAmount(), transfer.GetUSDRate(), transfer.Decimals),
			Direction:   DirectionIn,
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

func (r *TransferOutReader) Type() ledger.TransactionType {
	return ledger.TxTypeTransferOut
}

func (r *TransferOutReader) ReadForList(raw map[string]interface{}) (*ListFields, error) {
	transfer, err := rawdata.ParseTransferOutFromRawData(raw)
	if err != nil {
		return nil, err
	}

	return &ListFields{
		WalletID:    transfer.WalletID,
		AssetID:     transfer.AssetID,
		AssetSymbol: transfer.AssetSymbol,
		Amount:      transfer.GetAmount(),
		USDValue:    money.CalcUSDValue(transfer.GetAmount(), transfer.GetUSDRate(), transfer.Decimals),
		Direction:   DirectionOut,
		ChainID:     transfer.ChainID,
	}, nil
}

func (r *TransferOutReader) ReadForDetail(raw map[string]interface{}) (*DetailFields, error) {
	transfer, err := rawdata.ParseTransferOutFromRawData(raw)
	if err != nil {
		return nil, err
	}

	return &DetailFields{
		ListFields: ListFields{
			WalletID:    transfer.WalletID,
			AssetID:     transfer.AssetID,
			AssetSymbol: transfer.AssetSymbol,
			Amount:      transfer.GetAmount(),
			USDValue:    money.CalcUSDValue(transfer.GetAmount(), transfer.GetUSDRate(), transfer.Decimals),
			Direction:   DirectionOut,
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

func (r *InternalTransferReader) Type() ledger.TransactionType {
	return ledger.TxTypeInternalTransfer
}

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
		Direction: DirectionInternal,
		ChainID:   transfer.ChainID,
	}, nil
}

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
			Direction: DirectionInternal,
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

// --- Adjustment reader ---

// AdjustmentReader parses asset_adjustment transactions
type AdjustmentReader struct{}

func (r *AdjustmentReader) Type() ledger.TransactionType {
	return ledger.TxTypeAssetAdjustment
}

func (r *AdjustmentReader) ReadForList(raw map[string]interface{}) (*ListFields, error) {
	adj, err := rawdata.ParseAdjustmentFromRawData(raw)
	if err != nil {
		return nil, err
	}

	return &ListFields{
		WalletID:    adj.WalletID,
		AssetID:     adj.AssetID,
		AssetSymbol: adj.AssetSymbol,
		Amount:      adj.GetNewBalance(),
		USDValue:    money.CalcUSDValue(adj.GetNewBalance(), adj.GetUSDRate(), adj.Decimals),
		Direction:   DirectionAdjustment,
	}, nil
}

func (r *AdjustmentReader) ReadForDetail(raw map[string]interface{}) (*DetailFields, error) {
	adj, err := rawdata.ParseAdjustmentFromRawData(raw)
	if err != nil {
		return nil, err
	}

	return &DetailFields{
		ListFields: ListFields{
			WalletID:    adj.WalletID,
			AssetID:     adj.AssetID,
			AssetSymbol: adj.AssetSymbol,
			Amount:      adj.GetNewBalance(),
			USDValue:    money.CalcUSDValue(adj.GetNewBalance(), adj.GetUSDRate(), adj.Decimals),
			Direction:   DirectionAdjustment,
		},
		Notes: adj.Notes,
		ExtraFields: map[string]interface{}{
			"new_balance":  adj.GetNewBalance().String(),
			"price_source": adj.PriceSource,
			"occurred_at":  adj.OccurredAt,
		},
	}, nil
}

// --- LP / DeFi reader (shared) ---

// LPReader parses LP and DeFi transactions with a "transfers" array.
// The reader picks the primary transfer based on direction.
type LPReader struct {
	txType    ledger.TransactionType
	direction string
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

	assetID, assetSymbol, amount, usdValue := r.primaryTransfer(raw)
	chainID, _ := raw["chain_id"].(string)

	return &ListFields{
		WalletID:    walletID,
		AssetID:     assetID,
		AssetSymbol: assetSymbol,
		Amount:      amount,
		USDValue:    usdValue,
		Direction:   r.direction,
		ChainID:     chainID,
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
func (r *LPReader) primaryTransfer(raw map[string]interface{}) (uuid.UUID, string, *big.Int, *big.Int) {
	transfers, ok := raw["transfers"].([]map[string]interface{})
	if !ok {
		// Try type assertion for []interface{} (JSON roundtrip)
		if arr, ok2 := raw["transfers"].([]interface{}); ok2 {
			for _, item := range arr {
				if m, ok3 := item.(map[string]interface{}); ok3 {
					dir, _ := m["direction"].(string)
					if dir == r.direction {
						return extractTransferFields(m)
					}
				}
			}
		}
		return uuid.Nil, "", big.NewInt(0), nil
	}

	for _, t := range transfers {
		dir, _ := t["direction"].(string)
		if dir == r.direction {
			return extractTransferFields(t)
		}
	}

	// Fallback: first transfer regardless of direction
	if len(transfers) > 0 {
		return extractTransferFields(transfers[0])
	}
	return uuid.Nil, "", big.NewInt(0), nil
}

// --- Lending reader ---

// LendingReader parses lending transactions with flat raw_data structure.
type LendingReader struct {
	txType    ledger.TransactionType
	direction string
}

func (r *LendingReader) Type() ledger.TransactionType {
	return r.txType
}

func (r *LendingReader) ReadForList(raw map[string]interface{}) (*ListFields, error) {
	walletIDStr, _ := raw["wallet_id"].(string)
	walletID, err := uuid.Parse(walletIDStr)
	if err != nil {
		return nil, fmt.Errorf("invalid wallet_id in lending transaction: %w", err)
	}

	// setLendingAssetFields (sync) writes the registry UUID under "asset_id"
	// and the ticker under "asset". A malformed id yields uuid.Nil here rather
	// than an error: this is the list view, and refusing to render a row is a
	// worse outcome than rendering it without a stable asset key.
	assetID, _ := uuid.Parse(stringField(raw, "asset_id"))
	assetSymbol, _ := raw["asset"].(string)
	amount := parseBigIntField(raw, "amount")
	chainID, _ := raw["chain_id"].(string)

	var usdValue *big.Int
	decimals := 0
	if d, ok := raw["decimals"].(float64); ok {
		decimals = int(d)
	}
	if priceStr, ok := raw["usd_price"].(string); ok && priceStr != "" && priceStr != "0" {
		price, ok := new(big.Int).SetString(priceStr, 10)
		if ok {
			usdValue = money.CalcUSDValue(amount, price, decimals)
		}
	}

	return &ListFields{
		WalletID:    walletID,
		AssetID:     assetID,
		AssetSymbol: assetSymbol,
		Amount:      amount,
		USDValue:    usdValue,
		Direction:   r.direction,
		ChainID:     chainID,
	}, nil
}

func (r *LendingReader) ReadForDetail(raw map[string]interface{}) (*DetailFields, error) {
	fields, err := r.ReadForList(raw)
	if err != nil {
		return nil, err
	}

	extras := map[string]interface{}{}
	for _, key := range []string{"tx_hash", "chain_id", "protocol", "contract_address", "occurred_at"} {
		if v, ok := raw[key]; ok {
			extras[key] = v
		}
	}

	return &DetailFields{
		ListFields:  *fields,
		ExtraFields: extras,
	}, nil
}

// --- Swap reader ---

// SwapReader parses swap transactions with transfers_in/transfers_out arrays.
type SwapReader struct{}

func (r *SwapReader) Type() ledger.TransactionType {
	return ledger.TxTypeSwap
}

func (r *SwapReader) ReadForList(raw map[string]interface{}) (*ListFields, error) {
	walletIDStr, _ := raw["wallet_id"].(string)
	walletID, err := uuid.Parse(walletIDStr)
	if err != nil {
		return nil, fmt.Errorf("invalid wallet_id in swap transaction: %w", err)
	}

	chainID, _ := raw["chain_id"].(string)

	// Display the received asset (transfers_in) as primary
	assetID, assetSymbol, amount, usdValue := firstTransferFromArray(raw, "transfers_in")

	return &ListFields{
		WalletID:    walletID,
		AssetID:     assetID,
		AssetSymbol: assetSymbol,
		Amount:      amount,
		USDValue:    usdValue,
		Direction:   DirectionIn,
		ChainID:     chainID,
	}, nil
}

func (r *SwapReader) ReadForDetail(raw map[string]interface{}) (*DetailFields, error) {
	fields, err := r.ReadForList(raw)
	if err != nil {
		return nil, err
	}

	extras := map[string]interface{}{}
	for _, key := range []string{"tx_hash", "chain_id", "protocol", "occurred_at"} {
		if v, ok := raw[key]; ok {
			extras[key] = v
		}
	}

	return &DetailFields{
		ListFields:  *fields,
		ExtraFields: extras,
	}, nil
}

// --- Genesis reader ---

// GenesisReader parses genesis_balance transactions.
type GenesisReader struct{}

func (r *GenesisReader) Type() ledger.TransactionType {
	return ledger.TxTypeGenesisBalance
}

func (r *GenesisReader) ReadForList(raw map[string]interface{}) (*ListFields, error) {
	walletIDStr, _ := raw["wallet_id"].(string)
	walletID, err := uuid.Parse(walletIDStr)
	if err != nil {
		return nil, fmt.Errorf("invalid wallet_id in genesis transaction: %w", err)
	}

	// Genesis raw_data carries the registry UUID under asset_id and the ticker
	// under asset_symbol (#59). uuid.Nil on a malformed id — the list view
	// degrades rather than dropping the row.
	assetID, _ := uuid.Parse(stringField(raw, "asset_id"))
	assetSymbol, _ := raw["asset_symbol"].(string)
	amount := parseBigIntField(raw, "amount")
	chainID, _ := raw["chain_id"].(string)

	var usdValue *big.Int
	decimals := 0
	if d, ok := raw["decimals"].(float64); ok {
		decimals = int(d)
	}
	if rateStr, ok := raw["usd_rate"].(string); ok && rateStr != "0" {
		rate, ok := new(big.Int).SetString(rateStr, 10)
		if ok {
			usdValue = money.CalcUSDValue(amount, rate, decimals)
		}
	}

	return &ListFields{
		WalletID:    walletID,
		AssetID:     assetID,
		AssetSymbol: assetSymbol,
		Amount:      amount,
		USDValue:    usdValue,
		Direction:   DirectionIn,
		ChainID:     chainID,
	}, nil
}

func (r *GenesisReader) ReadForDetail(raw map[string]interface{}) (*DetailFields, error) {
	fields, err := r.ReadForList(raw)
	if err != nil {
		return nil, err
	}

	extras := map[string]interface{}{}
	for _, key := range []string{"chain_id", "occurred_at"} {
		if v, ok := raw[key]; ok {
			extras[key] = v
		}
	}

	return &DetailFields{
		ListFields:  *fields,
		ExtraFields: extras,
	}, nil
}

// --- Helpers ---

// extractTransferFields extracts the registry id, symbol, amount and USD value
// from a transfer map. The id is the key the API returns; the symbol is what
// gets rendered (#59). An unparseable id yields uuid.Nil rather than an error —
// this is the read path, and a row with no stable key still displays.
func extractTransferFields(t map[string]interface{}) (uuid.UUID, string, *big.Int, *big.Int) {
	assetID, _ := uuid.Parse(stringField(t, "asset_id"))
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
	if priceStr, ok := t["usd_price"].(string); ok && priceStr != "" && priceStr != "0" {
		price, ok := new(big.Int).SetString(priceStr, 10)
		if ok {
			usdValue = money.CalcUSDValue(amount, price, decimals)
		}
	}

	return assetID, symbol, amount, usdValue
}

// stringField reads a string value from a raw_data map, returning "" when the
// key is absent or holds another type.
func stringField(raw map[string]interface{}, key string) string {
	v, _ := raw[key].(string)
	return v
}

// parseBigIntField parses a string field from raw data into a *big.Int.
func parseBigIntField(raw map[string]interface{}, key string) *big.Int {
	if str, ok := raw[key].(string); ok {
		v, ok := new(big.Int).SetString(str, 10)
		if ok {
			return v
		}
	}
	return big.NewInt(0)
}

// firstTransferFromArray extracts the first element from a named transfer array
// (e.g., "transfers_in" or "transfers_out") and returns its fields.
func firstTransferFromArray(raw map[string]interface{}, key string) (uuid.UUID, string, *big.Int, *big.Int) {
	if arr, ok := raw[key].([]interface{}); ok && len(arr) > 0 {
		if m, ok := arr[0].(map[string]interface{}); ok {
			return extractTransferFields(m)
		}
	}
	if arr, ok := raw[key].([]map[string]interface{}); ok && len(arr) > 0 {
		return extractTransferFields(arr[0])
	}
	return uuid.Nil, "", big.NewInt(0), nil
}
