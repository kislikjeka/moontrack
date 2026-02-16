package user

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/kislikjeka/moontrack/pkg/logger"
)

// Service handles user business logic
type Service struct {
	repo   Repository
	logger *logger.Logger
}

// NewService creates a new user service
func NewService(repo Repository, log *logger.Logger) *Service {
	return &Service{
		repo:   repo,
		logger: log.WithField("component", "user"),
	}
}

// Register registers a new user
// Returns the created user (without password hash exposed) and any error
func (s *Service) Register(ctx context.Context, email, password string) (*User, error) {
	// Validate email format
	if email == "" {
		return nil, ErrInvalidEmail
	}

	// Check if user already exists
	exists, err := s.repo.Exists(ctx, email)
	if err != nil {
		return nil, fmt.Errorf("failed to check if user exists: %w", err)
	}

	if exists {
		s.logger.Warn("registration attempt for existing email", "email", email)
		return nil, ErrUserAlreadyExists
	}

	// Create user
	user := &User{
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

	s.logger.Info("user registered", "user_id", user.ID)

	return user, nil
}

// Login authenticates a user with email and password
// Returns the user if authentication succeeds
func (s *Service) Login(ctx context.Context, email, password string) (*User, error) {
	// Get user by email
	user, err := s.repo.GetByEmail(ctx, email)
	if err != nil {
		if err == ErrUserNotFound {
			// Don't reveal that the user doesn't exist
			s.logger.Warn("login failed", "email", email)
			return nil, ErrInvalidPassword
		}
		return nil, fmt.Errorf("failed to get user: %w", err)
	}

	// Check password
	if err := user.CheckPassword(password); err != nil {
		s.logger.Warn("login failed", "email", email)
		return nil, err
	}

	// Update last login timestamp
	user.UpdateLastLogin()
	if err := s.repo.Update(ctx, user); err != nil {
		// Log error but don't fail login
		// This is a non-critical operation
		s.logger.Error("failed to update last login", "user_id", user.ID, "error", err)
	}

	s.logger.Info("user logged in", "user_id", user.ID)

	return user, nil
}

// GetByID retrieves a user by ID
func (s *Service) GetByID(ctx context.Context, id uuid.UUID) (*User, error) {
	return s.repo.GetByID(ctx, id)
}

// GetByEmail retrieves a user by email
func (s *Service) GetByEmail(ctx context.Context, email string) (*User, error) {
	return s.repo.GetByEmail(ctx, email)
}
