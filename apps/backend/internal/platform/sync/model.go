package sync

import (
	"math/big"
	"time"

	"github.com/google/uuid"
)

// SyncPhase represents the current phase of a wallet's sync process
type SyncPhase string

const (
	SyncPhaseIdle        SyncPhase = "idle"
	SyncPhaseCollecting  SyncPhase = "collecting"
	SyncPhaseReconciling SyncPhase = "reconciling"
	SyncPhaseProcessing  SyncPhase = "processing"
	SyncPhaseSynced      SyncPhase = "synced"
)

// ProcessingStatus represents the processing state of a raw transaction
type ProcessingStatus string

const (
	ProcessingStatusPending   ProcessingStatus = "pending"
	ProcessingStatusProcessed ProcessingStatus = "processed"
	ProcessingStatusSkipped   ProcessingStatus = "skipped"
	ProcessingStatusError     ProcessingStatus = "error"
)

// RawTransaction stores a raw transaction from Zerion before ledger processing
type RawTransaction struct {
	ID               uuid.UUID        `db:"id"`
	WalletID         uuid.UUID        `db:"wallet_id"`
	ZerionID         string           `db:"zerion_id"`
	TxHash           string           `db:"tx_hash"`
	ChainID          string           `db:"chain_id"`
	OperationType    string           `db:"operation_type"`
	MinedAt          time.Time        `db:"mined_at"`
	Status           string           `db:"status"`
	RawJSON          []byte           `db:"raw_json"`
	ProcessingStatus ProcessingStatus `db:"processing_status"`
	ProcessingError  *string          `db:"processing_error"`
	LedgerTxID       *uuid.UUID       `db:"ledger_tx_id"`
	IsSynthetic      bool             `db:"is_synthetic"`
	CreatedAt        time.Time        `db:"created_at"`
	ProcessedAt      *time.Time       `db:"processed_at"`
}

// AssetFlow tracks net inflows and outflows for a specific asset on a chain
type AssetFlow struct {
	ChainID         string
	AssetSymbol     string
	ContractAddress string
	Decimals        int
	Inflow          *big.Int
	Outflow         *big.Int
}

// NetFlow returns Inflow - Outflow
func (f *AssetFlow) NetFlow() *big.Int {
	return new(big.Int).Sub(f.Inflow, f.Outflow)
}

// OnChainPosition represents an on-chain token balance from Zerion Positions API
type OnChainPosition struct {
	ChainID         string
	AssetSymbol     string
	AssetName       string // Human-readable name, empty if unknown
	ContractAddress string
	Decimals        int
	Quantity        *big.Int
	USDPrice        *big.Int
	IconURL         string // Token icon URL, empty if unavailable
}
