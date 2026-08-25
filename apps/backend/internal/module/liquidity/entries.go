package liquidity

import (
	"math/big"
	"time"

	"github.com/google/uuid"

	"github.com/kislikjeka/moontrack/internal/ledger"
	"github.com/kislikjeka/moontrack/internal/ledger/accountcode"
	"github.com/kislikjeka/moontrack/pkg/money"
)

// transferAsset is the identity of the asset one LP transfer moves: the
// transaction's chain paired with the transfer's registry UUID (#59).
//
// The wallet leg and its clearing or income counterpart sit on neighbouring
// lines and used to read the chain from two different expressions — a local
// alias on one, the transaction field on the other. Same value today, but that
// is the shape that let the halves drift apart in #70.
func transferAsset(txn *LPTransaction, tr LPTransfer) accountcode.Asset {
	return accountcode.OnChain(txn.ChainID, tr.AssetID)
}

// generateSwapLikeEntries generates balanced entries for LP deposit/withdraw.
// Same pattern as swap: outgoing assets go through clearing, incoming assets go through clearing.
func generateSwapLikeEntries(txn *LPTransaction) []*ledger.Entry {
	entries := make([]*ledger.Entry, 0, 2*(len(txn.Transfers))+2)

	walletIDStr := txn.WalletID.String()
	chainIDStr := txn.ChainID

	for _, tr := range txn.Transfers {
		amount := tr.Amount.ToBigInt()
		usdRate := tr.USDPrice.ToBigInt()
		usdValue := money.CalcUSDValue(amount, usdRate, tr.Decimals)

		if tr.Direction == "out" {
			// CREDIT wallet (asset decrease)
			entries = append(entries, &ledger.Entry{
				ID:          uuid.New(),
				AccountID:   uuid.Nil,
				DebitCredit: ledger.Credit,
				EntryType:   ledger.EntryTypeAssetDecrease,
				Amount:      new(big.Int).Set(amount),
				AssetID:     tr.AssetID,
				USDRate:     money.CopyRate(usdRate),
				USDValue:    money.CopyRate(usdValue),
				OccurredAt:  txn.OccurredAt,
				CreatedAt:   time.Now().UTC(),
				Metadata: map[string]any{
					"wallet_id":        walletIDStr,
					"account_code":     accountcode.Wallet(txn.WalletID, transferAsset(txn, tr)),
					"tx_hash":          txn.TxHash,
					"chain_id":         chainIDStr,
					"lp_direction":     "out",
					"contract_address": tr.ContractAddress,
				},
			})

			// DEBIT clearing
			entries = append(entries, &ledger.Entry{
				ID:          uuid.New(),
				AccountID:   uuid.Nil,
				DebitCredit: ledger.Debit,
				EntryType:   ledger.EntryTypeClearing,
				Amount:      new(big.Int).Set(amount),
				AssetID:     tr.AssetID,
				USDRate:     money.CopyRate(usdRate),
				USDValue:    money.CopyRate(usdValue),
				OccurredAt:  txn.OccurredAt,
				CreatedAt:   time.Now().UTC(),
				Metadata: map[string]any{
					"account_code": accountcode.Clearing(transferAsset(txn, tr)),
					"account_type": "CLEARING",
					"chain_id":     chainIDStr,
					"tx_hash":      txn.TxHash,
					"lp_direction": "out",
				},
			})
		} else {
			// DEBIT wallet (asset increase)
			entries = append(entries, &ledger.Entry{
				ID:          uuid.New(),
				AccountID:   uuid.Nil,
				DebitCredit: ledger.Debit,
				EntryType:   ledger.EntryTypeAssetIncrease,
				Amount:      new(big.Int).Set(amount),
				AssetID:     tr.AssetID,
				USDRate:     money.CopyRate(usdRate),
				USDValue:    money.CopyRate(usdValue),
				OccurredAt:  txn.OccurredAt,
				CreatedAt:   time.Now().UTC(),
				Metadata: map[string]any{
					"wallet_id":        walletIDStr,
					"account_code":     accountcode.Wallet(txn.WalletID, transferAsset(txn, tr)),
					"tx_hash":          txn.TxHash,
					"chain_id":         chainIDStr,
					"lp_direction":     "in",
					"contract_address": tr.ContractAddress,
				},
			})

			// CREDIT clearing
			entries = append(entries, &ledger.Entry{
				ID:          uuid.New(),
				AccountID:   uuid.Nil,
				DebitCredit: ledger.Credit,
				EntryType:   ledger.EntryTypeClearing,
				Amount:      new(big.Int).Set(amount),
				AssetID:     tr.AssetID,
				USDRate:     money.CopyRate(usdRate),
				USDValue:    money.CopyRate(usdValue),
				OccurredAt:  txn.OccurredAt,
				CreatedAt:   time.Now().UTC(),
				Metadata: map[string]any{
					"account_code": accountcode.Clearing(transferAsset(txn, tr)),
					"account_type": "CLEARING",
					"chain_id":     chainIDStr,
					"tx_hash":      txn.TxHash,
					"lp_direction": "in",
				},
			})
		}
	}

	return entries
}

