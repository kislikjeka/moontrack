package sync_test

import (
	"context"
	"math/big"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/kislikjeka/moontrack/internal/ledger"
	"github.com/kislikjeka/moontrack/internal/platform/lpposition"
	"github.com/kislikjeka/moontrack/internal/platform/sync"
	"github.com/kislikjeka/moontrack/pkg/logger"
)

// =============================================================================
// Mock LP Position Service
// =============================================================================

type MockLPPositionService struct {
	mock.Mock
	positions map[uuid.UUID]*lpposition.LPPosition
}

func newMockLPPositionService() *MockLPPositionService {
	return &MockLPPositionService{
		positions: make(map[uuid.UUID]*lpposition.LPPosition),
	}
}

func (m *MockLPPositionService) FindOrCreate(ctx context.Context, userID, walletID uuid.UUID, chainID, protocol, nftTokenID, contractAddress string, token0, token1 lpposition.TokenInfo, openedAt time.Time) (*lpposition.LPPosition, error) {
	// Check if position already exists by NFT token ID
	for _, pos := range m.positions {
		if pos.NFTTokenID == nftTokenID && nftTokenID != "" {
			return pos, nil
		}
	}

	// Create new position
	pos := &lpposition.LPPosition{
		ID:              uuid.New(),
		UserID:          userID,
		WalletID:        walletID,
		ChainID:         chainID,
		Protocol:        protocol,
		NFTTokenID:      nftTokenID,
		ContractAddress: contractAddress,

		Token0Symbol:   token0.Symbol,
		Token1Symbol:   token1.Symbol,
		Token0Contract: token0.Contract,
		Token0Decimals: token0.Decimals,
		Token1Contract: token1.Contract,
		Token1Decimals: token1.Decimals,

		TotalDepositedUSD:    big.NewInt(0),
		TotalWithdrawnUSD:    big.NewInt(0),
		TotalClaimedFeesUSD:  big.NewInt(0),
		TotalDepositedToken0: big.NewInt(0),
		TotalDepositedToken1: big.NewInt(0),
		TotalWithdrawnToken0: big.NewInt(0),
		TotalWithdrawnToken1: big.NewInt(0),
		TotalClaimedToken0:   big.NewInt(0),
		TotalClaimedToken1:   big.NewInt(0),

		Status:    lpposition.StatusOpen,
		OpenedAt:  openedAt,
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}
	m.positions[pos.ID] = pos
	return pos, nil
}

func (m *MockLPPositionService) FindOpenByTokenPair(ctx context.Context, walletID uuid.UUID, chainID, protocol, token0, token1 string) (*lpposition.LPPosition, error) {
	for _, pos := range m.positions {
		if pos.WalletID == walletID && pos.ChainID == chainID && pos.Protocol == protocol && pos.Status == lpposition.StatusOpen {
			if (pos.Token0Symbol == token0 && pos.Token1Symbol == token1) || (pos.Token0Symbol == token1 && pos.Token1Symbol == token0) {
				return pos, nil
			}
		}
	}
	return nil, nil
}

func (m *MockLPPositionService) RecordDeposit(ctx context.Context, positionID uuid.UUID, token0Amt, token1Amt, usdValue *big.Int) error {
	pos := m.positions[positionID]
	if pos == nil {
		return nil
	}
	pos.TotalDepositedToken0.Add(pos.TotalDepositedToken0, token0Amt)
	pos.TotalDepositedToken1.Add(pos.TotalDepositedToken1, token1Amt)
	pos.TotalDepositedUSD.Add(pos.TotalDepositedUSD, usdValue)
	return nil
}

func (m *MockLPPositionService) RecordWithdraw(ctx context.Context, positionID uuid.UUID, token0Amt, token1Amt, usdValue *big.Int) error {
	pos := m.positions[positionID]
	if pos == nil {
		return nil
	}
	pos.TotalWithdrawnToken0.Add(pos.TotalWithdrawnToken0, token0Amt)
	pos.TotalWithdrawnToken1.Add(pos.TotalWithdrawnToken1, token1Amt)
	pos.TotalWithdrawnUSD.Add(pos.TotalWithdrawnUSD, usdValue)
	return nil
}

