package handler

import (
	"context"

	"github.com/kislikjeka/moontrack/internal/core/ledger/domain"
)

// TransactionHandler defines the interface for transaction type handlers
// Per constitution Principle VI: Handler Registry Pattern
//
// Each transaction type (income, outcome, asset adjustment, etc.) implements this interface
// to generate ledger entries for the transaction.
//
// Type parameter T is the transaction-specific data structure (e.g., ManualIncomeTransaction)
type TransactionHandler[T any] interface {
	// Type returns the unique transaction type identifier
	// Examples: "manual_income", "manual_outcome", "asset_adjustment"
	Type() string

	// GenerateEntries generates ledger entries for the given transaction data
	// The entries MUST balance (SUM(debit) = SUM(credit))
	// This is where the double-entry accounting logic resides for each transaction type
	GenerateEntries(ctx context.Context, data T) ([]*domain.Entry, error)

	// Validate validates the transaction-specific data before generating entries
	// Returns an error if the data is invalid or incomplete
	Validate(ctx context.Context, data T) error
}

// Handler is a type-erased wrapper around TransactionHandler[T]
// This allows the registry to store handlers of different types
type Handler interface {
	// Type returns the unique transaction type identifier
	Type() string

	// Handle processes a transaction and generates entries
	// The data parameter is a map[string]interface{} that will be unmarshaled
	// into the transaction-specific type
	Handle(ctx context.Context, data map[string]interface{}) ([]*domain.Entry, error)

	// ValidateData validates the transaction data
	ValidateData(ctx context.Context, data map[string]interface{}) error
}

// BaseHandler provides common functionality for handlers
type BaseHandler struct {
	handlerType string
}

// NewBaseHandler creates a new base handler
func NewBaseHandler(handlerType string) BaseHandler {
	return BaseHandler{
		handlerType: handlerType,
	}
}

// Type returns the transaction type
func (h *BaseHandler) Type() string {
	return h.handlerType
}
