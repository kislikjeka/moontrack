package rawdata

import (
	"math/big"
	"time"

	"github.com/google/uuid"
	"github.com/kislikjeka/moontrack/pkg/money"
)

// TransferInData represents parsed data from a transfer_in transaction's raw_data
type TransferInData struct {
	WalletID uuid.UUID
	// AssetID is the registry UUID (#59); AssetSymbol is the ticker beside it in
	// raw_data. This package feeds the transaction list and detail views only —
	// nothing parsed here reaches a ledger entry — so the symbol is what gets
	// rendered and the UUID is the stable key the API returns alongside it.
	AssetID         uuid.UUID
	AssetSymbol     string
	Decimals        int
	Amount          *money.BigInt
	USDRate         *money.BigInt
	ChainID         string
	TxHash          string
	BlockNumber     int64
	FromAddress     string
	ContractAddress string
	OccurredAt      time.Time
	UniqueID        string
}

// GetAmount returns the amount as *big.Int
func (d *TransferInData) GetAmount() *big.Int {
	if d.Amount == nil {
		return big.NewInt(0)
	}
	return d.Amount.ToBigInt()
}

// GetUSDRate returns the USD rate as *big.Int
func (d *TransferInData) GetUSDRate() *big.Int {
	if d.USDRate == nil {
		return big.NewInt(0)
	}
	return d.USDRate.ToBigInt()
}

// ParseTransferInFromRawData parses a raw_data map into TransferInData
func ParseTransferInFromRawData(raw map[string]interface{}) (*TransferInData, error) {
	data := &TransferInData{}

	// Parse wallet_id
	if walletIDStr, ok := raw["wallet_id"].(string); ok {
		walletID, err := uuid.Parse(walletIDStr)
		if err != nil {
			return nil, ErrInvalidWalletID
		}
		data.WalletID = walletID
	}

	// Parse asset_id. A present-but-malformed id is an error: this is a read
	// path, but returning uuid.Nil would put a bogus id in the API response
	// where the caller cannot tell it apart from a real one (#59).
	if assetIDStr, ok := raw["asset_id"].(string); ok {
		assetID, err := uuid.Parse(assetIDStr)
		if err != nil {
			return nil, ErrInvalidAssetID
		}
		data.AssetID = assetID
	}

	// asset_symbol is display data — absent is fine, it renders blank.
	if symbol, ok := raw["asset_symbol"].(string); ok {
		data.AssetSymbol = symbol
	}

	// Parse amount
	if amountStr, ok := raw["amount"].(string); ok {
		amount, ok := money.NewBigIntFromString(amountStr)
		if !ok {
			return nil, ErrInvalidAmount
		}
		data.Amount = amount
	}

	// Parse usd_rate (optional)
	if usdRateStr, ok := raw["usd_rate"].(string); ok && usdRateStr != "" {
		usdRate, ok := money.NewBigIntFromString(usdRateStr)
		if !ok {
			return nil, ErrInvalidUSDRate
		}
		data.USDRate = usdRate
	}

	// Parse chain_id
	if chainID, ok := raw["chain_id"].(string); ok {
		data.ChainID = chainID
	}

	// Parse tx_hash
	if txHash, ok := raw["tx_hash"].(string); ok {
		data.TxHash = txHash
	}

	// Parse block_number
	if blockNum, ok := raw["block_number"].(float64); ok {
		data.BlockNumber = int64(blockNum)
	} else if blockNum, ok := raw["block_number"].(int64); ok {
		data.BlockNumber = blockNum
	}

	// Parse from_address
	if fromAddr, ok := raw["from_address"].(string); ok {
		data.FromAddress = fromAddr
	}

	// Parse contract_address
	if contractAddr, ok := raw["contract_address"].(string); ok {
		data.ContractAddress = contractAddr
	}

	// Parse occurred_at
	if occurredAtStr, ok := raw["occurred_at"].(string); ok {
		occurredAt, err := time.Parse(time.RFC3339, occurredAtStr)
		if err != nil {
			return nil, err
		}
		data.OccurredAt = occurredAt
	}

	// Parse unique_id
	if uniqueID, ok := raw["unique_id"].(string); ok {
		data.UniqueID = uniqueID
	}

	// Parse decimals (default to 18 for EVM tokens)
	data.Decimals = 18
	if decimals, ok := raw["decimals"].(float64); ok {
		data.Decimals = int(decimals)
	} else if decimals, ok := raw["decimals"].(int); ok {
		data.Decimals = decimals
	}

	return data, nil
}

