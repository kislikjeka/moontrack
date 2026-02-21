package ledger

import (
	"context"
	"fmt"
	"math/big"
	"time"

	"github.com/google/uuid"
	"github.com/kislikjeka/moontrack/pkg/logger"
)

// NewTaxLotHook returns a PostBalanceHook that creates/consumes tax lots
// for every CRYPTO_WALLET entry in the transaction.
//
// Classification is entry-type-based (not tx-type-based), so it works
// automatically with any current or future transaction type.
func NewTaxLotHook(repo TaxLotRepository, ledgerRepo Repository, log *logger.Logger) PostBalanceHook {
	hookLog := log.WithField("component", "taxlot_hook")

	return func(ctx context.Context, tx *Transaction) error {
		if len(tx.Entries) == 0 {
			return nil
		}

		// Cache account lookups to avoid repeated DB hits
		accountCache := make(map[uuid.UUID]*Account)
		getAccount := func(accountID uuid.UUID) (*Account, error) {
			if a, ok := accountCache[accountID]; ok {
				return a, nil
			}
			a, err := ledgerRepo.GetAccount(ctx, accountID)
			if err != nil {
				return nil, err
			}
			accountCache[accountID] = a
			return a, nil
		}

		// Separate entries into disposals and acquisitions.
		// Process disposals first so we can link acquired lots to source lots.
		type disposalEntry struct {
			entry *Entry
			acct  *Account
		}
		type acquisitionEntry struct {
			entry *Entry
			acct  *Account
		}

		var disposals []disposalEntry
		var acquisitions []acquisitionEntry

		for _, entry := range tx.Entries {
			acct, err := getAccount(entry.AccountID)
			if err != nil {
				return fmt.Errorf("failed to lookup account %s for tax lot: %w", entry.AccountID, err)
			}

			if acct.Type != AccountTypeCryptoWallet {
				continue
			}

			switch entry.EntryType {
			case EntryTypeAssetDecrease:
				disposals = append(disposals, disposalEntry{entry: entry, acct: acct})
			case EntryTypeAssetIncrease:
				acquisitions = append(acquisitions, acquisitionEntry{entry: entry, acct: acct})
			}
		}

		// Track disposals by (accountID, asset) → first consumed lot ID, for linking.
		// Track disposal results per asset for internal-transfer lot linking
		// and cost basis carry-over.
		type disposalResult struct {
			firstLotID *uuid.UUID
			disposals  []*LotDisposal
		}
		disposalResults := make(map[string]*disposalResult) // key: asset

		// --- Process disposals ---
		for _, d := range disposals {
			dt := classifyDisposalType(tx, d.entry)
			proceedsPerUnit := d.entry.USDRate
			if proceedsPerUnit == nil {
				proceedsPerUnit = big.NewInt(0)
			}

			lotDisposals, err := DisposeFIFO(
				ctx, repo,
				d.acct.ID, d.entry.AssetID,
				d.entry.Amount,
				proceedsPerUnit,
				dt,
				tx.ID,
				d.entry.OccurredAt,
			)
			if err == ErrInsufficientLots {
				hookLog.Warn("insufficient lots for disposal, continuing",
					"tx_id", tx.ID.String(),
					"account_id", d.acct.ID.String(),
					"asset", d.entry.AssetID,
					"amount", d.entry.Amount.String())
				// Don't fail the transaction
			} else if err != nil {
				return err
			}

			// Accumulate disposal results for this asset
			if len(lotDisposals) > 0 {
				dr, exists := disposalResults[d.entry.AssetID]
				if !exists {
					id := lotDisposals[0].LotID
					dr = &disposalResult{firstLotID: &id}
					disposalResults[d.entry.AssetID] = dr
				}
				dr.disposals = append(dr.disposals, lotDisposals...)
			}
		}

		// --- Process acquisitions ---
		for _, a := range acquisitions {
			costBasisPerUnit := a.entry.USDRate
			if costBasisPerUnit == nil {
				costBasisPerUnit = big.NewInt(0)
			}

			source := classifyCostBasisSource(tx)

			var linkedLotID *uuid.UUID
			if tx.Type == TxTypeInternalTransfer {
				if dr, ok := disposalResults[a.entry.AssetID]; ok {
					linkedLotID = dr.firstLotID

					// Carry over weighted-average cost basis from consumed source lots
					// instead of using FMV at transfer time.
					waCost := weightedAvgCostBasis(ctx, repo, dr.disposals)
					if waCost != nil {
						costBasisPerUnit = waCost
					}
				}
			}

			lot := &TaxLot{
				ID:                   uuid.New(),
				TransactionID:        tx.ID,
				AccountID:            a.acct.ID,
				Asset:                a.entry.AssetID,
				QuantityAcquired:     new(big.Int).Set(a.entry.Amount),
				QuantityRemaining:    new(big.Int).Set(a.entry.Amount),
				AcquiredAt:           a.entry.OccurredAt,
				AutoCostBasisPerUnit: new(big.Int).Set(costBasisPerUnit),
				AutoCostBasisSource:  source,
				LinkedSourceLotID:    linkedLotID,
				CreatedAt:            time.Now(),
			}

			if err := repo.CreateTaxLot(ctx, lot); err != nil {
				return err
			}

			hookLog.Debug("created tax lot",
				"lot_id", lot.ID.String(),
				"tx_id", tx.ID.String(),
				"asset", lot.Asset,
				"quantity", lot.QuantityAcquired.String(),
				"source", string(lot.AutoCostBasisSource))
		}

		return nil
	}
}

