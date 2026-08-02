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
	"github.com/kislikjeka/moontrack/internal/platform/sync"
	"github.com/kislikjeka/moontrack/internal/platform/wallet"
	"github.com/kislikjeka/moontrack/pkg/logger"
)

// =============================================================================
// Fake registry
// =============================================================================

// fakeAssetRegistry is an in-memory stand-in for the asset registry. It mints a
// fresh UUID per unseen (chain, contract) and returns the same UUID for a key it
// has already seen — which is the whole behavioural contract the resolve relies
// on, and exactly what the table's UNIQUE (chain, contract) enforces in
// Postgres.
type fakeAssetRegistry struct {
	byKey map[sync.AssetKey]*sync.RegistryAsset
	calls []sync.AssetKey
}

func newFakeAssetRegistry() *fakeAssetRegistry {
	return &fakeAssetRegistry{byKey: map[sync.AssetKey]*sync.RegistryAsset{}}
}

func (f *fakeAssetRegistry) Resolve(ctx context.Context, key sync.AssetKey, symbol, name string, decimals int) (*sync.RegistryAsset, error) {
	f.calls = append(f.calls, key)
	if existing, ok := f.byKey[key]; ok {
		return existing, nil
	}
	a := &sync.RegistryAsset{
		ID:       uuid.New(),
		Key:      key,
		Symbol:   symbol,
		Name:     name,
		Decimals: decimals,
	}
	f.byKey[key] = a
	return a, nil
}

// idFor returns the UUID minted for a (chain, contract) pair, failing the test
// when the pair was never resolved.
func (f *fakeAssetRegistry) idFor(t *testing.T, chain, contract string) uuid.UUID {
	t.Helper()
	a, ok := f.byKey[sync.NewAssetKey(chain, contract)]
	require.True(t, ok, "expected %s:%s to have been resolved; resolved keys: %v", chain, contract, f.calls)
	return a.ID
}

var _ sync.AssetRegistry = (*fakeAssetRegistry)(nil)

// newRegistryTestBuilder wires a TxBuilder whose only configured dependency
// beyond the ledger is the registry, so nothing else can account for a resolve.
func newRegistryTestBuilder(t *testing.T, registry sync.AssetRegistry) (*sync.TxBuilder, *MockLedgerService) {
	t.Helper()
	log := logger.New("test", os.Stdout)
	walletRepo := new(MockWalletRepository)
	// Every counterparty in these fixtures is an outside address, so the
	// internal-transfer probe finds nothing. Identity resolution is what is
	// under test here; the classification path is incidental to it.
	walletRepo.On("GetWalletsByAddressAndUserID", mock.Anything, mock.Anything, mock.Anything).
		Return([]*wallet.Wallet{}, nil).Maybe()

	ledgerSvc := new(MockLedgerService)
	ledgerSvc.On("RecordTransaction", mock.Anything, mock.Anything, "noves",
		mock.Anything, mock.Anything, mock.Anything).
		Return(&ledger.Transaction{ID: uuid.New()}, nil)

	return sync.NewTxBuilder(walletRepo, ledgerSvc, nil, nil, log, nil, nil, registry), ledgerSvc
}

// =============================================================================
// AssetKey
// =============================================================================

func TestAssetKey_Normalization(t *testing.T) {
	tests := []struct {
		name     string
		chain    string
		contract string
		want     sync.AssetKey
	}{
		{
			name:     "contract casing is normalized",
			chain:    "base",
			contract: "0x833589FCD6EDB6E08F4C7C32D4F71B54BDA02913",
			want:     sync.AssetKey{Chain: "base", Contract: "0x833589fcd6edb6e08f4c7c32d4f71b54bda02913"},
		},
		{
			name:     "chain casing and whitespace are normalized",
			chain:    "  Base ",
			contract: " 0xABC ",
			want:     sync.AssetKey{Chain: "base", Contract: "0xabc"},
		},
		{
			// A leg that reaches the key with no contract at all is treated as
			// native rather than as a blank identity, so a provider that omits
			// the token on a native leg cannot mint an invalid key.
			name:     "an empty contract becomes the native sentinel",
			chain:    "ethereum",
			contract: "",
			want:     sync.AssetKey{Chain: "ethereum", Contract: sync.NativeContract},
		},
		{
			name:     "the sentinel survives normalization unchanged",
			chain:    "arbitrum",
			contract: sync.NativeContract,
			want:     sync.AssetKey{Chain: "arbitrum", Contract: sync.NativeContract},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, sync.NewAssetKey(tt.chain, tt.contract))
		})
	}
}

