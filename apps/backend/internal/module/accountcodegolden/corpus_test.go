// Package accountcodegolden holds a golden-file regression net over the shape
// of ledger account codes.
//
// Account codes are built by hand with fmt.Sprintf in ~10 production files
// (see issue #36). Before that construction is centralised into a typed
// constructor (#55), this test freezes the exact set of codes the current
// production handlers emit for a controlled corpus of inputs. The
// centralisation is then required to leave testdata/account_codes.golden
// byte-for-byte identical.
//
// The package holds tests only — it deliberately contains no production code,
// so that it can import every handler module at once without adding an edge
// to the production dependency graph.
package accountcodegolden

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/kislikjeka/moontrack/internal/ledger"
	"github.com/kislikjeka/moontrack/internal/module/defi"
	"github.com/kislikjeka/moontrack/internal/module/genesis"
	"github.com/kislikjeka/moontrack/internal/module/lending"
	"github.com/kislikjeka/moontrack/internal/module/liquidity"
	"github.com/kislikjeka/moontrack/internal/module/swap"
	"github.com/kislikjeka/moontrack/internal/module/transfer"
	"github.com/kislikjeka/moontrack/internal/platform/wallet"
	"github.com/kislikjeka/moontrack/pkg/logger"
	"github.com/kislikjeka/moontrack/pkg/testasset"
)

// Fixed identities. Wallet UUIDs are embedded verbatim in wallet.*,
// collateral.* and liability.* codes, so they must be constants rather than
// uuid.New() — otherwise the golden set would differ on every run.
var (
	walletID     = uuid.MustParse("11111111-1111-4111-8111-111111111111")
	destWalletID = uuid.MustParse("22222222-2222-4222-8222-222222222222")
	userID       = uuid.MustParse("33333333-3333-4333-8333-333333333333")
)

// occurredAt is fixed and in the past: several validators reject transactions
// dated in the future.
var occurredAt = time.Date(2024, 6, 1, 12, 0, 0, 0, time.UTC)

// fakeWalletRepo satisfies the structurally identical WalletRepository
// interface declared in transfer, swap, defi, liquidity and lending.
type fakeWalletRepo struct{}

func (fakeWalletRepo) GetByID(_ context.Context, id uuid.UUID) (*wallet.Wallet, error) {
	return &wallet.Wallet{
		ID:      id,
		UserID:  userID,
		Address: "0x0000000000000000000000000000000000000001",
	}, nil
}

// corpusCase is one handler driven by one input fixture.
//
// Every handler is exercised through the ledger.Handler interface rather than
// its concrete GenerateEntries method: only 7 of the handlers export
// GenerateEntries, but all of them implement Handle, so the interface is the
// only uniform seam. Handle also runs ValidateData first, which means the
// corpus doubles as a check that the fixtures are inputs production would
// actually accept.
type corpusCase struct {
	name    string
	handler ledger.Handler
	data    map[string]interface{}
}