// TransferOutData represents parsed data from a transfer_out transaction's raw_data
type TransferOutData struct {
	WalletID uuid.UUID
	// AssetID is the registry UUID (#59); AssetSymbol is the ticker beside it in
	// raw_data. This package feeds the transaction list and detail views only —
	// nothing parsed here reaches a ledger entry — so the symbol is what gets
	// rendered and the UUID is the stable key the API returns alongside it.
	AssetID         uuid.UUID
	AssetSymbol     string
	Decimals        int
	Amount          *money.BigInt
	USDRate         *money.BigInt
	GasAmount       *money.BigInt
	GasUSDRate      *money.BigInt
	ChainID         string
	TxHash          string
	BlockNumber     int64
	ToAddress       string
	ContractAddress string
	OccurredAt      time.Time
	UniqueID        string
}

// GetAmount returns the amount as *big.Int
func (d *TransferOutData) GetAmount() *big.Int {
	if d.Amount == nil {
		return big.NewInt(0)
	}
	return d.Amount.ToBigInt()
}

// GetUSDRate returns the USD rate as *big.Int
func (d *TransferOutData) GetUSDRate() *big.Int {
	if d.USDRate == nil {
		return big.NewInt(0)
	}
	return d.USDRate.ToBigInt()
}

// ParseTransferOutFromRawData parses a raw_data map into TransferOutData
func ParseTransferOutFromRawData(raw map[string]interface{}) (*TransferOutData, error) {
	data := &TransferOutData{}

	// Parse wallet_id
	if walletIDStr, ok := raw["wallet_id"].(string); ok {
		walletID, err := uuid.Parse(walletIDStr)
		if err != nil {
			return nil, ErrInvalidWalletID
		}
		data.WalletID = walletID
	}

	// Parse asset_id. A present-but-malformed id is an error: this is a read
	// path, but returning uuid.Nil would put a bogus id in the API response
	// where the caller cannot tell it apart from a real one (#59).
	if assetIDStr, ok := raw["asset_id"].(string); ok {
		assetID, err := uuid.Parse(assetIDStr)
		if err != nil {
			return nil, ErrInvalidAssetID
		}
		data.AssetID = assetID
	}

	// asset_symbol is display data — absent is fine, it renders blank.
	if symbol, ok := raw["asset_symbol"].(string); ok {
		data.AssetSymbol = symbol
	}

	// Parse amount
	if amountStr, ok := raw["amount"].(string); ok {
		amount, ok := money.NewBigIntFromString(amountStr)
		if !ok {
			return nil, ErrInvalidAmount
		}
		data.Amount = amount
	}

	// Parse usd_rate (optional)
	if usdRateStr, ok := raw["usd_rate"].(string); ok && usdRateStr != "" {
		usdRate, ok := money.NewBigIntFromString(usdRateStr)
		if !ok {
			return nil, ErrInvalidUSDRate
		}
		data.USDRate = usdRate
	}

	// Parse chain_id
	if chainID, ok := raw["chain_id"].(string); ok {
		data.ChainID = chainID
	}

	// Parse tx_hash
	if txHash, ok := raw["tx_hash"].(string); ok {
		data.TxHash = txHash
	}

	// Parse block_number
	if blockNum, ok := raw["block_number"].(float64); ok {
		data.BlockNumber = int64(blockNum)
	} else if blockNum, ok := raw["block_number"].(int64); ok {
		data.BlockNumber = blockNum
	}

	// Parse to_address
	if toAddr, ok := raw["to_address"].(string); ok {
		data.ToAddress = toAddr
	}

	// Parse contract_address
	if contractAddr, ok := raw["contract_address"].(string); ok {
		data.ContractAddress = contractAddr
	}

	// Parse occurred_at
	if occurredAtStr, ok := raw["occurred_at"].(string); ok {
		occurredAt, err := time.Parse(time.RFC3339, occurredAtStr)
		if err != nil {
			return nil, err
		}
		data.OccurredAt = occurredAt
	}

	// Parse unique_id
	if uniqueID, ok := raw["unique_id"].(string); ok {
		data.UniqueID = uniqueID
	}

	// Parse decimals (default to 18 for EVM tokens)
	data.Decimals = 18
	if decimals, ok := raw["decimals"].(float64); ok {
		data.Decimals = int(decimals)
	} else if decimals, ok := raw["decimals"].(int); ok {
		data.Decimals = decimals
	}

	return data, nil
}

