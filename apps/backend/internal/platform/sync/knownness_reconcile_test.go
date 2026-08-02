package sync_test

import (
	"context"
	"math/big"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/google/uuid"
	"github.com/kislikjeka/moontrack/internal/platform/wallet"

	pkgsync "github.com/kislikjeka/moontrack/internal/platform/sync"
	"github.com/kislikjeka/moontrack/pkg/logger"
)

// =============================================================================
// APPLICATION POINT 2: the reconciliation input (acceptance criteria 4 and 6)
//
// The decision calls this point mandatory and gives the reason from live data:
// a filter on the transaction stream ALONE leaves spam able to re-enter through
// reconciliation, because a spam position has no matching flow and therefore
// reads as a full-size unexplained delta.
// =============================================================================

// newReconcilerWithFilter wires a Reconciler whose position provider returns the
// given positions on a single chain, with the known-asset filter attached.
func newReconcilerWithFilter(
	t *testing.T,
	reg pkgsync.KnownnessRegistry,
	positions []pkgsync.OnChainPosition,
) (*pkgsync.Reconciler, *MockWalletRepository, *wallet.Wallet) {
	t.Helper()
	log := logger.New("test", os.Stdout)

	w := newTestWallet(uuid.New(), "0x9afc000000000000000000000000000000000000")

	rawTxRepo := new(MockRawTransactionRepository)
	// No collected history at all: every position is therefore a full-size
	// unexplained delta, which is the worst case for the filter and exactly the
	// shape a spam position takes.
	rawTxRepo.On("GetAllByWallet", mock.Anything, w.ID).
		Return([]*pkgsync.RawTransaction{}, nil)

	posProvider := new(MockPositionDataProvider)
	posProvider.On("GetPositions", mock.Anything, w.Address, "base").
		Return(positions, nil)

	walletRepo := new(MockWalletRepository)
	walletRepo.On("SetSyncPhase", mock.Anything, w.ID, mock.Anything).Return(nil)
	walletRepo.On("GetChainSyncRows", mock.Anything, w.ID).
		Return([]wallet.WalletChainSync{{Chain: "base"}}, nil)
	walletRepo.On("SetChainSyncError", mock.Anything, w.ID, mock.Anything, mock.Anything).Return(nil).Maybe()

	r := pkgsync.NewReconciler(rawTxRepo, posProvider, walletRepo,
		pkgsync.NewKnownAssetFilter(reg), log)
	return r, walletRepo, w
}

// TestReconcile_UnknownPosition_ExcludedFromDeltaButCounted is the port
// statement of application point 2.
//
// A spam position must not flag the chain — otherwise every genuine discrepancy
// is buried under noise the transaction filter already rejected — but it must
// still be COUNTED AND CARRIED, because "filter silently" was rejected in the
// decision: silence destroys the very information the reconciliation report
// (#61) uses to tell spam from a migration bug.
func TestReconcile_UnknownPosition_ExcludedFromDeltaButCounted(t *testing.T) {
	ctx := context.Background()
	reg := newFakeKnownnessRegistry()

	spamKey := pkgsync.NewAssetKey("base", spamUSDCBase)
	reg.setStatus(spamKey, pkgsync.KnownnessUnknown, pkgsync.KnownnessSourceQuotable)

	// The real measured case: a homoglyph USDC position of 6525.81, the exact
	// balance that produced a synthetic genesis in the live database.
	positions := []pkgsync.OnChainPosition{{
		ChainID:         "base",
		AssetSymbol:     "UЅDС",
		ContractAddress: spamUSDCBase,
		Decimals:        6,
		Quantity:        big.NewInt(6_525_810_000),
	}}

	r, walletRepo, w := newReconcilerWithFilter(t, reg, positions)

	res, err := r.Reconcile(ctx, w)
	require.NoError(t, err)

	// Not in the delta …
	assert.Zero(t, res.Flagged, "an unknown position may not flag the chain")
	assert.Zero(t, res.PositionsChecked, "and it is not compared at all")
	walletRepo.AssertNotCalled(t, "SetChainSyncError", mock.Anything, mock.Anything, mock.Anything, mock.Anything)

	// … but counted and carried, with the reason attached.
	assert.Equal(t, 1, res.SkippedUnknown, "counted as terminally unknown")
	assert.Zero(t, res.SkippedPending)
	require.Len(t, res.Excluded, 1, "the excluded position must be available to the report")

	ex := res.Excluded[0]
	assert.Equal(t, "base", ex.ChainID)
	assert.Equal(t, spamUSDCBase, ex.Contract)
	assert.Equal(t, "6525810000", ex.Quantity.String(), "the quantity is preserved for the report")
	assert.True(t, ex.Checked, "checked, and the answer was no")
}

