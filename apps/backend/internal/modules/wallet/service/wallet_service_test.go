package service_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/kislikjeka/moontrack/internal/modules/wallet/domain"
	"github.com/kislikjeka/moontrack/internal/modules/wallet/service"
)

// MockWalletRepository is a mock implementation of WalletRepository
type MockWalletRepository struct {
	mock.Mock
}

func (m *MockWalletRepository) Create(ctx context.Context, wallet *domain.Wallet) error {
	args := m.Called(ctx, wallet)
	return args.Error(0)
}

func (m *MockWalletRepository) GetByID(ctx context.Context, id uuid.UUID) (*domain.Wallet, error) {
	args := m.Called(ctx, id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.Wallet), args.Error(1)
}

func (m *MockWalletRepository) GetByUserID(ctx context.Context, userID uuid.UUID) ([]*domain.Wallet, error) {
	args := m.Called(ctx, userID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*domain.Wallet), args.Error(1)
}

func (m *MockWalletRepository) Update(ctx context.Context, wallet *domain.Wallet) error {
	args := m.Called(ctx, wallet)
	return args.Error(0)
}

func (m *MockWalletRepository) Delete(ctx context.Context, id uuid.UUID) error {
	args := m.Called(ctx, id)
	return args.Error(0)
}

func (m *MockWalletRepository) ExistsByUserAndName(ctx context.Context, userID uuid.UUID, name string) (bool, error) {
	args := m.Called(ctx, userID, name)
	return args.Bool(0), args.Error(1)
}

// TestWalletService_Create tests wallet creation
func TestWalletService_Create(t *testing.T) {
	ctx := context.Background()
	userID := uuid.New()

	tests := []struct {
		name           string
		wallet         *domain.Wallet
		setupMock      func(*MockWalletRepository)
		wantErr        bool
		expectedErrMsg string
	}{
		{
			name: "valid wallet creation",
			wallet: &domain.Wallet{
				UserID:  userID,
				Name:    "My Ethereum Wallet",
				ChainID: "ethereum",
			},
			setupMock: func(m *MockWalletRepository) {
				m.On("ExistsByUserAndName", ctx, userID, "My Ethereum Wallet").Return(false, nil)
				m.On("Create", ctx, mock.AnythingOfType("*domain.Wallet")).Return(nil)
			},
			wantErr: false,
		},
		{
			name: "duplicate wallet name",
			wallet: &domain.Wallet{
				UserID:  userID,
				Name:    "Existing Wallet",
				ChainID: "ethereum",
			},
			setupMock: func(m *MockWalletRepository) {
				m.On("ExistsByUserAndName", ctx, userID, "Existing Wallet").Return(true, nil)
			},
			wantErr:        true,
			expectedErrMsg: "wallet name already exists",
		},
		{
			name: "missing wallet name",
			wallet: &domain.Wallet{
				UserID:  userID,
				Name:    "",
				ChainID: "ethereum",
			},
			setupMock:      func(m *MockWalletRepository) {},
			wantErr:        true,
			expectedErrMsg: "wallet name is required",
		},
		{
			name: "invalid chain ID",
			wallet: &domain.Wallet{
				UserID:  userID,
				Name:    "Test Wallet",
				ChainID: "invalid-chain",
			},
			setupMock:      func(m *MockWalletRepository) {},
			wantErr:        true,
			expectedErrMsg: "invalid or unsupported chain ID",
		},
		{
			name: "missing user ID",
			wallet: &domain.Wallet{
				UserID:  uuid.Nil,
				Name:    "Test Wallet",
				ChainID: "ethereum",
			},
			setupMock:      func(m *MockWalletRepository) {},
			wantErr:        true,
			expectedErrMsg: "invalid user ID",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockRepo := new(MockWalletRepository)
			tt.setupMock(mockRepo)

			svc := service.NewWalletService(mockRepo)
			wallet, err := svc.Create(ctx, tt.wallet)

			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.expectedErrMsg)
				assert.Nil(t, wallet)
			} else {
				require.NoError(t, err)
				assert.NotNil(t, wallet)
				assert.NotEqual(t, uuid.Nil, wallet.ID)
				assert.Equal(t, tt.wallet.Name, wallet.Name)
				assert.Equal(t, tt.wallet.ChainID, wallet.ChainID)
			}

			mockRepo.AssertExpectations(t)
		})
	}
}

