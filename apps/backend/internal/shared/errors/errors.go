package errors

import (
	"errors"
	"fmt"
)

// AppError represents an application error with additional context
type AppError struct {
	Code    string // Error code for client
	Message string // Human-readable message
	Err     error  // Underlying error
}

// Error implements the error interface
func (e *AppError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("%s: %s: %v", e.Code, e.Message, e.Err)
	}
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

// Unwrap returns the underlying error
func (e *AppError) Unwrap() error {
	return e.Err
}

// Common error codes
const (
	ErrCodeValidation      = "VALIDATION_ERROR"
	ErrCodeNotFound        = "NOT_FOUND"
	ErrCodeUnauthorized    = "UNAUTHORIZED"
	ErrCodeForbidden       = "FORBIDDEN"
	ErrCodeConflict        = "CONFLICT"
	ErrCodeInternal        = "INTERNAL_ERROR"
	ErrCodeBadRequest      = "BAD_REQUEST"
	ErrCodeDatabaseError   = "DATABASE_ERROR"
	ErrCodeInvalidInput    = "INVALID_INPUT"
	ErrCodeInsufficientBalance = "INSUFFICIENT_BALANCE"
	ErrCodeLedgerUnbalanced = "LEDGER_UNBALANCED"
)

// New creates a new AppError
func New(code, message string) *AppError {
	return &AppError{
		Code:    code,
		Message: message,
	}
}

// Wrap wraps an error with additional context
func Wrap(err error, code, message string) *AppError {
	return &AppError{
		Code:    code,
		Message: message,
		Err:     err,
	}
}

// Validation creates a validation error
func Validation(message string) *AppError {
	return &AppError{
		Code:    ErrCodeValidation,
		Message: message,
	}
}

// NotFound creates a not found error
func NotFound(resource string) *AppError {
	return &AppError{
		Code:    ErrCodeNotFound,
		Message: fmt.Sprintf("%s not found", resource),
	}
}

// Unauthorized creates an unauthorized error
func Unauthorized(message string) *AppError {
	return &AppError{
		Code:    ErrCodeUnauthorized,
		Message: message,
	}
}

// Forbidden creates a forbidden error
func Forbidden(message string) *AppError {
	return &AppError{
		Code:    ErrCodeForbidden,
		Message: message,
	}
}

// Conflict creates a conflict error
func Conflict(message string) *AppError {
	return &AppError{
		Code:    ErrCodeConflict,
		Message: message,
	}
}

// Internal creates an internal error
func Internal(message string, err error) *AppError {
	return &AppError{
		Code:    ErrCodeInternal,
		Message: message,
		Err:     err,
	}
}

// BadRequest creates a bad request error
func BadRequest(message string) *AppError {
	return &AppError{
		Code:    ErrCodeBadRequest,
		Message: message,
	}
}

// DatabaseError creates a database error
func DatabaseError(message string, err error) *AppError {
	return &AppError{
		Code:    ErrCodeDatabaseError,
		Message: message,
		Err:     err,
	}
}

// InvalidInput creates an invalid input error
func InvalidInput(message string) *AppError {
	return &AppError{
		Code:    ErrCodeInvalidInput,
		Message: message,
	}
}

// InsufficientBalance creates an insufficient balance error
func InsufficientBalance(message string) *AppError {
	return &AppError{
		Code:    ErrCodeInsufficientBalance,
		Message: message,
	}
}

// LedgerUnbalanced creates a ledger unbalanced error
func LedgerUnbalanced(message string) *AppError {
	return &AppError{
		Code:    ErrCodeLedgerUnbalanced,
		Message: message,
	}
}

// IsAppError checks if an error is an AppError
func IsAppError(err error) bool {
	var appErr *AppError
	return errors.As(err, &appErr)
}

// GetAppError extracts an AppError from an error
func GetAppError(err error) *AppError {
	var appErr *AppError
	if errors.As(err, &appErr) {
		return appErr
	}
	return nil
}
