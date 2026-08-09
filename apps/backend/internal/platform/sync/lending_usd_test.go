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
	"github.com/kislikjeka/moontrack/internal/platform/lendingposition"
	"github.com/kislikjeka/moontrack/internal/platform/sync"
	"github.com/kislikjeka/moontrack/internal/platform/wallet"
	"github.com/kislikjeka/moontrack/pkg/logger"
)

// mockLendingPositionService captures the usdValue passed to each Record* method
// so tests can assert the lending USD calculation applies the decimals divisor.
type mockLendingPositionService struct {
	lastUSD *big.Int
}

func (m *mockLendingPositionService) FindOrCreate(ctx context.Context, userID, walletID uuid.UUID, protocol, chainID string, openedAt time.Time) (*lendingposition.LendingPosition, error) {
	return &lendingposition.LendingPosition{ID: uuid.New(), UserID: userID, WalletID: walletID, Protocol: protocol, ChainID: chainID}, nil
}

func (m *mockLendingPositionService) RecordSupply(ctx context.Context, positionID uuid.UUID, assetID uuid.UUID, amount, usdValue *big.Int) error {
	m.lastUSD = usdValue
	return nil
}

func (m *mockLendingPositionService) RecordWithdraw(ctx context.Context, positionID uuid.UUID, assetID uuid.UUID, amount, usdValue *big.Int) error {
	m.lastUSD = usdValue
	return nil
}

func (m *mockLendingPositionService) RecordBorrow(ctx context.Context, positionID uuid.UUID, assetID uuid.UUID, amount, usdValue *big.Int) error {
	m.lastUSD = usdValue
	return nil
}

func (m *mockLendingPositionService) RecordRepay(ctx context.Context, positionID uuid.UUID, assetID uuid.UUID, amount, usdValue *big.Int) error {
	m.lastUSD = usdValue
	return nil
}

func (m *mockLendingPositionService) RecordClaim(ctx context.Context, positionID uuid.UUID, usdValue *big.Int) error {
	m.lastUSD = usdValue
	return nil
}

var _ sync.LendingPositionService = (*mockLendingPositionService)(nil)

// lendingTransfer builds a transfer of `whole` whole-units of an asset priced at
// `usdPrice` dollars, in the given direction. Amount is scaled by 10^decimals.
//
// The asset is the PRINCIPAL (plain USDC), not a receipt: since #57 a receipt
// leg never reaches the ledger, so a lending position is valued off the
// principal alone. What marks the transaction as a lending operation is the
// action on LegActions, which the caller sets — the ticker no longer carries
// that meaning, and neither does the leg itself (#76).
func lendingTransfer(dir sync.TransferDirection, decimals int, whole int64, usdPriceDollars int64) sync.DecodedTransfer {
	amount := new(big.Int).Mul(big.NewInt(whole), new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(decimals)), nil))
	return sync.DecodedTransfer{
		AssetSymbol:     "USDC",
		AssetName:       "USD Coin",
		ContractAddress: "0xdeadbeef",
		Decimals:        decimals,
		Amount:          amount,
		Direction:       dir,
		Sender:          "0x1111111111111111111111111111111111111111",
		Recipient:       "0x2222222222222222222222222222222222222222",
		USDPrice:        big.NewInt(usdPriceDollars * 1e8), // scaled by 1e8
	}
}

// TestLendingUSD_AppliesDecimalsDivisor is a regression test for the calcLendingUSD
// bug: USD value must be amount * usdPrice / 10^decimals, not amount * usdPrice.
// It drives every lending operation (supply/withdraw/borrow/repay/claim) — the five
// call sites of the lending-USD helper — through ProcessTransaction and asserts the
// USD value passed to the lending position service is divided by 10^decimals.
func TestLendingUSD_AppliesDecimalsDivisor(t *testing.T) {
	ctx := context.Background()
	log := logger.New("test", os.Stdout)

	// 100 whole units at $1 each = $100 = 100 * 1e8 (USD scaled by 1e8).
	// Buggy (no divisor): 100 * 10^decimals * (1 * 1e8) — inflated by 10^decimals.
	const decimals = 6
	const whole = int64(100)
	const priceDollars = int64(1)
	wantUSD := big.NewInt(100 * 1e8)

	tests := []struct {
		name   string
		opType sync.OperationType
		dir    sync.TransferDirection
		action string
		txType ledger.TransactionType
	}{
		{"supply", sync.OpDeposit, sync.DirectionOut, "deposited", ledger.TxTypeLendingSupply},
		{"withdraw", sync.OpWithdraw, sync.DirectionIn, "withdrawn", ledger.TxTypeLendingWithdraw},
		{"borrow", sync.OpReceive, sync.DirectionIn, "borrowed", ledger.TxTypeLendingBorrow},
		{"repay", sync.OpSend, sync.DirectionOut, "repaid", ledger.TxTypeLendingRepay},
		{"claim", sync.OpClaim, sync.DirectionIn, "withdrawn", ledger.TxTypeLendingClaim},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			userID := uuid.New()
			w := newTestWallet(userID, "0x1111111111111111111111111111111111111111")

			walletRepo := new(MockWalletRepository)
			walletRepo.On("GetWalletsByAddressAndUserID", ctx, mock.Anything, mock.Anything).
				Return([]*wallet.Wallet{}, nil).Maybe()

			ledgerSvc := new(MockLedgerService)
			ledgerSvc.On("RecordTransaction", ctx, tc.txType, "noves", mock.Anything, mock.Anything, mock.Anything).
				Return(&ledger.Transaction{ID: uuid.New()}, nil)

			lendingSvc := &mockLendingPositionService{}

			processor := sync.NewTxBuilder(walletRepo, ledgerSvc, nil, lendingSvc, log, nil, nil, nil)

			tx := sync.DecodedTransaction{
				ID:            "ext-tx-" + uuid.New().String()[:8],
				TxHash:        "0x" + uuid.New().String()[:32],
				ChainID:       "ethereum",
				OperationType: tc.opType,
				Protocol:      "AAVE",
				LegActions:    []string{tc.action},
				Transfers:     []sync.DecodedTransfer{lendingTransfer(tc.dir, decimals, whole, priceDollars)},
				MinedAt:       time.Now(),
				Status:        "confirmed",
			}

			_, err := processor.ProcessTransaction(ctx, w, tx)
			require.NoError(t, err)

			require.NotNil(t, lendingSvc.lastUSD, "lending position service was not called")
			assert.Equal(t, wantUSD.String(), lendingSvc.lastUSD.String(),
				"lending USD must apply the /10^decimals divisor (amount*price/10^decimals)")
		})
	}
}
