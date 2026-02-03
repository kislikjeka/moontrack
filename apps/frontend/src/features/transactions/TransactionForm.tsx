import React, { useState, useEffect, useCallback } from 'react';
import { useMutation, useQueryClient, useQuery } from '@tanstack/react-query';
import transactionService, { CreateTransactionRequest } from '../../services/transaction';
import { getWallets } from '../../services/wallet';
import { assetService, Asset } from '../../services/asset';
import AssetAutocomplete from '../../components/AssetAutocomplete';

interface TransactionFormProps {
  onSuccess?: () => void;
  onCancel?: () => void;
  initialWalletId?: string;
}

export const TransactionForm: React.FC<TransactionFormProps> = ({
  onSuccess,
  onCancel,
  initialWalletId,
}) => {
  const queryClient = useQueryClient();
  const [formData, setFormData] = useState<CreateTransactionRequest>({
    type: 'manual_income',
    wallet_id: initialWalletId || '',
    asset_id: 'BTC',
    amount: '',
    usd_rate: '',
    occurred_at: new Date().toISOString().slice(0, 16), // datetime-local format
    notes: '',
  });

  const [errors, setErrors] = useState<Record<string, string>>({});
  const [useManualPrice, setUseManualPrice] = useState(false);
  const [selectedAsset, setSelectedAsset] = useState<Asset | null>(null);
  const [priceSource, setPriceSource] = useState<'coingecko' | 'manual' | 'unknown'>('unknown');
  const [isFetchingPrice, setIsFetchingPrice] = useState(false);

  // Fetch user's wallets for the selector
  const { data: walletsData } = useQuery({
    queryKey: ['wallets'],
    queryFn: () => getWallets(),
  });

  // Create transaction mutation
  const createMutation = useMutation({
    mutationFn: (data: CreateTransactionRequest) => transactionService.create(data),
    onSuccess: () => {
      // Invalidate relevant queries to refresh data
      queryClient.invalidateQueries({ queryKey: ['transactions'] });
      queryClient.invalidateQueries({ queryKey: ['portfolio'] });
      queryClient.invalidateQueries({ queryKey: ['wallets'] });

      if (onSuccess) {
        onSuccess();
      }

      // Reset form
      setFormData({
        type: 'manual_income',
        wallet_id: initialWalletId || '',
        asset_id: 'BTC',
        amount: '',
        usd_rate: '',
        occurred_at: new Date().toISOString().slice(0, 16),
        notes: '',
      });
      setErrors({});
    },
    onError: (error: any) => {
      const errorMsg = error.response?.data?.error || error.message || 'Failed to create transaction';
      setErrors({ submit: errorMsg });
    },
  });

  const validate = (): boolean => {
    const newErrors: Record<string, string> = {};

    if (!formData.wallet_id) {
      newErrors.wallet_id = 'Please select a wallet';
    }

    if (!formData.asset_id) {
      newErrors.asset_id = 'Please enter an asset ID';
    }

    if (!formData.amount || parseFloat(formData.amount) <= 0) {
      newErrors.amount = 'Amount must be greater than 0';
    }

    if (useManualPrice && (!formData.usd_rate || parseFloat(formData.usd_rate) <= 0)) {
      newErrors.usd_rate = 'Manual price must be greater than 0';
    }

    if (!formData.occurred_at) {
      newErrors.occurred_at = 'Please select a date and time';
    }

    setErrors(newErrors);
    return Object.keys(newErrors).length === 0;
  };

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault();

    if (!validate()) {
      return;
    }

    // Prepare data for submission
    // Convert datetime-local format to RFC3339 for backend
    const occurredAtRFC3339 = new Date(formData.occurred_at).toISOString();

    const submitData: CreateTransactionRequest = {
      ...formData,
      occurred_at: occurredAtRFC3339,
      coingecko_id: selectedAsset?.id, // Pass CoinGecko ID for price lookup
      usd_rate: useManualPrice ? formData.usd_rate : undefined,
    };

    createMutation.mutate(submitData);
  };

  const handleChange = (
    e: React.ChangeEvent<HTMLInputElement | HTMLSelectElement | HTMLTextAreaElement>
  ) => {
    const { name, value } = e.target;
    setFormData((prev) => ({ ...prev, [name]: value }));

    // Clear error for this field when user starts typing
    if (errors[name]) {
      setErrors((prev) => {
        const newErrors = { ...prev };
        delete newErrors[name];
        return newErrors;
      });
    }
  };

  // Handle asset selection from autocomplete
  const handleAssetChange = useCallback((asset: Asset | null, symbol: string) => {
    setFormData((prev) => ({ ...prev, asset_id: symbol }));
    setSelectedAsset(asset);

    // Clear asset_id error
    setErrors((prev) => {
      if (prev.asset_id) {
        const newErrors = { ...prev };
        delete newErrors.asset_id;
        return newErrors;
      }
      return prev;
    });

    // Fetch price if we have a valid CoinGecko asset and manual price is not enabled
    if (asset && !useManualPrice) {
      fetchAssetPrice(asset.id);
    } else if (!asset) {
      // Unknown asset - show warning
      setPriceSource('unknown');
      if (!useManualPrice) {
        setFormData((prev) => ({ ...prev, usd_rate: '' }));
      }
    }
  }, [useManualPrice]);

  // Fetch price for a CoinGecko asset
  const fetchAssetPrice = async (coinGeckoId: string) => {
    setIsFetchingPrice(true);
    try {
      const priceData = await assetService.getPrice(coinGeckoId);
      // Convert scaled price (10^8) to human-readable
      const priceValue = parseFloat(priceData.price) / 100000000;
      setFormData((prev) => ({ ...prev, usd_rate: priceValue.toString() }));
      setPriceSource('coingecko');
    } catch (error) {
      // Price fetch failed - user can still enter manually
      setPriceSource('unknown');
      setFormData((prev) => ({ ...prev, usd_rate: '' }));
    } finally {
      setIsFetchingPrice(false);
    }
  };

  // Handle manual price toggle
  const handleManualPriceToggle = (enabled: boolean) => {
    setUseManualPrice(enabled);
    if (enabled) {
      setPriceSource('manual');
    } else if (selectedAsset) {
      // Re-fetch price from CoinGecko
      fetchAssetPrice(selectedAsset.id);
    } else {
      setPriceSource('unknown');
      setFormData((prev) => ({ ...prev, usd_rate: '' }));
    }
  };

  return (
    <form onSubmit={handleSubmit} className="transaction-form">
      <h2>Record Transaction</h2>

      {/* Transaction Type Selector */}
      <div className="form-group">
        <label htmlFor="type">Transaction Type *</label>
        <select
          id="type"
          name="type"
          value={formData.type}
          onChange={handleChange}
          className={errors.type ? 'error' : ''}
        >
          <option value="manual_income">Income (Deposit/Purchase)</option>
          <option value="manual_outcome">Outcome (Withdrawal/Sale)</option>
          <option value="asset_adjustment">Balance Adjustment</option>
        </select>
        {errors.type && <span className="error-message">{errors.type}</span>}
      </div>

      {/* Wallet Selector */}
      <div className="form-group">
        <label htmlFor="wallet_id">Wallet *</label>
        <select
          id="wallet_id"
          name="wallet_id"
          value={formData.wallet_id}
          onChange={handleChange}
          className={errors.wallet_id ? 'error' : ''}
        >
          <option value="">-- Select Wallet --</option>
          {walletsData?.map((wallet: any) => (
            <option key={wallet.id} value={wallet.id}>
              {wallet.name} ({wallet.chain_id})
            </option>
          ))}
        </select>
        {errors.wallet_id && <span className="error-message">{errors.wallet_id}</span>}
      </div>

      {/* Asset Selector with Autocomplete */}
      <div className="form-group">
        <label htmlFor="asset_id">Asset *</label>
        <AssetAutocomplete
          value={formData.asset_id}
          onChange={handleAssetChange}
          placeholder="Search asset (e.g., BTC, Bitcoin)"
          error={errors.asset_id}
        />
        {priceSource === 'unknown' && formData.asset_id && formData.asset_id.length >= 2 && !selectedAsset && (
          <span className="help-text warning">
            Asset not found in CoinGecko. Price must be entered manually.
          </span>
        )}
      </div>

      {/* Amount Input */}
      <div className="form-group">
        <label htmlFor="amount">
          {formData.type === 'asset_adjustment' ? 'New Balance *' : 'Amount *'}
        </label>
        <input
          type="number"
          id="amount"
          name="amount"
          value={formData.amount}
          onChange={handleChange}
          step="any"
          min="0"
          placeholder="0.00"
          className={errors.amount ? 'error' : ''}
        />
        <span className="help-text">
          Enter amount in base units (e.g., 1.5 for 1.5 {formData.asset_id})
        </span>
        {errors.amount && <span className="error-message">{errors.amount}</span>}
      </div>

      {/* Price Section */}
      <div className="form-group">
        <label>
          <input
            type="checkbox"
            checked={useManualPrice}
            onChange={(e) => handleManualPriceToggle(e.target.checked)}
          />
          <span>Manually enter USD price (optional)</span>
        </label>

        {/* Price source indicator */}
        {!useManualPrice && formData.usd_rate && (
          <span className={`price-source ${priceSource}`}>
            {isFetchingPrice ? (
              'Fetching price...'
            ) : priceSource === 'coingecko' ? (
              <>Price from CoinGecko: ${parseFloat(formData.usd_rate).toLocaleString()}</>
            ) : null}
          </span>
        )}
      </div>

      {(useManualPrice || priceSource === 'unknown') && (
        <div className="form-group">
          <label htmlFor="usd_rate">USD Price per Unit {priceSource === 'unknown' ? '*' : ''}</label>
          <input
            type="number"
            id="usd_rate"
            name="usd_rate"
            value={formData.usd_rate}
            onChange={handleChange}
            step="any"
            min="0"
            placeholder="0.00"
            className={errors.usd_rate ? 'error' : ''}
          />
          <span className="help-text">
            {priceSource === 'unknown'
              ? 'Asset not found in CoinGecko. Please enter the USD price manually.'
              : 'Price in USD (e.g., 42000.50 for BTC). Uncheck above to auto-fetch from CoinGecko.'}
          </span>
          {errors.usd_rate && <span className="error-message">{errors.usd_rate}</span>}
        </div>
      )}

      {/* Date/Time Input */}
      <div className="form-group">
        <label htmlFor="occurred_at">Date & Time *</label>
        <input
          type="datetime-local"
          id="occurred_at"
          name="occurred_at"
          value={formData.occurred_at}
          onChange={handleChange}
          max={new Date().toISOString().slice(0, 16)}
          className={errors.occurred_at ? 'error' : ''}
        />
        {errors.occurred_at && <span className="error-message">{errors.occurred_at}</span>}
      </div>

      {/* Notes Input */}
      <div className="form-group">
        <label htmlFor="notes">Notes (optional)</label>
        <textarea
          id="notes"
          name="notes"
          value={formData.notes}
          onChange={handleChange}
          rows={3}
          placeholder="Add any notes about this transaction..."
        />
      </div>

      {/* Submit Error */}
      {errors.submit && (
        <div className="error-message error-banner">{errors.submit}</div>
      )}

      {/* Form Actions */}
      <div className="form-actions">
        <button
          type="submit"
          className="btn btn-primary"
          disabled={createMutation.isPending}
        >
          {createMutation.isPending ? 'Recording...' : 'Record Transaction'}
        </button>

        {onCancel && (
          <button
            type="button"
            className="btn btn-secondary"
            onClick={onCancel}
            disabled={createMutation.isPending}
          >
            Cancel
          </button>
        )}
      </div>
    </form>
  );
};

export default TransactionForm;