func TestAssetKey_IsNativeAndValid(t *testing.T) {
	native := sync.NewAssetKey("ethereum", sync.NativeContract)
	assert.True(t, native.IsNative())
	assert.True(t, native.Valid())

	token := sync.NewAssetKey("base", "0x833589fcd6edb6e08f4c7c32d4f71b54bda02913")
	assert.False(t, token.IsNative())
	assert.True(t, token.Valid())

	// A missing chain is not a usable identity — half a composite key.
	assert.False(t, sync.NewAssetKey("", sync.NativeContract).Valid())
}

// =============================================================================
// Acceptance test: same ticker, different contracts, one chain → two UUIDs
// =============================================================================

// TestTxBuilder_SameSymbolDifferentContracts_ResolveToDistinctUUIDs is the
// central claim of issue #56. Two legs sharing the ticker USDC on ONE chain but
// carrying different contracts are two different assets and must resolve to two
// different UUIDs.
//
// This is the hole in the store being replaced: chain_assets is UNIQUE (symbol,
// chain_id), so these two legs collapse into a single row there and the second
// upsert overwrites the first one's decimals — silently corrupting every
// base-unit conversion made against it. Identity keyed on (chain, contract)
// keeps them apart by construction.
func TestTxBuilder_SameSymbolDifferentContracts_ResolveToDistinctUUIDs(t *testing.T) {
	ctx := context.Background()
	registry := newFakeAssetRegistry()
	builder, _ := newRegistryTestBuilder(t, registry)

	walletAddr := "0x1111111111111111111111111111111111111111"
	w := newTestWallet(uuid.New(), walletAddr)

	// Circle's canonical USDC on Base, and a second contract claiming the very
	// same ticker on the very same chain.
	realUSDC := "0x833589fcd6edb6e08f4c7c32d4f71b54bda02913"
	impostorUSDC := "0xdead89fcd6edb6e08f4c7c32d4f71b54bda02913"

	tx := sync.DecodedTransaction{
		ID:            "base:0xsame-symbol-two-contracts",
		TxHash:        "0xsame-symbol-two-contracts",
		ChainID:       "base",
		OperationType: sync.OpTrade,
		Transfers: []sync.DecodedTransfer{
			{
				AssetSymbol:     "USDC",
				ContractAddress: realUSDC,
				Decimals:        6,
				Amount:          big.NewInt(1000000),
				Direction:       sync.DirectionOut,
				Sender:          walletAddr,
				Recipient:       "0xrouter",
			},
			{
				AssetSymbol:     "USDC",
				ContractAddress: impostorUSDC,
				Decimals:        18, // a different decimals — the value that gets clobbered today
				Amount:          big.NewInt(1000000000000000000),
				Direction:       sync.DirectionIn,
				Sender:          "0xrouter",
				Recipient:       walletAddr,
			},
		},
		MinedAt: time.Now(),
		Status:  "confirmed",
	}

	_, err := builder.ProcessTransaction(ctx, w, tx)
	require.NoError(t, err)

	realID := registry.idFor(t, "base", realUSDC)
	impostorID := registry.idFor(t, "base", impostorUSDC)

	assert.NotEqual(t, realID, impostorID,
		"two contracts sharing a ticker on one chain must be two assets")

	// The metadata must follow the contract, not the ticker: each row keeps its
	// own decimals, which is precisely what symbol-keyed uniqueness destroys.
	assert.Equal(t, 6, registry.byKey[sync.NewAssetKey("base", realUSDC)].Decimals)
	assert.Equal(t, 18, registry.byKey[sync.NewAssetKey("base", impostorUSDC)].Decimals)
}