// TestReconcile_PendingPosition_CountedSeparatelyFromConvicted keeps the two
// kinds of exclusion apart at the reconciliation boundary. A position whose
// asset is merely unchecked is not spam, and a report that cannot tell the two
// apart cannot distinguish spam from a migration bug.
func TestReconcile_PendingPosition_CountedSeparatelyFromConvicted(t *testing.T) {
	ctx := context.Background()
	reg := newFakeKnownnessRegistry()

	convicted := pkgsync.NewAssetKey("base", spamUSDCBase)
	reg.setStatus(convicted, pkgsync.KnownnessUnknown, pkgsync.KnownnessSourceQuotable)

	pending := pkgsync.NewAssetKey("base", debtTokenBase)
	reg.setStatus(pending, pkgsync.KnownnessPending, "")

	positions := []pkgsync.OnChainPosition{
		{ChainID: "base", AssetSymbol: "UЅDС", ContractAddress: spamUSDCBase, Decimals: 6, Quantity: big.NewInt(6_525_810_000)},
		{ChainID: "base", AssetSymbol: "variableDebtBasUSDC", ContractAddress: debtTokenBase, Decimals: 8, Quantity: big.NewInt(5_000_000)},
	}

	r, _, w := newReconcilerWithFilter(t, reg, positions)

	res, err := r.Reconcile(ctx, w)
	require.NoError(t, err)

	assert.Equal(t, 1, res.SkippedUnknown, "one convicted")
	assert.Equal(t, 1, res.SkippedPending, "one merely unchecked")
	require.Len(t, res.Excluded, 2)

	byContract := map[string]pkgsync.ExcludedPosition{}
	for _, ex := range res.Excluded {
		byContract[ex.Contract] = ex
	}
	assert.True(t, byContract[spamUSDCBase].Checked, "spam: checked, unknown")
	assert.False(t, byContract[debtTokenBase].Checked, "queued: could not check yet")
}

// TestReconcile_KnownPosition_StillReconciled guards the other direction: the
// filter must not quietly shrink what reconciliation covers. A real coin with an
// unexplained balance still has to flag its chain, or the filter would hide the
// very defects reconciliation exists to surface.
func TestReconcile_KnownPosition_StillReconciled(t *testing.T) {
	ctx := context.Background()
	reg := newFakeKnownnessRegistry()

	// Real USDC — level 1 knows it, no registry row needed.
	positions := []pkgsync.OnChainPosition{{
		ChainID:         "base",
		AssetSymbol:     "USDC",
		ContractAddress: realUSDCBase,
		Decimals:        6,
		Quantity:        big.NewInt(2_800_000_000),
	}}

	r, walletRepo, w := newReconcilerWithFilter(t, reg, positions)

	res, err := r.Reconcile(ctx, w)
	require.NoError(t, err)

	assert.Equal(t, 1, res.PositionsChecked, "a known position is compared")
	assert.Equal(t, 1, res.Flagged, "and its unexplained balance still flags the chain")
	assert.Empty(t, res.Excluded)
	walletRepo.AssertCalled(t, "SetChainSyncError", mock.Anything, w.ID, "base", mock.Anything)
}
