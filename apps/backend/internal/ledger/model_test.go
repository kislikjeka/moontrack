package ledger_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kislikjeka/moontrack/internal/ledger"
)

// =============================================================================
// Transaction Type Tests
// =============================================================================

func TestTransactionType_IsValid(t *testing.T) {
	validTypes := []ledger.TransactionType{
		ledger.TxTypeTransferIn,
		ledger.TxTypeTransferOut,
		ledger.TxTypeInternalTransfer,
		ledger.TxTypeManualIncome,
		ledger.TxTypeManualOutcome,
		ledger.TxTypeAssetAdjustment,
		ledger.TxTypeSwap,
		ledger.TxTypeDefiDeposit,
		ledger.TxTypeDefiWithdraw,
		ledger.TxTypeDefiClaim,
	}

	for _, tt := range validTypes {
		t.Run(string(tt), func(t *testing.T) {
			assert.True(t, tt.IsValid(), "expected %s to be valid", tt)
		})
	}

	// Invalid type
	invalidType := ledger.TransactionType("invalid_type")
	assert.False(t, invalidType.IsValid())
}

func TestTransactionType_Label(t *testing.T) {
	tests := []struct {
		txType ledger.TransactionType
		label  string
	}{
		{ledger.TxTypeTransferIn, "Transfer In"},
		{ledger.TxTypeTransferOut, "Transfer Out"},
		{ledger.TxTypeInternalTransfer, "Internal Transfer"},
		{ledger.TxTypeManualIncome, "Manual Income"},
		{ledger.TxTypeManualOutcome, "Manual Outcome"},
		{ledger.TxTypeAssetAdjustment, "Adjustment"},
		{ledger.TxTypeSwap, "Swap"},
		{ledger.TxTypeDefiDeposit, "DeFi Deposit"},
		{ledger.TxTypeDefiWithdraw, "DeFi Withdraw"},
		{ledger.TxTypeDefiClaim, "DeFi Claim"},
	}

	for _, tt := range tests {
		t.Run(string(tt.txType), func(t *testing.T) {
			assert.Equal(t, tt.label, tt.txType.Label())
		})
	}

	// Unknown type
	unknown := ledger.TransactionType("unknown")
	assert.Equal(t, "Unknown", unknown.Label())
}

func TestAllTransactionTypes(t *testing.T) {
	allTypes := ledger.AllTransactionTypes()

	// Should contain all 10 types
	require.Len(t, allTypes, 10)

	// Verify new DeFi types are included
	typeSet := make(map[ledger.TransactionType]bool)
	for _, tt := range allTypes {
		typeSet[tt] = true
	}

	assert.True(t, typeSet[ledger.TxTypeSwap], "AllTransactionTypes should include swap")
	assert.True(t, typeSet[ledger.TxTypeDefiDeposit], "AllTransactionTypes should include defi_deposit")
	assert.True(t, typeSet[ledger.TxTypeDefiWithdraw], "AllTransactionTypes should include defi_withdraw")
	assert.True(t, typeSet[ledger.TxTypeDefiClaim], "AllTransactionTypes should include defi_claim")
}

// =============================================================================
// Account Type Tests
// =============================================================================

func TestAccount_Validate_ClearingType(t *testing.T) {
	// CLEARING account should be valid without wallet ID
	account := &ledger.Account{
		ID:      uuid.New(),
		Code:    "clearing.swap.ETH",
		Type:    ledger.AccountTypeClearing,
		AssetID: "ETH",
	}

	err := account.Validate()
	assert.NoError(t, err)
}

func TestAccount_Validate_ClearingType_RejectsWalletID(t *testing.T) {
	// CLEARING account should NOT have a wallet ID
	walletID := uuid.New()
	account := &ledger.Account{
		ID:       uuid.New(),
		Code:     "clearing.swap.ETH",
		Type:     ledger.AccountTypeClearing,
		AssetID:  "ETH",
		WalletID: &walletID,
	}

	err := account.Validate()
	assert.ErrorIs(t, err, ledger.ErrNonWalletAccountCannotHaveWalletID)
}

func TestAccount_Validate_AllTypes(t *testing.T) {
	// Types that require wallet ID
	walletID := uuid.New()
	walletAccount := &ledger.Account{
		ID:       uuid.New(),
		Code:     "wallet.test.ETH",
		Type:     ledger.AccountTypeCryptoWallet,
		AssetID:  "ETH",
		WalletID: &walletID,
	}
	assert.NoError(t, walletAccount.Validate())

	// Types that should NOT have wallet ID
	nonWalletTypes := []ledger.AccountType{
		ledger.AccountTypeIncome,
		ledger.AccountTypeExpense,
		ledger.AccountTypeGasFee,
		ledger.AccountTypeClearing,
	}

	for _, at := range nonWalletTypes {
		t.Run(string(at), func(t *testing.T) {
			account := &ledger.Account{
				ID:      uuid.New(),
				Code:    "test." + string(at),
				Type:    at,
				AssetID: "ETH",
			}
			assert.NoError(t, account.Validate())
		})
	}
}

func TestAccount_Validate_InvalidType(t *testing.T) {
	account := &ledger.Account{
		ID:      uuid.New(),
		Code:    "test.invalid",
		Type:    ledger.AccountType("INVALID"),
		AssetID: "ETH",
	}

	err := account.Validate()
	assert.ErrorIs(t, err, ledger.ErrInvalidAccountType)
}