// =============================================================================
// Acceptance test: one coin, two chains → two UUIDs
// =============================================================================

// TestTxBuilder_SameCoinAcrossChains_ResolveToDistinctUUIDs pins cross-chain
// splitting as INTENDED behaviour, not a defect. USDC on Base and USDC on
// Arbitrum are separate contracts on separate chains, so they are separate
// assets with separate UUIDs and separate tax lots.
//
// This follows from the composite key and was accepted when it was chosen
// (decision #35). Grouping one coin across chains, if it is ever wanted, belongs
// a level up via a shared coingecko_id — never by weakening the key.
func TestTxBuilder_SameCoinAcrossChains_ResolveToDistinctUUIDs(t *testing.T) {
	ctx := context.Background()
	registry := newFakeAssetRegistry()
	builder, _ := newRegistryTestBuilder(t, registry)

	walletAddr := "0x1111111111111111111111111111111111111111"
	w := newTestWallet(uuid.New(), walletAddr)

	baseUSDC := "0x833589fcd6edb6e08f4c7c32d4f71b54bda02913"
	arbUSDC := "0xaf88d065e77c8cc2239327c5edb3a432268e5831"

	// The same coin seen on two chains, in two separate transactions — which is
	// how it genuinely arrives, one chain being collected independently of the
	// other.
	for _, tc := range []struct {
		chain    string
		contract string
		extID    string
	}{
		{"base", baseUSDC, "base:0xusdc-on-base"},
		{"arbitrum", arbUSDC, "arbitrum:0xusdc-on-arbitrum"},
	} {
		tx := sync.DecodedTransaction{
			ID:            tc.extID,
			TxHash:        tc.extID,
			ChainID:       tc.chain,
			OperationType: sync.OpReceive,
			Transfers: []sync.DecodedTransfer{
				{
					AssetSymbol:     "USDC",
					ContractAddress: tc.contract,
					Decimals:        6,
					Amount:          big.NewInt(1000000),
					Direction:       sync.DirectionIn,
					Sender:          "0xsomeone",
					Recipient:       walletAddr,
				},
			},
			MinedAt: time.Now(),
			Status:  "confirmed",
		}
		_, err := builder.ProcessTransaction(ctx, w, tx)
		require.NoError(t, err)
	}

	assert.NotEqual(t,
		registry.idFor(t, "base", baseUSDC),
		registry.idFor(t, "arbitrum", arbUSDC),
		"one coin on two chains must be two assets")
}

// TestTxBuilder_SameNativeCoinAcrossChains_ResolveToDistinctUUIDs is the native
// half of the cross-chain claim. Every chain's native leg carries the identical
// sentinel, so uniqueness here rests entirely on the chain — if it did not, all
// native coins everywhere would collapse into one asset.
func TestTxBuilder_SameNativeCoinAcrossChains_ResolveToDistinctUUIDs(t *testing.T) {
	ctx := context.Background()
	registry := newFakeAssetRegistry()
	builder, _ := newRegistryTestBuilder(t, registry)

	walletAddr := "0x1111111111111111111111111111111111111111"
	w := newTestWallet(uuid.New(), walletAddr)

	for _, chain := range []string{"ethereum", "base", "arbitrum"} {
		tx := sync.DecodedTransaction{
			ID:            chain + ":0xnative-receive",
			TxHash:        "0xnative-receive",
			ChainID:       chain,
			OperationType: sync.OpReceive,
			Transfers: []sync.DecodedTransfer{
				{
					AssetSymbol:     "ETH",
					ContractAddress: sync.NativeContract,
					Decimals:        18,
					Amount:          big.NewInt(1000000000000000000),
					Direction:       sync.DirectionIn,
					Sender:          "0xsomeone",
					Recipient:       walletAddr,
				},
			},
			MinedAt: time.Now(),
			Status:  "confirmed",
		}
		_, err := builder.ProcessTransaction(ctx, w, tx)
		require.NoError(t, err)
	}

	ethID := registry.idFor(t, "ethereum", sync.NativeContract)
	baseID := registry.idFor(t, "base", sync.NativeContract)
	arbID := registry.idFor(t, "arbitrum", sync.NativeContract)

	assert.NotEqual(t, ethID, baseID)
	assert.NotEqual(t, baseID, arbID)
	assert.NotEqual(t, ethID, arbID)
}

