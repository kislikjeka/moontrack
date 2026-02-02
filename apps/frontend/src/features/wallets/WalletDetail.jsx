import { useState } from 'react';
import { useParams, useNavigate } from 'react-router-dom';
import { useQuery } from '@tanstack/react-query';
import { getWallet, updateWallet, deleteWallet, SUPPORTED_CHAINS } from '../../services/wallet';

/**
 * WalletDetail component
 * Displays detailed wallet information with edit and delete options
 */
const WalletDetail = () => {
  const { id } = useParams();
  const navigate = useNavigate();
  const [isEditing, setIsEditing] = useState(false);
  const [formData, setFormData] = useState({
    name: '',
    chain_id: '',
    address: '',
  });
  const [errors, setErrors] = useState({});
  const [isSubmitting, setIsSubmitting] = useState(false);

  // Fetch wallet details
  const {
    data: wallet,
    isLoading,
    isError,
    error,
    refetch,
  } = useQuery({
    queryKey: ['wallet', id],
    queryFn: () => getWallet(id),
    onSuccess: (data) => {
      setFormData({
        name: data.name,
        chain_id: data.chain_id,
        address: data.address || '',
      });
    },
  });

  const chain = SUPPORTED_CHAINS.find((c) => c.id === wallet?.chain_id);

  const handleEdit = () => {
    setFormData({
      name: wallet.name,
      chain_id: wallet.chain_id,
      address: wallet.address || '',
    });
    setIsEditing(true);
  };

  const handleCancelEdit = () => {
    setIsEditing(false);
    setErrors({});
  };

  const handleChange = (e) => {
    const { name, value } = e.target;
    setFormData((prev) => ({
      ...prev,
      [name]: value,
    }));
    if (errors[name]) {
      setErrors((prev) => ({
        ...prev,
        [name]: null,
      }));
    }
  };

  const handleSubmit = async (e) => {
    e.preventDefault();

    const newErrors = {};
    if (!formData.name.trim()) {
      newErrors.name = 'Wallet name is required';
    }
    if (Object.keys(newErrors).length > 0) {
      setErrors(newErrors);
      return;
    }

    setIsSubmitting(true);
    setErrors({});

    try {
      const walletData = {
        name: formData.name.trim(),
        chain_id: formData.chain_id,
      };
      if (formData.address.trim()) {
        walletData.address = formData.address.trim();
      }

      await updateWallet(id, walletData);
      setIsEditing(false);
      refetch();
    } catch (error) {
      console.error('Failed to update wallet:', error);
      if (error.response?.data?.error) {
        setErrors({ general: error.response.data.error });
      } else {
        setErrors({ general: 'Failed to update wallet. Please try again.' });
      }
    } finally {
      setIsSubmitting(false);
    }
  };

  const handleDelete = async () => {
    if (!window.confirm(`Are you sure you want to delete "${wallet.name}"?`)) {
      return;
    }

    try {
      await deleteWallet(id);
      navigate('/wallets');
    } catch (error) {
      console.error('Failed to delete wallet:', error);
      alert('Failed to delete wallet. Please try again.');
    }
  };

  if (isLoading) {
    return (
      <div className="wallet-detail-loading">
        <div className="spinner"></div>
        <p>Loading wallet...</p>
      </div>
    );
  }

  if (isError) {
    return (
      <div className="wallet-detail-error">
        <p>Error loading wallet: {error.message}</p>
        <button onClick={() => navigate('/wallets')}>Back to Wallets</button>
      </div>
    );
  }

  if (!wallet) {
    return (
      <div className="wallet-detail-not-found">
        <p>Wallet not found</p>
        <button onClick={() => navigate('/wallets')}>Back to Wallets</button>
      </div>
    );
  }

  return (
    <div className="wallet-detail">
      <div className="wallet-detail-header">
        <button className="btn-back" onClick={() => navigate('/wallets')}>
          ← Back to Wallets
        </button>
      </div>

      <div className="wallet-detail-content">
        {isEditing ? (
          <form className="wallet-edit-form" onSubmit={handleSubmit}>
            <h2>Edit Wallet</h2>

            {errors.general && (
              <div className="form-error-general">{errors.general}</div>
            )}

            <div className="form-group">
              <label htmlFor="name">Wallet Name</label>
              <input
                type="text"
                id="name"
                name="name"
                value={formData.name}
                onChange={handleChange}
                className={errors.name ? 'input-error' : ''}
                disabled={isSubmitting}
              />
              {errors.name && <span className="form-error">{errors.name}</span>}
            </div>

            <div className="form-group">
              <label htmlFor="chain_id">Blockchain</label>
              <select
                id="chain_id"
                name="chain_id"
                value={formData.chain_id}
                onChange={handleChange}
                disabled={isSubmitting}
              >
                {SUPPORTED_CHAINS.map((chain) => (
                  <option key={chain.id} value={chain.id}>
                    {chain.name} ({chain.symbol})
                  </option>
                ))}
              </select>
            </div>

            <div className="form-group">
              <label htmlFor="address">Wallet Address (optional)</label>
              <input
                type="text"
                id="address"
                name="address"
                value={formData.address}
                onChange={handleChange}
                disabled={isSubmitting}
              />
            </div>

            <div className="form-actions">
              <button
                type="button"
                className="btn-secondary"
                onClick={handleCancelEdit}
                disabled={isSubmitting}
              >
                Cancel
              </button>
              <button
                type="submit"
                className="btn-primary"
                disabled={isSubmitting}
              >
                {isSubmitting ? 'Saving...' : 'Save Changes'}
              </button>
            </div>
          </form>
        ) : (
          <>
            <div className="wallet-info-card">
              <div className="wallet-header">
                <div className="wallet-icon-large">{chain?.symbol}</div>
                <div>
                  <h1>{wallet.name}</h1>
                  <p className="wallet-chain-name">{chain?.name || wallet.chain_id}</p>
                </div>
              </div>

              {wallet.address && (
                <div className="wallet-address-full">
                  <label>Address:</label>
                  <code>{wallet.address}</code>
                </div>
              )}

              <div className="wallet-metadata">
                <div className="metadata-item">
                  <label>Created:</label>
                  <span>{new Date(wallet.created_at).toLocaleString()}</span>
                </div>
                <div className="metadata-item">
                  <label>Last Updated:</label>
                  <span>{new Date(wallet.updated_at).toLocaleString()}</span>
                </div>
              </div>
            </div>

            <div className="wallet-actions">
              <button className="btn-primary" onClick={handleEdit}>
                Edit Wallet
              </button>
              <button className="btn-danger" onClick={handleDelete}>
                Delete Wallet
              </button>
            </div>

            <div className="wallet-assets">
              <h3>Assets</h3>
              <p className="placeholder-text">
                Asset management coming soon in Phase 5
              </p>
            </div>
          </>
        )}
      </div>
    </div>
  );
};

export default WalletDetail;
