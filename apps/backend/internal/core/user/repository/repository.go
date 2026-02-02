package repository

import (
	"context"

	"github.com/google/uuid"
	"github.com/kislikjeka/moontrack/internal/core/user/domain"
)

// UserRepository defines the interface for user persistence operations
type UserRepository interface {
	// Create creates a new user
	Create(ctx context.Context, user *domain.User) error

	// GetByID retrieves a user by ID
	GetByID(ctx context.Context, id uuid.UUID) (*domain.User, error)

	// GetByEmail retrieves a user by email
	GetByEmail(ctx context.Context, email string) (*domain.User, error)

	// Update updates a user
	Update(ctx context.Context, user *domain.User) error

	// Delete deletes a user
	Delete(ctx context.Context, id uuid.UUID) error

	// Exists checks if a user with the given email exists
	Exists(ctx context.Context, email string) (bool, error)
}