// =============================================================================
// Acceptance test: every leg is resolved, natives included
// =============================================================================

// TestTxBuilder_ResolvesEveryLeg_Native is the regression guard on the defect
// issue #56 calls out by name. The legacy on-chain upsert ran only to feed the
// price-backfill job and returned early on an empty contract, so the native
// coin — usually a wallet's largest position — never acquired an on-chain
// identity at all. Under (chain, contract) identity the resolve is mandatory
// for EVERY leg, and a native leg is simply the leg whose contract is the
// sentinel.
func TestTxBuilder_ResolvesEveryLeg_Native(t *testing.T) {
	ctx := context.Background()
	registry := newFakeAssetRegistry()
	builder, _ := newRegistryTestBuilder(t, registry)

	walletAddr := "0x1111111111111111111111111111111111111111"
	w := newTestWallet(uuid.New(), walletAddr)
	usdc := "0x833589fcd6edb6e08f4c7c32d4f71b54bda02913"

	// A native leg swapped for a token, with a gas fee — also paid in native.
	tx := sync.DecodedTransaction{
		ID:            "base:0xnative-swap",
		TxHash:        "0xnative-swap",
		ChainID:       "base",
		OperationType: sync.OpTrade,
		Transfers: []sync.DecodedTransfer{
			{
				AssetSymbol:     "ETH",
				ContractAddress: sync.NativeContract,
				Decimals:        18,
				Amount:          big.NewInt(1000000000000000000),
				Direction:       sync.DirectionOut,
				Sender:          walletAddr,
				Recipient:       "0xrouter",
				// Priced by the provider: the resolve must not be conditional
				// on a missing price the way the legacy backfill upsert was.
				USDPrice: big.NewInt(250000000000),
			},
			{
				AssetSymbol:     "USDC",
				ContractAddress: usdc,
				Decimals:        6,
				Amount:          big.NewInt(2500000000),
				Direction:       sync.DirectionIn,
				Sender:          "0xrouter",
				Recipient:       walletAddr,
				USDPrice:        big.NewInt(100000000),
			},
		},
		Fee: &sync.DecodedFee{
			AssetSymbol: "ETH",
			Amount:      big.NewInt(21000),
			Decimals:    18,
		},
		MinedAt: time.Now(),
		Status:  "confirmed",
	}

	_, err := builder.ProcessTransaction(ctx, w, tx)
	require.NoError(t, err)

	// Both legs resolved, the native one included.
	nativeID := registry.idFor(t, "base", sync.NativeContract)
	usdcID := registry.idFor(t, "base", usdc)
	assert.NotEqual(t, nativeID, usdcID)

	// The fee is paid in the native coin, so it resolves to the SAME identity as
	// the native transfer leg rather than minting a second row.
	assert.Len(t, registry.byKey, 2, "expected exactly two identities: native + USDC")
}

