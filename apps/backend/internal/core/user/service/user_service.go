package service

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/kislikjeka/moontrack/internal/core/user/domain"
	"github.com/kislikjeka/moontrack/internal/core/user/repository"
)

// UserService handles user business logic
type UserService struct {
	repo repository.UserRepository
}

// NewUserService creates a new user service
func NewUserService(repo repository.UserRepository) *UserService {
	return &UserService{
		repo: repo,
	}
}

// Register registers a new user
// Returns the created user (without password hash exposed) and any error
func (s *UserService) Register(ctx context.Context, email, password string) (*domain.User, error) {
	// Validate email format
	if email == "" {
		return nil, domain.ErrInvalidEmail
	}

	// Check if user already exists
	exists, err := s.repo.Exists(ctx, email)
	if err != nil {
		return nil, fmt.Errorf("failed to check if user exists: %w", err)
	}

	if exists {
		return nil, domain.ErrUserAlreadyExists
	}

	// Create user
	user := &domain.User{
		ID:        uuid.New(),
		Email:     email,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	// Hash password
	if err := user.SetPassword(password); err != nil {
		return nil, err
	}

	// Save to database
	if err := s.repo.Create(ctx, user); err != nil {
		return nil, fmt.Errorf("failed to create user: %w", err)
	}

	return user, nil
}

// Login authenticates a user with email and password
// Returns the user if authentication succeeds
func (s *UserService) Login(ctx context.Context, email, password string) (*domain.User, error) {
	// Get user by email
	user, err := s.repo.GetByEmail(ctx, email)
	if err != nil {
		if err == domain.ErrUserNotFound {
			// Don't reveal that the user doesn't exist
			return nil, domain.ErrInvalidPassword
		}
		return nil, fmt.Errorf("failed to get user: %w", err)
	}

	// Check password
	if err := user.CheckPassword(password); err != nil {
		return nil, err
	}

	// Update last login timestamp
	user.UpdateLastLogin()
	if err := s.repo.Update(ctx, user); err != nil {
		// Log error but don't fail login
		// This is a non-critical operation
		fmt.Printf("warning: failed to update last login: %v\n", err)
	}

	return user, nil
}

// GetByID retrieves a user by ID
func (s *UserService) GetByID(ctx context.Context, id uuid.UUID) (*domain.User, error) {
	return s.repo.GetByID(ctx, id)
}

// GetByEmail retrieves a user by email
func (s *UserService) GetByEmail(ctx context.Context, email string) (*domain.User, error) {
	return s.repo.GetByEmail(ctx, email)
}