// InternalTransferData represents parsed data from an internal_transfer transaction's raw_data
type InternalTransferData struct {
	SourceWalletID uuid.UUID
	DestWalletID   uuid.UUID
	// AssetID is the registry UUID (#59); AssetSymbol is the ticker beside it in
	// raw_data. This package feeds the transaction list and detail views only —
	// nothing parsed here reaches a ledger entry — so the symbol is what gets
	// rendered and the UUID is the stable key the API returns alongside it.
	AssetID         uuid.UUID
	AssetSymbol     string
	Decimals        int
	Amount          *money.BigInt
	USDRate         *money.BigInt
	GasAmount       *money.BigInt
	GasUSDRate      *money.BigInt
	ChainID         string
	TxHash          string
	BlockNumber     int64
	ContractAddress string
	OccurredAt      time.Time
	UniqueID        string
}

// GetAmount returns the amount as *big.Int
func (d *InternalTransferData) GetAmount() *big.Int {
	if d.Amount == nil {
		return big.NewInt(0)
	}
	return d.Amount.ToBigInt()
}

// GetUSDRate returns the USD rate as *big.Int
func (d *InternalTransferData) GetUSDRate() *big.Int {
	if d.USDRate == nil {
		return big.NewInt(0)
	}
	return d.USDRate.ToBigInt()
}

// ParseInternalTransferFromRawData parses a raw_data map into InternalTransferData
func ParseInternalTransferFromRawData(raw map[string]interface{}) (*InternalTransferData, error) {
	data := &InternalTransferData{}

	// Parse source_wallet_id
	if walletIDStr, ok := raw["source_wallet_id"].(string); ok {
		walletID, err := uuid.Parse(walletIDStr)
		if err != nil {
			return nil, ErrInvalidWalletID
		}
		data.SourceWalletID = walletID
	}

	// Parse dest_wallet_id
	if walletIDStr, ok := raw["dest_wallet_id"].(string); ok {
		walletID, err := uuid.Parse(walletIDStr)
		if err != nil {
			return nil, ErrInvalidWalletID
		}
		data.DestWalletID = walletID
	}

	// Parse asset_id. A present-but-malformed id is an error: this is a read
	// path, but returning uuid.Nil would put a bogus id in the API response
	// where the caller cannot tell it apart from a real one (#59).
	if assetIDStr, ok := raw["asset_id"].(string); ok {
		assetID, err := uuid.Parse(assetIDStr)
		if err != nil {
			return nil, ErrInvalidAssetID
		}
		data.AssetID = assetID
	}

	// asset_symbol is display data — absent is fine, it renders blank.
	if symbol, ok := raw["asset_symbol"].(string); ok {
		data.AssetSymbol = symbol
	}

	// Parse amount
	if amountStr, ok := raw["amount"].(string); ok {
		amount, ok := money.NewBigIntFromString(amountStr)
		if !ok {
			return nil, ErrInvalidAmount
		}
		data.Amount = amount
	}

	// Parse usd_rate (optional)
	if usdRateStr, ok := raw["usd_rate"].(string); ok && usdRateStr != "" {
		usdRate, ok := money.NewBigIntFromString(usdRateStr)
		if !ok {
			return nil, ErrInvalidUSDRate
		}
		data.USDRate = usdRate
	}

	// Parse chain_id
	if chainID, ok := raw["chain_id"].(string); ok {
		data.ChainID = chainID
	}

	// Parse tx_hash
	if txHash, ok := raw["tx_hash"].(string); ok {
		data.TxHash = txHash
	}

	// Parse block_number
	if blockNum, ok := raw["block_number"].(float64); ok {
		data.BlockNumber = int64(blockNum)
	} else if blockNum, ok := raw["block_number"].(int64); ok {
		data.BlockNumber = blockNum
	}

	// Parse contract_address
	if contractAddr, ok := raw["contract_address"].(string); ok {
		data.ContractAddress = contractAddr
	}

	// Parse occurred_at
	if occurredAtStr, ok := raw["occurred_at"].(string); ok {
		occurredAt, err := time.Parse(time.RFC3339, occurredAtStr)
		if err != nil {
			return nil, err
		}
		data.OccurredAt = occurredAt
	}

	// Parse unique_id
	if uniqueID, ok := raw["unique_id"].(string); ok {
		data.UniqueID = uniqueID
	}

	// Parse decimals (default to 18 for EVM tokens)
	data.Decimals = 18
	if decimals, ok := raw["decimals"].(float64); ok {
		data.Decimals = int(decimals)
	} else if decimals, ok := raw["decimals"].(int); ok {
		data.Decimals = decimals
	}

	return data, nil
}