// generateLPClaimEntries generates entries for LP fee claims.
// Incoming assets are booked as income (income.lp.{chain}.{asset}).
func generateLPClaimEntries(txn *LPTransaction) []*ledger.Entry {
	entries := make([]*ledger.Entry, 0, 2*len(txn.Transfers))

	walletIDStr := txn.WalletID.String()
	chainIDStr := txn.ChainID

	for _, tr := range txn.Transfers {
		if tr.Direction != "in" {
			continue
		}

		amount := tr.Amount.ToBigInt()
		usdRate := tr.USDPrice.ToBigInt()
		usdValue := money.CalcUSDValue(amount, usdRate, tr.Decimals)

		// DEBIT wallet (asset increase)
		entries = append(entries, &ledger.Entry{
			ID:          uuid.New(),
			AccountID:   uuid.Nil,
			DebitCredit: ledger.Debit,
			EntryType:   ledger.EntryTypeAssetIncrease,
			Amount:      new(big.Int).Set(amount),
			AssetID:     tr.AssetID,
			USDRate:     money.CopyRate(usdRate),
			USDValue:    money.CopyRate(usdValue),
			OccurredAt:  txn.OccurredAt,
			CreatedAt:   time.Now().UTC(),
			Metadata: map[string]any{
				"wallet_id":        walletIDStr,
				"account_code":     accountcode.Wallet(txn.WalletID, transferAsset(txn, tr)),
				"tx_hash":          txn.TxHash,
				"chain_id":         chainIDStr,
				"lp_direction":     "in",
				"contract_address": tr.ContractAddress,
			},
		})

		// CREDIT income (LP fees)
		entries = append(entries, &ledger.Entry{
			ID:          uuid.New(),
			AccountID:   uuid.Nil,
			DebitCredit: ledger.Credit,
			EntryType:   ledger.EntryTypeIncome,
			Amount:      new(big.Int).Set(amount),
			AssetID:     tr.AssetID,
			USDRate:     money.CopyRate(usdRate),
			USDValue:    money.CopyRate(usdValue),
			OccurredAt:  txn.OccurredAt,
			CreatedAt:   time.Now().UTC(),
			Metadata: map[string]any{
				"account_code": accountcode.IncomeLp(transferAsset(txn, tr)),
				"account_type": "INCOME",
				"chain_id":     chainIDStr,
				"tx_hash":      txn.TxHash,
			},
		})
	}

	return entries
}

// generateGasFeeEntries generates gas fee entries if a fee is present.
func generateGasFeeEntries(txn *LPTransaction) []*ledger.Entry {
	if txn.FeeAmount == nil || txn.FeeAmount.IsNil() || txn.FeeAmount.Sign() <= 0 {
		return nil
	}

	feeAmount := txn.FeeAmount.ToBigInt()
	feeUSDRate := txn.FeeUSDPrice.ToBigInt()
	feeDecimals := txn.FeeDecimals
	if feeDecimals == 0 {
		feeDecimals = 18
	}
	feeUSDValue := money.CalcUSDValue(feeAmount, feeUSDRate, feeDecimals)

	walletIDStr := txn.WalletID.String()
	chainIDStr := txn.ChainID
	// The gas account and the wallet leg that pays it address one asset; the
	// two lines used to read the chain from two different expressions (#70).
	feeIdentity := accountcode.OnChain(txn.ChainID, txn.FeeAsset)

	return []*ledger.Entry{
		// DEBIT gas account
		{
			ID:          uuid.New(),
			AccountID:   uuid.Nil,
			DebitCredit: ledger.Debit,
			EntryType:   ledger.EntryTypeGasFee,
			Amount:      new(big.Int).Set(feeAmount),
			AssetID:     txn.FeeAsset,
			USDRate:     money.CopyRate(feeUSDRate),
			USDValue:    money.CopyRate(feeUSDValue),
			OccurredAt:  txn.OccurredAt,
			CreatedAt:   time.Now().UTC(),
			Metadata: map[string]any{
				"account_code": accountcode.Gas(feeIdentity),
				"tx_hash":      txn.TxHash,
				"chain_id":     chainIDStr,
			},
		},
		// CREDIT wallet (asset decrease for gas)
		{
			ID:          uuid.New(),
			AccountID:   uuid.Nil,
			DebitCredit: ledger.Credit,
			EntryType:   ledger.EntryTypeAssetDecrease,
			Amount:      new(big.Int).Set(feeAmount),
			AssetID:     txn.FeeAsset,
			USDRate:     money.CopyRate(feeUSDRate),
			USDValue:    money.CopyRate(feeUSDValue),
			OccurredAt:  txn.OccurredAt,
			CreatedAt:   time.Now().UTC(),
			Metadata: map[string]any{
				"wallet_id":    walletIDStr,
				"account_code": accountcode.Wallet(txn.WalletID, feeIdentity),
				"tx_hash":      txn.TxHash,
				"chain_id":     chainIDStr,
				"entry_type":   "gas_payment",
			},
		},
	}
}