func (m *MockLPPositionService) RecordClaimFees(ctx context.Context, positionID uuid.UUID, token0Amt, token1Amt, usdValue *big.Int) error {
	pos := m.positions[positionID]
	if pos == nil {
		return nil
	}
	pos.TotalClaimedToken0.Add(pos.TotalClaimedToken0, token0Amt)
	pos.TotalClaimedToken1.Add(pos.TotalClaimedToken1, token1Amt)
	pos.TotalClaimedFeesUSD.Add(pos.TotalClaimedFeesUSD, usdValue)
	return nil
}

var _ sync.LPPositionService = (*MockLPPositionService)(nil)

// =============================================================================
// LP Lifecycle Test
// =============================================================================

func TestSync_LP_FullLifecycle(t *testing.T) {
	ctx := context.Background()
	userID := uuid.New()
	walletAddr := "0x1111111111111111111111111111111111111111"
	nftTokenID := "12345"

	walletRepo := new(MockWalletRepository)
	ledgerSvc := new(MockLedgerService)
	lpSvc := newMockLPPositionService()
	log := logger.New("test", os.Stdout)

	// Accept all LP transaction types
	ledgerSvc.On("RecordTransaction", ctx, mock.Anything, "zerion", mock.Anything, mock.Anything, mock.Anything).
		Return(&ledger.Transaction{ID: uuid.New()}, nil)

	processor := sync.NewZerionProcessor(walletRepo, ledgerSvc, lpSvc, nil, log)
	w := newTestWallet(userID, walletAddr)

	// ─── Step 1: LP Deposit ───────────────────────────────────────────────
	t.Run("deposit", func(t *testing.T) {
		tx := sync.DecodedTransaction{
			ID:            "zerion-deposit-1",
			TxHash:        "0xdepositabc",
			ChainID:       "ethereum",
			OperationType: sync.OpDeposit,
			Protocol:      "Uniswap V3",
			NFTTokenID:    nftTokenID,
			Transfers: []sync.DecodedTransfer{
				{
					AssetSymbol:     "ETH",
					ContractAddress: "",
					Decimals:        18,
					Amount:          big.NewInt(1e18), // 1 ETH
					Direction:       sync.DirectionOut,
					Sender:          walletAddr,
					Recipient:       "0xuniswappool",
					USDPrice:        big.NewInt(250000000000), // $2500
				},
				{
					AssetSymbol:     "USDC",
					ContractAddress: "0xusdc",
					Decimals:        6,
					Amount:          big.NewInt(2500000000), // 2500 USDC
					Direction:       sync.DirectionOut,
					Sender:          walletAddr,
					Recipient:       "0xuniswappool",
					USDPrice:        big.NewInt(100000000), // $1
				},
			},
			MinedAt: time.Date(2024, 6, 15, 12, 0, 0, 0, time.UTC),
			Status:  "confirmed",
		}

		err := processor.ProcessTransaction(ctx, w, tx)
		require.NoError(t, err)

		// Verify ledger recorded LP deposit
		require.Len(t, ledgerSvc.recordedTransactions, 1)
		assert.Equal(t, ledger.TxTypeLPDeposit, ledgerSvc.recordedTransactions[0].TxType)

		// Verify LP position was created with deposit amounts
		require.Len(t, lpSvc.positions, 1)
		var pos *lpposition.LPPosition
		for _, p := range lpSvc.positions {
			pos = p
		}
		assert.Equal(t, "ETH", pos.Token0Symbol)
		assert.Equal(t, "USDC", pos.Token1Symbol)
		assert.Equal(t, nftTokenID, pos.NFTTokenID)
		assert.Equal(t, lpposition.StatusOpen, pos.Status)
		assert.True(t, pos.TotalDepositedToken0.Cmp(big.NewInt(1e18)) == 0, "deposited token0 should be 1 ETH")
		assert.True(t, pos.TotalDepositedToken1.Cmp(big.NewInt(2500000000)) == 0, "deposited token1 should be 2500 USDC")
		assert.True(t, pos.TotalDepositedUSD.Sign() > 0, "deposited USD should be positive")
	})

	// ─── Step 2: Claim Fees ───────────────────────────────────────────────
	t.Run("claim_fees", func(t *testing.T) {
		tx := sync.DecodedTransaction{
			ID:            "zerion-claim-1",
			TxHash:        "0xclaim123",
			ChainID:       "ethereum",
			OperationType: sync.OpReceive,
			Protocol:      "Uniswap V3",
			NFTTokenID:    nftTokenID,
			Acts:          []string{"claim"},
			Transfers: []sync.DecodedTransfer{
				{
					AssetSymbol:     "ETH",
					ContractAddress: "",
					Decimals:        18,
					Amount:          big.NewInt(5e16), // 0.05 ETH
					Direction:       sync.DirectionIn,
					Sender:          "0xuniswappool",
					Recipient:       walletAddr,
					USDPrice:        big.NewInt(250000000000),
				},
				{
					AssetSymbol:     "USDC",
					ContractAddress: "0xusdc",
					Decimals:        6,
					Amount:          big.NewInt(125000000), // 125 USDC
					Direction:       sync.DirectionIn,
					Sender:          "0xuniswappool",
					Recipient:       walletAddr,
					USDPrice:        big.NewInt(100000000),
				},
			},
			MinedAt: time.Date(2024, 7, 1, 12, 0, 0, 0, time.UTC),
			Status:  "confirmed",
		}

		err := processor.ProcessTransaction(ctx, w, tx)
		require.NoError(t, err)

		// Verify ledger recorded claim fees
		assert.Equal(t, ledger.TxTypeLPClaimFees, ledgerSvc.recordedTransactions[len(ledgerSvc.recordedTransactions)-1].TxType)

		// Verify position updated with claimed amounts
		var pos *lpposition.LPPosition
		for _, p := range lpSvc.positions {
			pos = p
		}
		assert.True(t, pos.TotalClaimedToken0.Cmp(big.NewInt(5e16)) == 0, "claimed token0 should be 0.05 ETH")
		assert.True(t, pos.TotalClaimedToken1.Cmp(big.NewInt(125000000)) == 0, "claimed token1 should be 125 USDC")
		assert.True(t, pos.TotalClaimedFeesUSD.Sign() > 0, "claimed USD should be positive")
		assert.Equal(t, lpposition.StatusOpen, pos.Status, "position should still be open")
	})

	// ─── Step 3: Full Withdraw ────────────────────────────────────────────
	t.Run("full_withdraw", func(t *testing.T) {
		tx := sync.DecodedTransaction{
			ID:            "zerion-withdraw-1",
			TxHash:        "0xwithdraw456",
			ChainID:       "ethereum",
			OperationType: sync.OpWithdraw,
			Protocol:      "Uniswap V3",
			NFTTokenID:    nftTokenID,
			Transfers: []sync.DecodedTransfer{
				{
					AssetSymbol:     "ETH",
					ContractAddress: "",
					Decimals:        18,
					Amount:          big.NewInt(1e18), // 1 ETH (full amount)
					Direction:       sync.DirectionIn,
					Sender:          "0xuniswappool",
					Recipient:       walletAddr,
					USDPrice:        big.NewInt(300000000000), // $3000 (price went up)
				},
				{
					AssetSymbol:     "USDC",
					ContractAddress: "0xusdc",
					Decimals:        6,
					Amount:          big.NewInt(2500000000), // 2500 USDC (full amount)
					Direction:       sync.DirectionIn,
					Sender:          "0xuniswappool",
					Recipient:       walletAddr,
					USDPrice:        big.NewInt(100000000), // $1
				},
			},
			MinedAt: time.Date(2024, 8, 1, 12, 0, 0, 0, time.UTC),
			Status:  "confirmed",
		}

		err := processor.ProcessTransaction(ctx, w, tx)
		require.NoError(t, err)

		// Verify ledger recorded LP withdraw
		assert.Equal(t, ledger.TxTypeLPWithdraw, ledgerSvc.recordedTransactions[len(ledgerSvc.recordedTransactions)-1].TxType)

		// Verify position updated with withdrawn amounts
		var pos *lpposition.LPPosition
		for _, p := range lpSvc.positions {
			pos = p
		}
		assert.True(t, pos.TotalWithdrawnToken0.Cmp(big.NewInt(1e18)) == 0, "withdrawn token0 should be 1 ETH")
		assert.True(t, pos.TotalWithdrawnToken1.Cmp(big.NewInt(2500000000)) == 0, "withdrawn token1 should be 2500 USDC")
		assert.True(t, pos.TotalWithdrawnUSD.Sign() > 0, "withdrawn USD should be positive")
	})

	// ─── Step 4: Verify final aggregates ──────────────────────────────────
	t.Run("final_aggregates", func(t *testing.T) {
		require.Len(t, lpSvc.positions, 1, "should have exactly one position")

		var pos *lpposition.LPPosition
		for _, p := range lpSvc.positions {
			pos = p
		}

		// Remaining amounts should be zero (fully withdrawn)
		remaining0 := pos.RemainingToken0()
		remaining1 := pos.RemainingToken1()
		assert.Equal(t, 0, remaining0.Sign(), "remaining token0 should be zero")
		assert.Equal(t, 0, remaining1.Sign(), "remaining token1 should be zero")
		assert.True(t, pos.IsFullyWithdrawn(), "position should be fully withdrawn")

		// All 3 transactions recorded
		assert.Len(t, ledgerSvc.recordedTransactions, 3)
	})
}