// corpus enumerates the inputs the golden set is taken from.
//
// It is chosen for namespace coverage rather than realism: each case exists to
// reach account-code branches that no other case reaches. The comments naming a
// namespace on each case record that intent; they are not themselves checked.
// What TestAccountCodesGolden enforces is weaker but automatic: that each of the
// seven namespaces is reached by *something* in the corpus. The golden file is
// the strong net — it pins the exact set, so a code that moves between handlers
// still shows up as a diff.
//
// The registered asset_adjustment handler is deliberately absent: it sets
// Entry.AccountID directly and never writes an account_code into metadata, so
// it contributes nothing to the set. Adding it would only fail the
// missing-account_code check in collectCodes.
func corpus() []corpusCase {
	log := logger.NewDefault("test")
	repo := fakeWalletRepo{}

	return []corpusCase{
		// wallet. + income.  — plain incoming transfer.
		{
			name:    "transfer_in",
			handler: transfer.NewTransferInHandler(repo, log),
			data: map[string]interface{}{
				"wallet_id":        walletID.String(),
				"asset_id":         testasset.ETH.String(),
				"asset_symbol":     "ETH",
				"decimals":         float64(18),
				"amount":           "1000000000000000000",
				"usd_rate":         "250000000000",
				"chain_id":         "ethereum",
				"tx_hash":          "0xaaa1",
				"block_number":     float64(1),
				"from_address":     "0x0000000000000000000000000000000000000002",
				"contract_address": "",
				"occurred_at":      occurredAt.Format(time.RFC3339),
			},
		},
		// wallet. + expense. + gas.  — outgoing transfer with a gas fee.
		{
			name:    "transfer_out",
			handler: transfer.NewTransferOutHandler(repo, log),
			data: map[string]interface{}{
				"wallet_id":       walletID.String(),
				"asset_id":        testasset.USDC.String(),
				"asset_symbol":    "USDC",
				"decimals":        float64(6),
				"amount":          "1000000",
				"usd_rate":        "100000000",
				"gas_amount":      "210000000000000",
				"gas_usd_rate":    "250000000000",
				"native_asset_id": testasset.ETH.String(),
				"chain_id":        "ethereum",
				"tx_hash":         "0xaaa2",
				"block_number":    float64(2),
				"to_address":      "0x0000000000000000000000000000000000000003",
				"occurred_at":     occurredAt.Format(time.RFC3339),
			},
		},
		// wallet. (two distinct wallet IDs, two chains) + gas.
		// Cross-chain so that source and destination chain segments differ —
		// and so do the ASSET segments, because identity is (chain, contract)
		// and the token that arrives on Arbitrum is its own registry row. A
		// bridge whose two codes share one asset id is the #70 shape.
		{
			name:    "internal_transfer",
			handler: transfer.NewInternalTransferHandler(repo, log),
			data: map[string]interface{}{
				"source_wallet_id":  walletID.String(),
				"dest_wallet_id":    destWalletID.String(),
				"asset_id":          testasset.ETH.String(),
				"asset_symbol":      "ETH",
				"dest_asset_id":     testasset.ETHOnArbitrum.String(),
				"dest_asset_symbol": "ETH",
				"decimals":          float64(18),
				"amount":            "500000000000000000",
				"usd_rate":          "250000000000",
				"gas_amount":        "210000000000000",
				"gas_usd_rate":      "250000000000",
				"gas_decimals":      float64(18),
				"native_asset_id":   testasset.ETH.String(),
				"chain_id":          "base",
				"source_chain_id":   "base",
				"dest_chain_id":     "arbitrum",
				"tx_hash":           "0xaaa3",
				"block_number":      float64(3),
				"occurred_at":       occurredAt.Format(time.RFC3339),
			},
		},
		// clearing. + wallet. + gas.  — swap routes both legs through clearing.
		{
			name:    "swap",
			handler: swap.NewSwapHandler(repo, log),
			data: map[string]interface{}{
				"wallet_id":   walletID.String(),
				"tx_hash":     "0xaaa4",
				"chain_id":    "ethereum",
				"occurred_at": occurredAt.Format(time.RFC3339),
				"protocol":    "Uniswap V3",
				"transfers_out": []interface{}{
					map[string]interface{}{
						"asset_id":     testasset.ETH.String(),
						"asset_symbol": "ETH",
						"amount":       "1000000000000000000",
						"decimals":     float64(18),
						"usd_price":    "250000000000",
					},
				},
				"transfers_in": []interface{}{
					map[string]interface{}{
						"asset_id":     testasset.USDC.String(),
						"asset_symbol": "USDC",
						"amount":       "2500000000",
						"decimals":     float64(6),
						"usd_price":    "100000000",
					},
				},
				"fee_asset":        testasset.ETH.String(),
				"fee_asset_symbol": "ETH",
				"fee_amount":       "210000000000000",
				"fee_decimals":     float64(18),
				"fee_usd_price":    "250000000000",
			},
		},
		// clearing. + wallet. + gas.  — DeFi deposit.
		{
			name:    "defi_deposit",
			handler: defi.NewDeFiDepositHandler(repo, log),
			data: map[string]interface{}{
				"wallet_id":      walletID.String(),
				"tx_hash":        "0xaaa5",
				"chain_id":       "ethereum",
				"occurred_at":    occurredAt.Format(time.RFC3339),
				"protocol":       "Curve",
				"operation_type": "deposit",
				"transfers": []interface{}{
					map[string]interface{}{
						"asset_id":     testasset.USDC.String(),
						"asset_symbol": "USDC",
						"amount":       "1000000000",
						"decimals":     float64(6),
						"usd_price":    "100000000",
						"direction":    "out",
					},
					map[string]interface{}{
						"asset_id":     testasset.ForTicker("crvUSDC").String(),
						"asset_symbol": "crvUSDC",
						"amount":       "990000000",
						"decimals":     float64(6),
						"usd_price":    "101000000",
						"direction":    "in",
					},
				},
				"fee_asset":        testasset.ETH.String(),
				"fee_asset_symbol": "ETH",
				"fee_amount":       "210000000000000",
				"fee_decimals":     float64(18),
				"fee_usd_price":    "250000000000",
			},
		},
		// clearing. + wallet.  — DeFi withdraw (mirror of deposit).
		{
			name:    "defi_withdraw",
			handler: defi.NewDeFiWithdrawHandler(repo, log),
			data: map[string]interface{}{
				"wallet_id":      walletID.String(),
				"tx_hash":        "0xaaa6",
				"chain_id":       "ethereum",
				"occurred_at":    occurredAt.Format(time.RFC3339),
				"protocol":       "Curve",
				"operation_type": "withdraw",
				"transfers": []interface{}{
					map[string]interface{}{
						"asset_id":     testasset.ForTicker("crvUSDC").String(),
						"asset_symbol": "crvUSDC",
						"amount":       "990000000",
						"decimals":     float64(6),
						"usd_price":    "101000000",
						"direction":    "out",
					},
					map[string]interface{}{
						"asset_id":     testasset.USDC.String(),
						"asset_symbol": "USDC",
						"amount":       "1000000000",
						"decimals":     float64(6),
						"usd_price":    "100000000",
						"direction":    "in",
					},
				},
			},
		},
		// income.defi. variant + wallet.
		{
			name:    "defi_claim",
			handler: defi.NewDeFiClaimHandler(repo, log),
			data: map[string]interface{}{
				"wallet_id":      walletID.String(),
				"tx_hash":        "0xaaa7",
				"chain_id":       "ethereum",
				"occurred_at":    occurredAt.Format(time.RFC3339),
				"protocol":       "Curve",
				"operation_type": "claim",
				"transfers": []interface{}{
					map[string]interface{}{
						"asset_id":     testasset.CRV.String(),
						"asset_symbol": "CRV",
						"amount":       "5000000000000000000",
						"decimals":     float64(18),
						"usd_price":    "50000000",
						"direction":    "in",
					},
				},
			},
		},
		// clearing. + wallet. + gas.  — LP deposit.
		{
			name:    "lp_deposit",
			handler: liquidity.NewLPDepositHandler(repo, log),
			data: map[string]interface{}{
				"wallet_id":   walletID.String(),
				"tx_hash":     "0xaaa8",
				"chain_id":    "base",
				"occurred_at": occurredAt.Format(time.RFC3339),
				"protocol":    "Uniswap V3",
				"transfers": []interface{}{
					map[string]interface{}{
						"asset_id":     testasset.WETH.String(),
						"asset_symbol": "WETH",
						"amount":       "1000000000000000000",
						"decimals":     float64(18),
						"usd_price":    "250000000000",
						"direction":    "out",
					},
					map[string]interface{}{
						"asset_id":     testasset.USDC.String(),
						"asset_symbol": "USDC",
						"amount":       "2500000000",
						"decimals":     float64(6),
						"usd_price":    "100000000",
						"direction":    "out",
					},
				},
				"fee_asset":        testasset.ETH.String(),
				"fee_asset_symbol": "ETH",
				"fee_amount":       "210000000000000",
				"fee_decimals":     float64(18),
				"fee_usd_price":    "250000000000",
			},
		},
		// clearing. + wallet.  — LP withdraw.
		{
			name:    "lp_withdraw",
			handler: liquidity.NewLPWithdrawHandler(repo, log),
			data: map[string]interface{}{
				"wallet_id":   walletID.String(),
				"tx_hash":     "0xaaa9",
				"chain_id":    "base",
				"occurred_at": occurredAt.Format(time.RFC3339),
				"protocol":    "Uniswap V3",
				"transfers": []interface{}{
					map[string]interface{}{
						"asset_id":     testasset.WETH.String(),
						"asset_symbol": "WETH",
						"amount":       "1000000000000000000",
						"decimals":     float64(18),
						"usd_price":    "250000000000",
						"direction":    "in",
					},
				},
			},
		},
		// income.lp. variant + wallet.
		{
			name:    "lp_claim_fees",
			handler: liquidity.NewLPClaimFeesHandler(repo, log),
			data: map[string]interface{}{
				"wallet_id":   walletID.String(),
				"tx_hash":     "0xaaaa",
				"chain_id":    "base",
				"occurred_at": occurredAt.Format(time.RFC3339),
				"protocol":    "Uniswap V3",
				"transfers": []interface{}{
					map[string]interface{}{
						"asset_id":     testasset.USDC.String(),
						"asset_symbol": "USDC",
						"amount":       "1500000",
						"decimals":     float64(6),
						"usd_price":    "100000000",
						"direction":    "in",
					},
				},
			},
		},
		// collateral. (five segments) + wallet. + gas.
		//
		// Supply and withdraw are the pair that split on live data (#73): the
		// provider named the protocol on one leg and left it empty on the
		// other, so the two must be spelled differently here or the corpus
		// cannot show that both now reach the same collateral account.
		{
			name:    "lending_supply",
			handler: lending.NewLendingSupplyHandler(repo, log),
			data:    lendingData("0xaab1", testasset.USDC, "USDC", "1000000000", float64(6), true, "Fluid USD Coin"),
		},
		// collateral. (five segments) + wallet.
		{
			name:    "lending_withdraw",
			handler: lending.NewLendingWithdrawHandler(repo, log),
			data:    lendingData("0xaab2", testasset.USDC, "USDC", "1000000000", float64(6), false, "fluid usd coin"),
		},
		// liability. (five segments) + wallet.
		{
			name:    "lending_borrow",
			handler: lending.NewLendingBorrowHandler(repo, log),
			data:    lendingData("0xaab3", testasset.DAI, "DAI", "1000000000000000000", float64(18), false, "Aave v3.1"),
		},
		// liability. (five segments) + wallet. Empty protocol: the provider
		// often omits it entirely, which is what produced the empty segment.
		{
			name:    "lending_repay",
			handler: lending.NewLendingRepayHandler(repo, log),
			data:    lendingData("0xaab4", testasset.DAI, "DAI", "1000000000000000000", float64(18), false, ""),
		},
		// income.lending. variant + wallet.
		{
			name:    "lending_claim",
			handler: lending.NewLendingClaimHandler(repo, log),
			data:    lendingData("0xaab5", testasset.AAVE, "AAVE", "2000000000000000000", float64(18), false, "Aave V3"),
		},
		// income.genesis. variant + wallet.
		{
			name:    "genesis_balance",
			handler: genesis.NewHandler(log),
			data: map[string]interface{}{
				"wallet_id":    walletID.String(),
				"asset_id":     testasset.BTC.String(),
				"asset_symbol": "BTC",
				"chain_id":     "bitcoin",
				"amount":       "100000000",
				"decimals":     float64(8),
				"usd_rate":     "6500000000000",
				"occurred_at":  occurredAt.Format(time.RFC3339),
			},
		},
	}
}

