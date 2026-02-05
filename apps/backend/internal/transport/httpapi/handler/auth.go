package handler

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/google/uuid"
	"github.com/kislikjeka/moontrack/internal/platform/user"
)

// UserServiceInterface defines the interface for user operations needed by AuthHandler
type UserServiceInterface interface {
	Register(ctx context.Context, email, password string) (*user.User, error)
	Login(ctx context.Context, email, password string) (*user.User, error)
}

// JWTServiceInterface defines the interface for JWT operations
type JWTServiceInterface interface {
	GenerateToken(userID uuid.UUID, email string) (string, error)
}

// AuthHandler handles authentication-related HTTP requests
type AuthHandler struct {
	userService UserServiceInterface
	jwtService  JWTServiceInterface
}

// NewAuthHandler creates a new auth handler
func NewAuthHandler(userService UserServiceInterface, jwtService JWTServiceInterface) *AuthHandler {
	return &AuthHandler{
		userService: userService,
		jwtService:  jwtService,
	}
}

// RegisterRequest represents the registration request body
type RegisterRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

// LoginRequest represents the login request body
type LoginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

// AuthResponse represents the authentication response
type AuthResponse struct {
	Token string    `json:"token"`
	User  *UserInfo `json:"user"`
}

// UserInfo represents user information (without sensitive data)
type UserInfo struct {
	ID    string `json:"id"`
	Email string `json:"email"`
}

// Register handles user registration (POST /auth/register)
func (h *AuthHandler) Register(w http.ResponseWriter, r *http.Request) {
	// Parse request body
	var req RegisterRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, "invalid request body", http.StatusBadRequest)
		return
	}

	// Validate request
	if req.Email == "" {
		respondError(w, "email is required", http.StatusBadRequest)
		return
	}

	if req.Password == "" {
		respondError(w, "password is required", http.StatusBadRequest)
		return
	}

	// Register user
	registeredUser, err := h.userService.Register(r.Context(), req.Email, req.Password)
	if err != nil {
		if err == user.ErrUserAlreadyExists {
			respondError(w, "user with this email already exists", http.StatusConflict)
			return
		}
		if err == user.ErrPasswordTooShort {
			respondError(w, "password must be at least 8 characters", http.StatusBadRequest)
			return
		}
		if err == user.ErrInvalidEmail {
			respondError(w, "invalid email address", http.StatusBadRequest)
			return
		}
		respondError(w, "failed to register user", http.StatusInternalServerError)
		return
	}

	// Generate JWT token
	token, err := h.jwtService.GenerateToken(registeredUser.ID, registeredUser.Email)
	if err != nil {
		respondError(w, "failed to generate token", http.StatusInternalServerError)
		return
	}

	// Send response
	respondJSON(w, AuthResponse{
		Token: token,
		User: &UserInfo{
			ID:    registeredUser.ID.String(),
			Email: registeredUser.Email,
		},
	}, http.StatusCreated)
}

// Login handles user login (POST /auth/login)
func (h *AuthHandler) Login(w http.ResponseWriter, r *http.Request) {
	// Parse request body
	var req LoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, "invalid request body", http.StatusBadRequest)
		return
	}

	// Validate request
	if req.Email == "" {
		respondError(w, "email is required", http.StatusBadRequest)
		return
	}

	if req.Password == "" {
		respondError(w, "password is required", http.StatusBadRequest)
		return
	}

	// Authenticate user
	authenticatedUser, err := h.userService.Login(r.Context(), req.Email, req.Password)
	if err != nil {
		if err == user.ErrInvalidPassword {
			respondError(w, "invalid email or password", http.StatusUnauthorized)
			return
		}
		respondError(w, "failed to login", http.StatusInternalServerError)
		return
	}

	// Generate JWT token
	token, err := h.jwtService.GenerateToken(authenticatedUser.ID, authenticatedUser.Email)
	if err != nil {
		respondError(w, "failed to generate token", http.StatusInternalServerError)
		return
	}

	// Send response
	respondJSON(w, AuthResponse{
		Token: token,
		User: &UserInfo{
			ID:    authenticatedUser.ID.String(),
			Email: authenticatedUser.Email,
		},
	}, http.StatusOK)
}