// TestTxBuilder_ResolvesNativeOnlyTransaction covers the plain native transfer,
// where the native leg is the ONLY leg. Under the old early-return such a
// transaction resolved nothing whatsoever.
func TestTxBuilder_ResolvesNativeOnlyTransaction(t *testing.T) {
	ctx := context.Background()
	registry := newFakeAssetRegistry()
	builder, _ := newRegistryTestBuilder(t, registry)

	walletAddr := "0x1111111111111111111111111111111111111111"
	w := newTestWallet(uuid.New(), walletAddr)

	tx := sync.DecodedTransaction{
		ID:            "ethereum:0xplain-native-receive",
		TxHash:        "0xplain-native-receive",
		ChainID:       "ethereum",
		OperationType: sync.OpReceive,
		Transfers: []sync.DecodedTransfer{
			{
				AssetSymbol:     "ETH",
				ContractAddress: sync.NativeContract,
				Decimals:        18,
				Amount:          big.NewInt(500000000000000000),
				Direction:       sync.DirectionIn,
				Sender:          "0xsomeone",
				Recipient:       walletAddr,
			},
		},
		MinedAt: time.Now(),
		Status:  "confirmed",
	}

	_, err := builder.ProcessTransaction(ctx, w, tx)
	require.NoError(t, err)

	require.Len(t, registry.calls, 1, "the sole native leg must be resolved")
	assert.Equal(t, sync.NewAssetKey("ethereum", sync.NativeContract), registry.calls[0])
	assert.True(t, registry.calls[0].IsNative())
}

// TestTxBuilder_ResolveDedupesWithinTransaction verifies the resolve is called
// once per identity rather than once per leg. A swap routed through several hops
// repeats the same asset, and one resolve per identity is enough.
func TestTxBuilder_ResolveDedupesWithinTransaction(t *testing.T) {
	ctx := context.Background()
	registry := newFakeAssetRegistry()
	builder, _ := newRegistryTestBuilder(t, registry)

	walletAddr := "0x1111111111111111111111111111111111111111"
	w := newTestWallet(uuid.New(), walletAddr)
	usdc := "0x833589fcd6edb6e08f4c7c32d4f71b54bda02913"

	leg := func(dir sync.TransferDirection) sync.DecodedTransfer {
		return sync.DecodedTransfer{
			AssetSymbol:     "USDC",
			ContractAddress: usdc,
			Decimals:        6,
			Amount:          big.NewInt(1000000),
			Direction:       dir,
			Sender:          walletAddr,
			Recipient:       "0xrouter",
			USDPrice:        big.NewInt(100000000),
		}
	}

	tx := sync.DecodedTransaction{
		ID:            "base:0xmulti-hop",
		TxHash:        "0xmulti-hop",
		ChainID:       "base",
		OperationType: sync.OpTrade,
		Transfers:     []sync.DecodedTransfer{leg(sync.DirectionOut), leg(sync.DirectionIn), leg(sync.DirectionOut)},
		MinedAt:       time.Now(),
		Status:        "confirmed",
	}

	_, err := builder.ProcessTransaction(ctx, w, tx)
	require.NoError(t, err)

	assert.Len(t, registry.calls, 1, "one identity should resolve once, not once per leg")
}

// TestTxBuilder_MissingChain_IsNotResolved covers the one way a key can be
// invalid. NewAssetKey turns a missing contract into the sentinel, so only a
// missing chain can fail Valid() — a provider defect rather than a native leg,
// and one that must not mint a half-identity.
func TestTxBuilder_MissingChain_IsNotResolved(t *testing.T) {
	ctx := context.Background()
	registry := newFakeAssetRegistry()
	builder, _ := newRegistryTestBuilder(t, registry)

	walletAddr := "0x1111111111111111111111111111111111111111"
	w := newTestWallet(uuid.New(), walletAddr)

	tx := sync.DecodedTransaction{
		ID:            ":0xno-chain",
		TxHash:        "0xno-chain",
		ChainID:       "", // the provider told us nothing
		OperationType: sync.OpReceive,
		Transfers: []sync.DecodedTransfer{
			{
				AssetSymbol:     "ETH",
				ContractAddress: sync.NativeContract,
				Decimals:        18,
				Amount:          big.NewInt(1),
				Direction:       sync.DirectionIn,
				Sender:          "0xsomeone",
				Recipient:       walletAddr,
			},
		},
		MinedAt: time.Now(),
		Status:  "confirmed",
	}

	_, err := builder.ProcessTransaction(ctx, w, tx)
	require.NoError(t, err, "an unresolvable identity must not cost the transaction")
	assert.Empty(t, registry.calls, "half an identity must not reach the registry")
}

