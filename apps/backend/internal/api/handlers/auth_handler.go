package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/kislikjeka/moontrack/internal/core/user/auth"
	"github.com/kislikjeka/moontrack/internal/core/user/domain"
	"github.com/kislikjeka/moontrack/internal/core/user/service"
)

// AuthHandler handles authentication-related HTTP requests
type AuthHandler struct {
	userService *service.UserService
	jwtService  *auth.JWTService
}

// NewAuthHandler creates a new auth handler
func NewAuthHandler(userService *service.UserService, jwtService *auth.JWTService) *AuthHandler {
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

// ErrorResponse represents an error response
type ErrorResponse struct {
	Error string `json:"error"`
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
	user, err := h.userService.Register(r.Context(), req.Email, req.Password)
	if err != nil {
		if err == domain.ErrUserAlreadyExists {
			respondError(w, "user with this email already exists", http.StatusConflict)
			return
		}
		if err == domain.ErrPasswordTooShort {
			respondError(w, "password must be at least 8 characters", http.StatusBadRequest)
			return
		}
		if err == domain.ErrInvalidEmail {
			respondError(w, "invalid email address", http.StatusBadRequest)
			return
		}
		respondError(w, "failed to register user", http.StatusInternalServerError)
		return
	}

	// Generate JWT token
	token, err := h.jwtService.GenerateToken(user.ID, user.Email)
	if err != nil {
		respondError(w, "failed to generate token", http.StatusInternalServerError)
		return
	}

	// Send response
	respondJSON(w, AuthResponse{
		Token: token,
		User: &UserInfo{
			ID:    user.ID.String(),
			Email: user.Email,
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
	user, err := h.userService.Login(r.Context(), req.Email, req.Password)
	if err != nil {
		if err == domain.ErrInvalidPassword {
			respondError(w, "invalid email or password", http.StatusUnauthorized)
			return
		}
		respondError(w, "failed to login", http.StatusInternalServerError)
		return
	}

	// Generate JWT token
	token, err := h.jwtService.GenerateToken(user.ID, user.Email)
	if err != nil {
		respondError(w, "failed to generate token", http.StatusInternalServerError)
		return
	}

	// Send response
	respondJSON(w, AuthResponse{
		Token: token,
		User: &UserInfo{
			ID:    user.ID.String(),
			Email: user.Email,
		},
	}, http.StatusOK)
}

// respondJSON sends a JSON response
func respondJSON(w http.ResponseWriter, data interface{}, statusCode int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	json.NewEncoder(w).Encode(data)
}

// respondError sends an error response
func respondError(w http.ResponseWriter, message string, statusCode int) {
	respondJSON(w, ErrorResponse{Error: message}, statusCode)
}