// classifyDisposalType determines the disposal type from the transaction and entry.
func classifyDisposalType(tx *Transaction, entry *Entry) DisposalType {
	// Check for gas payment marker
	if entry.Metadata != nil {
		if et, ok := entry.Metadata["entry_type"].(string); ok && et == "gas_payment" {
			return DisposalTypeGasFee
		}
	}

	if tx.Type == TxTypeInternalTransfer {
		return DisposalTypeInternalTransfer
	}

	return DisposalTypeSale
}

// classifyCostBasisSource determines the cost basis source from the transaction type.
func classifyCostBasisSource(tx *Transaction) CostBasisSource {
	switch tx.Type {
	case TxTypeSwap:
		return CostBasisSwapPrice
	case TxTypeInternalTransfer:
		return CostBasisLinkedTransfer
	default:
		return CostBasisFMVAtTransfer
	}
}

// weightedAvgCostBasis computes a weighted-average cost basis from consumed
// source lots. Used for internal transfers so cost basis carries over
// rather than using FMV at transfer time.
//
// Formula: sum(lot.EffectiveCostBasis * quantityDisposed) / sum(quantityDisposed)
// Returns nil if no disposals or total quantity is zero.
func weightedAvgCostBasis(ctx context.Context, repo TaxLotRepository, disposals []*LotDisposal) *big.Int {
	if len(disposals) == 0 {
		return nil
	}

	totalCostTimesQty := new(big.Int)
	totalQty := new(big.Int)

	for _, d := range disposals {
		lot, err := repo.GetTaxLot(ctx, d.LotID)
		if err != nil {
			// If we can't look up the source lot, skip it.
			// The caller will fall back to FMV.
			continue
		}

		costBasis := lot.EffectiveCostBasisPerUnit()
		// cost * quantity
		contribution := new(big.Int).Mul(costBasis, d.QuantityDisposed)
		totalCostTimesQty.Add(totalCostTimesQty, contribution)
		totalQty.Add(totalQty, d.QuantityDisposed)
	}

	if totalQty.Sign() <= 0 {
		return nil
	}

	// weighted average = totalCostTimesQty / totalQty
	return new(big.Int).Div(totalCostTimesQty, totalQty)
}
