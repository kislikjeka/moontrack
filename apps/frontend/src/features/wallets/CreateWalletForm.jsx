import { useState } from 'react';
import { createWallet, SUPPORTED_CHAINS } from '../../services/wallet';

/**
 * CreateWalletForm component
 * Form for creating a new wallet
 */
const CreateWalletForm = ({ onSuccess, onCancel }) => {
  const [formData, setFormData] = useState({
    name: '',
    chain_id: 'ethereum',
    address: '',
  });
  const [errors, setErrors] = useState({});
  const [isSubmitting, setIsSubmitting] = useState(false);

  const handleChange = (e) => {
    const { name, value } = e.target;
    setFormData((prev) => ({
      ...prev,
      [name]: value,
    }));
    // Clear error for this field
    if (errors[name]) {
      setErrors((prev) => ({
        ...prev,
        [name]: null,
      }));
    }
  };

  const validate = () => {
    const newErrors = {};

    if (!formData.name.trim()) {
      newErrors.name = 'Wallet name is required';
    } else if (formData.name.length > 100) {
      newErrors.name = 'Wallet name must be 100 characters or less';
    }

    if (!formData.chain_id) {
      newErrors.chain_id = 'Please select a blockchain';
    }

    return newErrors;
  };

  const handleSubmit = async (e) => {
    e.preventDefault();

    // Validate form
    const newErrors = validate();
    if (Object.keys(newErrors).length > 0) {
      setErrors(newErrors);
      return;
    }

    setIsSubmitting(true);
    setErrors({});

    try {
      // Prepare data (remove address if empty)
      const walletData = {
        name: formData.name.trim(),
        chain_id: formData.chain_id,
      };

      if (formData.address.trim()) {
        walletData.address = formData.address.trim();
      }

      await createWallet(walletData);
      onSuccess();
    } catch (error) {
      console.error('Failed to create wallet:', error);

      // Handle specific error responses
      if (error.response?.data?.error) {
        const errorMessage = error.response.data.error;
        if (errorMessage.includes('already exists')) {
          setErrors({ name: 'A wallet with this name already exists' });
        } else if (errorMessage.includes('invalid chain')) {
          setErrors({ chain_id: 'Invalid blockchain selected' });
        } else {
          setErrors({ general: errorMessage });
        }
      } else {
        setErrors({ general: 'Failed to create wallet. Please try again.' });
      }
    } finally {
      setIsSubmitting(false);
    }
  };

  return (
    <form className="create-wallet-form" onSubmit={handleSubmit}>
      {errors.general && (
        <div className="form-error-general">{errors.general}</div>
      )}

      <div className="form-group">
        <label htmlFor="name">
          Wallet Name <span className="required">*</span>
        </label>
        <input
          type="text"
          id="name"
          name="name"
          value={formData.name}
          onChange={handleChange}
          placeholder="e.g., My Main Wallet"
          className={errors.name ? 'input-error' : ''}
          disabled={isSubmitting}
          maxLength={100}
        />
        {errors.name && <span className="form-error">{errors.name}</span>}
      </div>

      <div className="form-group">
        <label htmlFor="chain_id">
          Blockchain <span className="required">*</span>
        </label>
        <select
          id="chain_id"
          name="chain_id"
          value={formData.chain_id}
          onChange={handleChange}
          className={errors.chain_id ? 'input-error' : ''}
          disabled={isSubmitting}
        >
          {SUPPORTED_CHAINS.map((chain) => (
            <option key={chain.id} value={chain.id}>
              {chain.name} ({chain.symbol})
            </option>
          ))}
        </select>
        {errors.chain_id && <span className="form-error">{errors.chain_id}</span>}
      </div>

      <div className="form-group">
        <label htmlFor="address">
          Wallet Address <span className="optional">(optional)</span>
        </label>
        <input
          type="text"
          id="address"
          name="address"
          value={formData.address}
          onChange={handleChange}
          placeholder="0x..."
          disabled={isSubmitting}
        />
        <small className="form-help">
          You can add the blockchain address for reference
        </small>
      </div>

      <div className="form-actions">
        <button
          type="button"
          className="btn-secondary"
          onClick={onCancel}
          disabled={isSubmitting}
        >
          Cancel
        </button>
        <button
          type="submit"
          className="btn-primary"
          disabled={isSubmitting}
        >
          {isSubmitting ? 'Creating...' : 'Create Wallet'}
        </button>
      </div>
    </form>
  );
};

export default CreateWalletForm;
