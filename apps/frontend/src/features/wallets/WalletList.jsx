import { useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { getWallets } from '../../services/wallet';
import WalletCard from './WalletCard';
import CreateWalletForm from './CreateWalletForm';

/**
 * WalletList component
 * Displays a list of user's wallets with create option
 */
const WalletList = () => {
  const [showCreateForm, setShowCreateForm] = useState(false);

  // Fetch wallets using TanStack Query
  const {
    data: wallets,
    isLoading,
    isError,
    error,
    refetch,
  } = useQuery({
    queryKey: ['wallets'],
    queryFn: getWallets,
  });

  const handleWalletCreated = () => {
    setShowCreateForm(false);
    refetch(); // Refresh wallet list
  };

  if (isLoading) {
    return (
      <div className="wallet-list-loading">
        <div className="spinner"></div>
        <p>Loading wallets...</p>
      </div>
    );
  }

  if (isError) {
    return (
      <div className="wallet-list-error">
        <p>Error loading wallets: {error.message}</p>
        <button onClick={() => refetch()}>Retry</button>
      </div>
    );
  }

  return (
    <div className="wallet-list">
      <div className="wallet-list-header">
        <h2>My Wallets</h2>
        <button
          className="btn-primary"
          onClick={() => setShowCreateForm(true)}
        >
          + Create Wallet
        </button>
      </div>

      {showCreateForm && (
        <div className="create-wallet-modal">
          <div className="modal-overlay" onClick={() => setShowCreateForm(false)}></div>
          <div className="modal-content">
            <div className="modal-header">
              <h3>Create New Wallet</h3>
              <button
                className="modal-close"
                onClick={() => setShowCreateForm(false)}
              >
                ×
              </button>
            </div>
            <CreateWalletForm
              onSuccess={handleWalletCreated}
              onCancel={() => setShowCreateForm(false)}
            />
          </div>
        </div>
      )}

      {wallets && wallets.length === 0 ? (
        <div className="wallet-list-empty">
          <p>No wallets yet. Create your first wallet to get started!</p>
        </div>
      ) : (
        <div className="wallet-grid">
          {wallets?.map((wallet) => (
            <WalletCard key={wallet.id} wallet={wallet} onUpdate={refetch} />
          ))}
        </div>
      )}
    </div>
  );
};

export default WalletList;
