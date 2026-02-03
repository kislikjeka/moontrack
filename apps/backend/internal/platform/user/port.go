package user

import (
	"context"

	"github.com/google/uuid"
)

// Repository defines the interface for user persistence operations
type Repository interface {
	// Create creates a new user
	Create(ctx context.Context, user *User) error

	// GetByID retrieves a user by ID
	GetByID(ctx context.Context, id uuid.UUID) (*User, error)

	// GetByEmail retrieves a user by email
	GetByEmail(ctx context.Context, email string) (*User, error)

	// Update updates a user
	Update(ctx context.Context, user *User) error

	// Delete deletes a user
	Delete(ctx context.Context, id uuid.UUID) error

	// Exists checks if a user with the given email exists
	Exists(ctx context.Context, email string) (bool, error)
}
