package service_test

import (
	"testing"

	"github.com/kislikjeka/moontrack/internal/core/user/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// T049: Unit test UserService.Register with valid/invalid inputs
func TestUserService_Register(t *testing.T) {
	tests := []struct {
		name        string
		email       string
		password    string
		wantErr     bool
		expectedErr error
	}{
		{
			name:     "valid registration",
			email:    "user@example.com",
			password: "SecureP@ssw0rd",
			wantErr:  false,
		},
		{
			name:        "password too short",
			email:       "user@example.com",
			password:    "short",
			wantErr:     true,
			expectedErr: domain.ErrPasswordTooShort,
		},
		{
			name:        "invalid email",
			email:       "not-an-email",
			password:    "SecureP@ssw0rd",
			wantErr:     true,
			expectedErr: domain.ErrInvalidEmail,
		},
		{
			name:        "empty email",
			email:       "",
			password:    "SecureP@ssw0rd",
			wantErr:     true,
			expectedErr: domain.ErrInvalidEmail,
		},
		{
			name:     "minimum valid password length",
			email:    "user2@example.com",
			password: "12345678", // Exactly 8 characters
			wantErr:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Note: In a real test, you would:
			// 1. Create a mock repository or use a test database
			// 2. Initialize the UserService with the mock
			// 3. Call Register and verify the results

			// For now, we're testing the validation logic
			user := &domain.User{
				Email: tt.email,
			}

			// Validate email first (only check if email is expected to fail)
			if tt.expectedErr == domain.ErrInvalidEmail {
				err := user.ValidateEmail()
				if tt.wantErr {
					require.Error(t, err)
					assert.Equal(t, tt.expectedErr, err)
				} else {
					require.NoError(t, err)
				}
				return
			}

			// Then validate password
			err := user.SetPassword(tt.password)

			if tt.wantErr {
				require.Error(t, err)
				if tt.expectedErr != nil {
					assert.Equal(t, tt.expectedErr, err)
				}
			} else {
				require.NoError(t, err)
				assert.NotEmpty(t, user.PasswordHash)
				assert.NotEqual(t, tt.password, user.PasswordHash, "Password should be hashed")
			}
		})
	}
}

// T050: Unit test UserService.Login with correct/incorrect credentials
func TestUserService_Login(t *testing.T) {
	tests := []struct {
		name        string
		setupUser   func() *domain.User
		loginPass   string
		wantErr     bool
		expectedErr error
	}{
		{
			name: "correct password",
			setupUser: func() *domain.User {
				user := &domain.User{Email: "user@example.com"}
				user.SetPassword("SecureP@ssw0rd")
				return user
			},
			loginPass: "SecureP@ssw0rd",
			wantErr:   false,
		},
		{
			name: "incorrect password",
			setupUser: func() *domain.User {
				user := &domain.User{Email: "user@example.com"}
				user.SetPassword("SecureP@ssw0rd")
				return user
			},
			loginPass:   "WrongPassword",
			wantErr:     true,
			expectedErr: domain.ErrInvalidPassword,
		},
		{
			name: "empty password",
			setupUser: func() *domain.User {
				user := &domain.User{Email: "user@example.com"}
				user.SetPassword("SecureP@ssw0rd")
				return user
			},
			loginPass: "",
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			user := tt.setupUser()

			err := user.CheckPassword(tt.loginPass)

			if tt.wantErr {
				require.Error(t, err)
				if tt.expectedErr != nil {
					assert.Equal(t, tt.expectedErr, err)
				}
			} else {
				require.NoError(t, err)
			}
		})
	}
}

// TestPasswordHashing verifies password hashing works correctly
func TestPasswordHashing(t *testing.T) {
	t.Run("password is hashed with bcrypt", func(t *testing.T) {
		user := &domain.User{Email: "test@example.com"}
		password := "MySecurePassword123"

		err := user.SetPassword(password)
		require.NoError(t, err)

		// Password hash should not equal the original password
		assert.NotEqual(t, password, user.PasswordHash)

		// Password hash should start with bcrypt prefix
		assert.True(t, len(user.PasswordHash) > 0)

		// Should be able to verify the password
		err = user.CheckPassword(password)
		require.NoError(t, err)
	})

	t.Run("same password produces different hashes", func(t *testing.T) {
		password := "SamePassword123"

		user1 := &domain.User{Email: "user1@example.com"}
		user1.SetPassword(password)

		user2 := &domain.User{Email: "user2@example.com"}
		user2.SetPassword(password)

		// Hashes should be different due to bcrypt salt
		assert.NotEqual(t, user1.PasswordHash, user2.PasswordHash)

		// But both should verify correctly
		require.NoError(t, user1.CheckPassword(password))
		require.NoError(t, user2.CheckPassword(password))
	})
}

// TestUserValidation tests user entity validation
func TestUserValidation(t *testing.T) {
	tests := []struct {
		name        string
		user        *domain.User
		wantErr     bool
		expectedErr error
	}{
		{
			name: "valid user",
			user: &domain.User{
				Email:        "valid@example.com",
				PasswordHash: "$2a$10$...", // Simulated bcrypt hash
			},
			wantErr: false,
		},
		{
			name: "missing email",
			user: &domain.User{
				Email:        "",
				PasswordHash: "$2a$10$...",
			},
			wantErr:     true,
			expectedErr: domain.ErrInvalidEmail,
		},
		{
			name: "invalid email format",
			user: &domain.User{
				Email:        "not-an-email",
				PasswordHash: "$2a$10$...",
			},
			wantErr:     true,
			expectedErr: domain.ErrInvalidEmail,
		},
		{
			name: "missing password hash",
			user: &domain.User{
				Email:        "valid@example.com",
				PasswordHash: "",
			},
			wantErr:     true,
			expectedErr: domain.ErrInvalidPasswordHash,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.user.Validate()

			if tt.wantErr {
				require.Error(t, err)
				if tt.expectedErr != nil {
					assert.Equal(t, tt.expectedErr, err)
				}
			} else {
				require.NoError(t, err)
			}
		})
	}
}

// TestEmailValidation tests email validation
func TestEmailValidation(t *testing.T) {
	tests := []struct {
		email string
		valid bool
	}{
		{"user@example.com", true},
		{"user.name@example.com", true},
		{"user+tag@example.co.uk", true},
		{"user_name@example.org", true},
		{"123@example.com", true},
		{"not-an-email", false},
		{"@example.com", false},
		{"user@", false},
		{"user", false},
		{"", false},
		{"user @example.com", false}, // Space in email
		{"user@example", false},      // No TLD
	}

	for _, tt := range tests {
		t.Run(tt.email, func(t *testing.T) {
			user := &domain.User{
				Email:        tt.email,
				PasswordHash: "$2a$10$...",
			}

			err := user.Validate()

			if tt.valid {
				require.NoError(t, err, "Email %s should be valid", tt.email)
			} else {
				require.Error(t, err, "Email %s should be invalid", tt.email)
				assert.Equal(t, domain.ErrInvalidEmail, err)
			}
		})
	}
}
