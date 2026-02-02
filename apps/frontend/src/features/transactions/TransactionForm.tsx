import React, { useState } from 'react';
import { useMutation, useQueryClient, useQuery } from '@tanstack/react-query';
import transactionService, { CreateTransactionRequest } from '../../services/transaction';
import { getWallets } from '../../services/wallet';

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
    const submitData: CreateTransactionRequest = {
      ...formData,
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

  // Common crypto assets
  const commonAssets = ['BTC', 'ETH', 'USDC', 'USDT', 'BNB', 'SOL', 'ADA', 'DOT', 'MATIC', 'AVAX'];

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
          {walletsData?.wallets.map((wallet) => (
            <option key={wallet.id} value={wallet.id}>
              {wallet.name} ({wallet.chain_id})
            </option>
          ))}
        </select>
        {errors.wallet_id && <span className="error-message">{errors.wallet_id}</span>}
      </div>

      {/* Asset Selector */}
      <div className="form-group">
        <label htmlFor="asset_id">Asset *</label>
        <select
          id="asset_id"
          name="asset_id"
          value={formData.asset_id}
          onChange={handleChange}
          className={errors.asset_id ? 'error' : ''}
        >
          {commonAssets.map((asset) => (
            <option key={asset} value={asset}>
              {asset}
            </option>
          ))}
          <option value="custom">-- Custom (type below) --</option>
        </select>

        {formData.asset_id === 'custom' && (
          <input
            type="text"
            name="asset_id"
            placeholder="Enter custom asset ID (e.g., DOGE)"
            value={formData.asset_id === 'custom' ? '' : formData.asset_id}
            onChange={handleChange}
            className={errors.asset_id ? 'error' : ''}
          />
        )}
        {errors.asset_id && <span className="error-message">{errors.asset_id}</span>}
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

      {/* Manual Price Override */}
      <div className="form-group">
        <label>
          <input
            type="checkbox"
            checked={useManualPrice}
            onChange={(e) => setUseManualPrice(e.target.checked)}
          />
          <span>Manually enter USD price (optional)</span>
        </label>
      </div>

      {useManualPrice && (
        <div className="form-group">
          <label htmlFor="usd_rate">USD Price per Unit *</label>
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
            Price in USD (e.g., 42000.50 for BTC). Leave unchecked to auto-fetch from CoinGecko.
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
