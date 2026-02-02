package handler

import (
	"context"
	"fmt"
	"sync"

	"github.com/kislikjeka/moontrack/internal/core/ledger/domain"
)

// Registry manages transaction handlers
// Per constitution Principle VI: Handler Registry Pattern
//
// This allows adding new transaction types without modifying the ledger core
type Registry struct {
	handlers map[string]Handler
	mu       sync.RWMutex
}

// NewRegistry creates a new handler registry
func NewRegistry() *Registry {
	return &Registry{
		handlers: make(map[string]Handler),
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
func (r *Registry) Get(transactionType string) (Handler, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	handler, exists := r.handlers[transactionType]
	if !exists {
		return nil, fmt.Errorf("no handler registered for transaction type: %s", transactionType)
	}

	return handler, nil
}

// Has checks if a handler is registered for the given transaction type
func (r *Registry) Has(transactionType string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()

	_, exists := r.handlers[transactionType]
	return exists
}

// Types returns all registered transaction types
func (r *Registry) Types() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	types := make([]string, 0, len(r.handlers))
	for t := range r.handlers {
		types = append(types, t)
	}
	return types
}

// Handle processes a transaction using the appropriate handler
// This is a convenience method that looks up the handler and calls it
func (r *Registry) Handle(ctx context.Context, transactionType string, data map[string]interface{}) ([]*domain.Entry, error) {
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