func TestSync_LP_UniV3Mint_ClassifiesAsDeposit(t *testing.T) {
	ctx := context.Background()
	userID := uuid.New()
	walletAddr := "0x1111111111111111111111111111111111111111"

	walletRepo := new(MockWalletRepository)
	ledgerSvc := new(MockLedgerService)
	lpSvc := newMockLPPositionService()
	log := logger.New("test", os.Stdout)

	ledgerSvc.On("RecordTransaction", ctx, mock.Anything, "zerion", mock.Anything, mock.Anything, mock.Anything).
		Return(&ledger.Transaction{ID: uuid.New()}, nil)

	processor := sync.NewZerionProcessor(walletRepo, ledgerSvc, lpSvc, nil, log)
	w := newTestWallet(userID, walletAddr)

	// Mint operation on Uniswap V3 should classify as lp_deposit
	tx := sync.DecodedTransaction{
		ID:            "zerion-mint-1",
		TxHash:        "0xmint789",
		ChainID:       "ethereum",
		OperationType: sync.OpMint,
		Protocol:      "Uniswap V3",
		NFTTokenID:    "99999",
		Transfers: []sync.DecodedTransfer{
			{
				AssetSymbol: "WETH",
				Decimals:    18,
				Amount:      big.NewInt(5e17),
				Direction:   sync.DirectionOut,
				Sender:      walletAddr,
				Recipient:   "0xpool",
			},
		},
		MinedAt: time.Date(2024, 6, 1, 12, 0, 0, 0, time.UTC),
		Status:  "confirmed",
	}

	err := processor.ProcessTransaction(ctx, w, tx)
	require.NoError(t, err)

	require.Len(t, ledgerSvc.recordedTransactions, 1)
	assert.Equal(t, ledger.TxTypeLPDeposit, ledgerSvc.recordedTransactions[0].TxType)
	assert.Len(t, lpSvc.positions, 1)
}