// TestTxBuilder_LegacyRawDataKeepsEmptyNativeContract pins that the sentinel
// does NOT leak into the ledger's raw_data.
//
// The sentinel is the vocabulary of the new registry. raw_data belongs to the
// pre-existing symbolic path, is round-tripped by platform/rawdata and surfaced
// by the transactions API, and #56 explicitly leaves that path alone — so a
// native leg must keep producing the empty contract it always produced. Without
// this the adapter change would silently alter an API payload.
func TestTxBuilder_LegacyRawDataKeepsEmptyNativeContract(t *testing.T) {
	ctx := context.Background()
	registry := newFakeAssetRegistry()
	builder, ledgerSvc := newRegistryTestBuilder(t, registry)

	walletAddr := "0x1111111111111111111111111111111111111111"
	w := newTestWallet(uuid.New(), walletAddr)

	tx := sync.DecodedTransaction{
		ID:            "ethereum:0xnative-in",
		TxHash:        "0xnative-in",
		ChainID:       "ethereum",
		OperationType: sync.OpReceive,
		Transfers: []sync.DecodedTransfer{
			{
				AssetSymbol:     "ETH",
				ContractAddress: sync.NativeContract,
				Decimals:        18,
				Amount:          big.NewInt(1000000000000000000),
				Direction:       sync.DirectionIn,
				Sender:          "0xsomeone",
				Recipient:       walletAddr,
			},
		},
		MinedAt: time.Now(),
		Status:  "confirmed",
	}

	_, err := builder.ProcessTransaction(ctx, w, tx)
	require.NoError(t, err)

	require.Len(t, ledgerSvc.recordedTransactions, 1)
	rawData := ledgerSvc.recordedTransactions[0].RawData

	assert.Equal(t, "", rawData["contract_address"],
		"the ledger payload keeps the legacy empty-string spelling of native")

	transfers, ok := rawData["transfers"].([]map[string]interface{})
	require.True(t, ok, "expected a transfers array")
	require.Len(t, transfers, 1)
	assert.Equal(t, "", transfers[0]["contract_address"],
		"per-leg raw_data keeps the legacy spelling too")

	// The identity still resolved — the legacy spelling is a translation at the
	// boundary, not a loss of the native identity.
	require.Len(t, registry.calls, 1)
	assert.True(t, registry.calls[0].IsNative())
}

// TestTxBuilder_NilRegistry_IsNoOp pins that a builder wired without a registry
// behaves exactly as it did before the registry existed. The expand phase adds a
// path beside the old one; it does not make the old one conditional on the new.
func TestTxBuilder_NilRegistry_IsNoOp(t *testing.T) {
	ctx := context.Background()
	builder, ledgerSvc := newRegistryTestBuilder(t, nil)

	walletAddr := "0x1111111111111111111111111111111111111111"
	w := newTestWallet(uuid.New(), walletAddr)

	tx := sync.DecodedTransaction{
		ID:            "base:0xno-registry",
		TxHash:        "0xno-registry",
		ChainID:       "base",
		OperationType: sync.OpReceive,
		Transfers: []sync.DecodedTransfer{
			{
				AssetSymbol:     "ETH",
				ContractAddress: sync.NativeContract,
				Decimals:        18,
				Amount:          big.NewInt(1000000000000000000),
				Direction:       sync.DirectionIn,
				Sender:          "0xsomeone",
				Recipient:       walletAddr,
			},
		},
		MinedAt: time.Now(),
		Status:  "confirmed",
	}

	_, err := builder.ProcessTransaction(ctx, w, tx)
	require.NoError(t, err)
	require.Len(t, ledgerSvc.recordedTransactions, 1, "the transaction must still be recorded")
}
