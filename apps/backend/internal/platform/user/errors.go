package user

import "errors"

// User validation errors
var (
	ErrInvalidEmail        = errors.New("invalid email address")
	ErrInvalidPassword     = errors.New("invalid password")
	ErrInvalidPasswordHash = errors.New("invalid password hash")
	ErrPasswordTooShort    = errors.New("password must be at least 8 characters")
	ErrUserNotFound        = errors.New("user not found")
	ErrUserAlreadyExists   = errors.New("user with this email already exists")
	ErrUnauthorized        = errors.New("unauthorized")
)