func TestSync_LP_AaveDeposit_IsLendingSupply(t *testing.T) {
	ctx := context.Background()
	userID := uuid.New()
	walletAddr := "0x1111111111111111111111111111111111111111"

	walletRepo := new(MockWalletRepository)
	ledgerSvc := new(MockLedgerService)
	lpSvc := newMockLPPositionService()
	log := logger.New("test", os.Stdout)

	ledgerSvc.On("RecordTransaction", ctx, mock.Anything, "zerion", mock.Anything, mock.Anything, mock.Anything).
		Return(&ledger.Transaction{ID: uuid.New()}, nil)

	processor := sync.NewZerionProcessor(walletRepo, ledgerSvc, lpSvc, nil, log)
	w := newTestWallet(userID, walletAddr)

	// Aave deposit should be classified as lending_supply, not defi_deposit or lp_deposit
	tx := sync.DecodedTransaction{
		ID:            "zerion-aave-1",
		TxHash:        "0xaave123",
		ChainID:       "ethereum",
		OperationType: sync.OpDeposit,
		Protocol:      "Aave V3",
		Transfers: []sync.DecodedTransfer{
			{
				AssetSymbol: "ETH",
				Decimals:    18,
				Amount:      big.NewInt(1e18),
				Direction:   sync.DirectionOut,
				Sender:      walletAddr,
				Recipient:   "0xaavepool",
			},
		},
		MinedAt: time.Date(2024, 6, 1, 12, 0, 0, 0, time.UTC),
		Status:  "confirmed",
	}

	err := processor.ProcessTransaction(ctx, w, tx)
	require.NoError(t, err)

	require.Len(t, ledgerSvc.recordedTransactions, 1)
	assert.Equal(t, ledger.TxTypeLendingSupply, ledgerSvc.recordedTransactions[0].TxType)
	assert.Len(t, lpSvc.positions, 0, "no LP position should be created for AAVE lending")
}