// lendingData builds a single-item lending fixture. withGas adds a fee so that
// the lending gas branch is covered once without repeating it in all five.
//
// The asset is passed as an (id, ticker) pair rather than a ticker alone: the
// id is what the account code is built from since #59, and deriving it from the
// ticker here would give a ticker two identities if any other fixture names the
// same asset by its testasset constant.
//
// proto is the raw provider-supplied protocol name, deliberately NOT
// pre-normalised. The corpus used to hardcode "aave-v3" — a spelling no
// provider emits — so the golden file never saw the raw display names and
// empty values that actually arrive, and could not have caught #73. Handing it
// the real inputs is what makes the golden file guard the normalisation.
func lendingData(txHash string, assetID uuid.UUID, symbol, amount string, decimals float64, withGas bool, proto string) map[string]interface{} {
	data := map[string]interface{}{
		"wallet_id":   walletID.String(),
		"tx_hash":     txHash,
		"chain_id":    "arbitrum",
		"occurred_at": occurredAt.Format(time.RFC3339),
		"protocol":    proto,
		// No "direction" key: LendingTransferItem declares the field but no
		// lending entry builder reads it — the operation itself decides the
		// account pair. Setting it would imply a distinction that is not there.
		"transfers": []interface{}{
			map[string]interface{}{
				"asset_id":     assetID.String(),
				"asset_symbol": symbol,
				"decimals":     decimals,
				"amount":       amount,
				"usd_rate":     "100000000",
			},
		},
	}
	if withGas {
		data["fee_asset"] = testasset.ETH.String()
		data["fee_asset_symbol"] = "ETH"
		data["fee_amount"] = "210000000000000"
		data["fee_decimals"] = float64(18)
		data["fee_usd_price"] = "250000000000"
	}
	return data
}
