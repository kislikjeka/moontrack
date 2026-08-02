package sync

import (
	"math/big"
	"time"

	"github.com/google/uuid"
)

// sourceName is the ledger `source` tag stamped on every transaction recorded by
// the sync pipeline. The provider is Noves; the value is kept in lockstep with
// the `wipe_wallet_ledger` function and the data-clearing migration (000028),
// which TRUNCATE the old 'zerion' rows and re-sync under 'noves'.
const sourceName = "noves"

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

// RawTransaction stores a raw transaction from the sync provider before ledger processing
type RawTransaction struct {
	ID               uuid.UUID        `db:"id"`
	WalletID         uuid.UUID        `db:"wallet_id"`
	ExternalID       string           `db:"external_id"`
	TxHash           string           `db:"tx_hash"`
	ChainID          string           `db:"chain_id"`
	OperationType    string           `db:"operation_type"`
	MinedAt          time.Time        `db:"mined_at"`
	Status           string           `db:"status"`
	RawJSON          []byte           `db:"raw_json"`
	ProcessingStatus ProcessingStatus `db:"processing_status"`
	ProcessingError  *string          `db:"processing_error"`
	LedgerTxID       *uuid.UUID       `db:"ledger_tx_id"`
	// IsSynthetic marked a raw the Reconciler fabricated rather than collected
	// from the provider — only ever genesis balances, which sync no longer
	// produces (issue #53). Nothing sets it now, so it is always false on new
	// rows, but the NOT NULL column still exists and pre-change rows still carry
	// true, so the field stays to keep insert and scan aligned with the table.
	// It goes away with the column, in the clean-slate TRUNCATE (issue #40).
	IsSynthetic bool       `db:"is_synthetic"`
	CreatedAt   time.Time  `db:"created_at"`
	ProcessedAt *time.Time `db:"processed_at"`
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

// OnChainPosition represents an on-chain token balance from the sync provider positions API
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