// TestWalletService_GetByID tests retrieving a wallet by ID
func TestWalletService_GetByID(t *testing.T) {
	ctx := context.Background()
	userID := uuid.New()
	walletID := uuid.New()
	otherUserID := uuid.New()

	tests := []struct {
		name           string
		walletID       uuid.UUID
		userID         uuid.UUID
		setupMock      func(*MockWalletRepository)
		wantErr        bool
		expectedErrMsg string
	}{
		{
			name:     "successful retrieval",
			walletID: walletID,
			userID:   userID,
			setupMock: func(m *MockWalletRepository) {
				m.On("GetByID", ctx, walletID).Return(&domain.Wallet{
					ID:      walletID,
					UserID:  userID,
					Name:    "Test Wallet",
					ChainID: "ethereum",
				}, nil)
			},
			wantErr: false,
		},
		{
			name:     "wallet not found",
			walletID: walletID,
			userID:   userID,
			setupMock: func(m *MockWalletRepository) {
				m.On("GetByID", ctx, walletID).Return(nil, domain.ErrWalletNotFound)
			},
			wantErr:        true,
			expectedErrMsg: "wallet not found",
		},
		{
			name:     "unauthorized access - different user",
			walletID: walletID,
			userID:   otherUserID,
			setupMock: func(m *MockWalletRepository) {
				m.On("GetByID", ctx, walletID).Return(&domain.Wallet{
					ID:      walletID,
					UserID:  userID,
					Name:    "Test Wallet",
					ChainID: "ethereum",
				}, nil)
			},
			wantErr:        true,
			expectedErrMsg: "unauthorized wallet access",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockRepo := new(MockWalletRepository)
			tt.setupMock(mockRepo)

			svc := service.NewWalletService(mockRepo)
			wallet, err := svc.GetByID(ctx, tt.walletID, tt.userID)

			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.expectedErrMsg)
			} else {
				require.NoError(t, err)
				assert.NotNil(t, wallet)
				assert.Equal(t, tt.walletID, wallet.ID)
				assert.Equal(t, tt.userID, wallet.UserID)
			}

			mockRepo.AssertExpectations(t)
		})
	}
}

// TestWalletService_List tests listing wallets for a user
func TestWalletService_List(t *testing.T) {
	ctx := context.Background()
	userID := uuid.New()

	tests := []struct {
		name      string
		userID    uuid.UUID
		setupMock func(*MockWalletRepository)
		wantCount int
		wantErr   bool
	}{
		{
			name:   "successful list with multiple wallets",
			userID: userID,
			setupMock: func(m *MockWalletRepository) {
				wallets := []*domain.Wallet{
					{ID: uuid.New(), UserID: userID, Name: "Wallet 1", ChainID: "ethereum"},
					{ID: uuid.New(), UserID: userID, Name: "Wallet 2", ChainID: "bitcoin"},
				}
				m.On("GetByUserID", ctx, userID).Return(wallets, nil)
			},
			wantCount: 2,
			wantErr:   false,
		},
		{
			name:   "empty list - no wallets",
			userID: userID,
			setupMock: func(m *MockWalletRepository) {
				m.On("GetByUserID", ctx, userID).Return([]*domain.Wallet{}, nil)
			},
			wantCount: 0,
			wantErr:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockRepo := new(MockWalletRepository)
			tt.setupMock(mockRepo)

			svc := service.NewWalletService(mockRepo)
			wallets, err := svc.List(ctx, tt.userID)

			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Len(t, wallets, tt.wantCount)
			}

			mockRepo.AssertExpectations(t)
		})
	}
}

