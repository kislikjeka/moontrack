import { useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { deleteWallet, SUPPORTED_CHAINS } from '../../services/wallet';

/**
 * WalletCard component
 * Displays a single wallet with actions
 */
const WalletCard = ({ wallet, onUpdate }) => {
  const navigate = useNavigate();
  const [isDeleting, setIsDeleting] = useState(false);
  const [showDeleteConfirm, setShowDeleteConfirm] = useState(false);

  // Get chain info
  const chain = SUPPORTED_CHAINS.find((c) => c.id === wallet.chain_id);
  const chainName = chain?.name || wallet.chain_id;
  const chainSymbol = chain?.symbol || '';

  const handleDelete = async () => {
    setIsDeleting(true);
    try {
      await deleteWallet(wallet.id);
      onUpdate(); // Refresh parent list
    } catch (error) {
      console.error('Failed to delete wallet:', error);
      alert('Failed to delete wallet. Please try again.');
    } finally {
      setIsDeleting(false);
      setShowDeleteConfirm(false);
    }
  };

  const handleCardClick = () => {
    navigate(`/wallets/${wallet.id}`);
  };

  return (
    <div className="wallet-card">
      <div className="wallet-card-content" onClick={handleCardClick}>
        <div className="wallet-card-header">
          <div className="wallet-icon">{chainSymbol}</div>
          <div className="wallet-info">
            <h3 className="wallet-name">{wallet.name}</h3>
            <p className="wallet-chain">{chainName}</p>
          </div>
        </div>

        {wallet.address && (
          <div className="wallet-address">
            <span className="wallet-address-label">Address:</span>
            <span className="wallet-address-value">
              {wallet.address.substring(0, 6)}...
              {wallet.address.substring(wallet.address.length - 4)}
            </span>
          </div>
        )}

        <div className="wallet-meta">
          <span className="wallet-created">
            Created: {new Date(wallet.created_at).toLocaleDateString()}
          </span>
        </div>
      </div>

      <div className="wallet-card-actions">
        <button
          className="btn-secondary btn-sm"
          onClick={(e) => {
            e.stopPropagation();
            navigate(`/wallets/${wallet.id}`);
          }}
        >
          View Details
        </button>
        <button
          className="btn-danger btn-sm"
          onClick={(e) => {
            e.stopPropagation();
            setShowDeleteConfirm(true);
          }}
          disabled={isDeleting}
        >
          {isDeleting ? 'Deleting...' : 'Delete'}
        </button>
      </div>

      {showDeleteConfirm && (
        <div className="delete-confirm-modal">
          <div className="modal-overlay" onClick={() => setShowDeleteConfirm(false)}></div>
          <div className="modal-content">
            <h3>Confirm Delete</h3>
            <p>
              Are you sure you want to delete <strong>{wallet.name}</strong>?
              This action cannot be undone.
            </p>
            <div className="modal-actions">
              <button
                className="btn-secondary"
                onClick={() => setShowDeleteConfirm(false)}
                disabled={isDeleting}
              >
                Cancel
              </button>
              <button
                className="btn-danger"
                onClick={handleDelete}
                disabled={isDeleting}
              >
                {isDeleting ? 'Deleting...' : 'Delete Wallet'}
              </button>
            </div>
          </div>
        </div>
      )}
    </div>
  );
};

export default WalletCard;
