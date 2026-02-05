package ledger

import (
	"context"
	"fmt"
	"sync"
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
	// Examples: TxTypeManualIncome, TxTypeManualOutcome, TxTypeAssetAdjustment
	Type() TransactionType

	// GenerateEntries generates ledger entries for the given transaction data
	// The entries MUST balance (SUM(debit) = SUM(credit))
	// This is where the double-entry accounting logic resides for each transaction type
	GenerateEntries(ctx context.Context, data T) ([]*Entry, error)

	// Validate validates the transaction-specific data before generating entries
	// Returns an error if the data is invalid or incomplete
	Validate(ctx context.Context, data T) error
}

// Handler is a type-erased wrapper around TransactionHandler[T]
// This allows the registry to store handlers of different types
type Handler interface {
	// Type returns the unique transaction type identifier
	Type() TransactionType

	// Handle processes a transaction and generates entries
	// The data parameter is a map[string]interface{} that will be unmarshaled
	// into the transaction-specific type
	Handle(ctx context.Context, data map[string]interface{}) ([]*Entry, error)

	// ValidateData validates the transaction data
	ValidateData(ctx context.Context, data map[string]interface{}) error
}

// BaseHandler provides common functionality for handlers
type BaseHandler struct {
	handlerType TransactionType
}

// NewBaseHandler creates a new base handler
func NewBaseHandler(handlerType TransactionType) BaseHandler {
	return BaseHandler{
		handlerType: handlerType,
	}
}

// Type returns the transaction type
func (h *BaseHandler) Type() TransactionType {
	return h.handlerType
}

// Registry manages transaction handlers
// Per constitution Principle VI: Handler Registry Pattern
//
// This allows adding new transaction types without modifying the ledger core
type Registry struct {
	handlers map[TransactionType]Handler
	mu       sync.RWMutex
}

// NewRegistry creates a new handler registry
func NewRegistry() *Registry {
	return &Registry{
		handlers: make(map[TransactionType]Handler),
	}
}

// Register registers a handler for a transaction type
// Returns an error if a handler for this type is already registered
func (r *Registry) Register(handler Handler) error {
	if handler == nil {
		return fmt.Errorf("handler cannot be nil")
	}

	handlerType := handler.Type()
	if handlerType == "" {
		return fmt.Errorf("handler type cannot be empty")
	}

	if !handlerType.IsValid() {
		return fmt.Errorf("invalid handler type: %s", handlerType)
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.handlers[handlerType]; exists {
		return fmt.Errorf("handler for type '%s' already registered", handlerType)
	}

	r.handlers[handlerType] = handler
	return nil
}

// Get retrieves a handler by transaction type
// Returns an error if no handler is registered for this type
func (r *Registry) Get(transactionType TransactionType) (Handler, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	handler, exists := r.handlers[transactionType]
	if !exists {
		return nil, fmt.Errorf("no handler registered for transaction type: %s", transactionType)
	}

	return handler, nil
}

// Has checks if a handler is registered for the given transaction type
func (r *Registry) Has(transactionType TransactionType) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()

	_, exists := r.handlers[transactionType]
	return exists
}

// Types returns all registered transaction types
func (r *Registry) Types() []TransactionType {
	r.mu.RLock()
	defer r.mu.RUnlock()

	types := make([]TransactionType, 0, len(r.handlers))
	for t := range r.handlers {
		types = append(types, t)
	}
	return types
}

// Handle processes a transaction using the appropriate handler
// This is a convenience method that looks up the handler and calls it
func (r *Registry) Handle(ctx context.Context, transactionType TransactionType, data map[string]interface{}) ([]*Entry, error) {
	handler, err := r.Get(transactionType)
	if err != nil {
		return nil, err
	}

	// Validate the data first
	if err := handler.ValidateData(ctx, data); err != nil {
		return nil, fmt.Errorf("validation failed: %w", err)
	}

	// Generate entries
	entries, err := handler.Handle(ctx, data)
	if err != nil {
		return nil, fmt.Errorf("failed to generate entries: %w", err)
	}

	return entries, nil
}