// TestWalletService_Update tests wallet updates
func TestWalletService_Update(t *testing.T) {
	ctx := context.Background()
	userID := uuid.New()
	walletID := uuid.New()
	otherUserID := uuid.New()

	tests := []struct {
		name           string
		wallet         *domain.Wallet
		userID         uuid.UUID
		setupMock      func(*MockWalletRepository)
		wantErr        bool
		expectedErrMsg string
	}{
		{
			name: "successful update",
			wallet: &domain.Wallet{
				ID:      walletID,
				Name:    "Updated Name",
				ChainID: "ethereum",
			},
			userID: userID,
			setupMock: func(m *MockWalletRepository) {
				m.On("GetByID", ctx, walletID).Return(&domain.Wallet{
					ID:      walletID,
					UserID:  userID,
					Name:    "Old Name",
					ChainID: "ethereum",
				}, nil)
				m.On("ExistsByUserAndName", ctx, userID, "Updated Name").Return(false, nil)
				m.On("Update", ctx, mock.AnythingOfType("*domain.Wallet")).Return(nil)
			},
			wantErr: false,
		},
		{
			name: "unauthorized update - different user",
			wallet: &domain.Wallet{
				ID:      walletID,
				Name:    "Updated Name",
				ChainID: "ethereum",
			},
			userID: otherUserID,
			setupMock: func(m *MockWalletRepository) {
				m.On("GetByID", ctx, walletID).Return(&domain.Wallet{
					ID:      walletID,
					UserID:  userID,
					Name:    "Old Name",
					ChainID: "ethereum",
				}, nil)
			},
			wantErr:        true,
			expectedErrMsg: "unauthorized wallet access",
		},
		{
			name: "duplicate name conflict",
			wallet: &domain.Wallet{
				ID:      walletID,
				Name:    "Existing Wallet",
				ChainID: "ethereum",
			},
			userID: userID,
			setupMock: func(m *MockWalletRepository) {
				m.On("GetByID", ctx, walletID).Return(&domain.Wallet{
					ID:      walletID,
					UserID:  userID,
					Name:    "Old Name",
					ChainID: "ethereum",
				}, nil)
				m.On("ExistsByUserAndName", ctx, userID, "Existing Wallet").Return(true, nil)
			},
			wantErr:        true,
			expectedErrMsg: "wallet name already exists",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockRepo := new(MockWalletRepository)
			tt.setupMock(mockRepo)

			svc := service.NewWalletService(mockRepo)
			wallet, err := svc.Update(ctx, tt.wallet, tt.userID)

			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.expectedErrMsg)
			} else {
				require.NoError(t, err)
				assert.NotNil(t, wallet)
				assert.Equal(t, tt.wallet.Name, wallet.Name)
			}

			mockRepo.AssertExpectations(t)
		})
	}
}

// TestWalletService_Delete tests wallet deletion
func TestWalletService_Delete(t *testing.T) {
	ctx := context.Background()
	userID := uuid.New()
	walletID := uuid.New()
	otherUserID := uuid.New()

	tests := []struct {
		name           string
		walletID       uuid.UUID
		userID         uuid.UUID
		setupMock      func(*MockWalletRepository)
		wantErr        bool
		expectedErrMsg string
	}{
		{
			name:     "successful deletion",
			walletID: walletID,
			userID:   userID,
			setupMock: func(m *MockWalletRepository) {
				m.On("GetByID", ctx, walletID).Return(&domain.Wallet{
					ID:      walletID,
					UserID:  userID,
					Name:    "Test Wallet",
					ChainID: "ethereum",
				}, nil)
				m.On("Delete", ctx, walletID).Return(nil)
			},
			wantErr: false,
		},
		{
			name:     "unauthorized deletion - different user",
			walletID: walletID,
			userID:   otherUserID,
			setupMock: func(m *MockWalletRepository) {
				m.On("GetByID", ctx, walletID).Return(&domain.Wallet{
					ID:      walletID,
					UserID:  userID,
					Name:    "Test Wallet",
					ChainID: "ethereum",
				}, nil)
			},
			wantErr:        true,
			expectedErrMsg: "unauthorized wallet access",
		},
		{
			name:     "wallet not found",
			walletID: walletID,
			userID:   userID,
			setupMock: func(m *MockWalletRepository) {
				m.On("GetByID", ctx, walletID).Return(nil, domain.ErrWalletNotFound)
			},
			wantErr:        true,
			expectedErrMsg: "wallet not found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockRepo := new(MockWalletRepository)
			tt.setupMock(mockRepo)

			svc := service.NewWalletService(mockRepo)
			err := svc.Delete(ctx, tt.walletID, tt.userID)

			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.expectedErrMsg)
			} else {
				require.NoError(t, err)
			}

			mockRepo.AssertExpectations(t)
		})
	}
}
