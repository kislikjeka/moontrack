import React from 'react';
import { useParams, useNavigate, Link } from 'react-router-dom';
import { useQuery } from '@tanstack/react-query';
import transactionService, { TransactionDetail as TxDetail, LedgerEntry } from '../../services/transaction';
import './Transactions.css';
import './TransactionDetail.css';

export const TransactionDetail: React.FC = () => {
  const { id } = useParams<{ id: string }>();
  const navigate = useNavigate();

  const { data: transaction, isLoading, isError, error } = useQuery({
    queryKey: ['transaction', id],
    queryFn: () => transactionService.getById(id!),
    enabled: !!id,
  });

  // Format date for display
  const formatDate = (isoDateStr: string): string => {
    const date = new Date(isoDateStr);
    return new Intl.DateTimeFormat('en-US', {
      year: 'numeric',
      month: 'long',
      day: 'numeric',
      hour: '2-digit',
      minute: '2-digit',
      second: '2-digit',
    }).format(date);
  };

  // Get direction class for styling
  const getDirectionClass = (direction: string) => {
    switch (direction) {
      case 'in':
        return 'income';
      case 'out':
        return 'outcome';
      default:
        return 'adjustment';
    }
  };

  // Get direction prefix
  const getDirectionPrefix = (direction: string) => {
    switch (direction) {
      case 'in':
        return '+';
      case 'out':
        return '-';
      default:
        return '';
    }
  };

  if (isLoading) {
    return (
      <div className="transaction-detail-page">
        <div className="transaction-detail loading">
          <div className="spinner"></div>
          <p>Loading transaction...</p>
        </div>
      </div>
    );
  }

  if (isError || !transaction) {
    return (
      <div className="transaction-detail-page">
        <div className="transaction-detail error">
          <div className="error-icon">!</div>
          <h2>Transaction not found</h2>
          <p>{(error as Error)?.message || 'The transaction you are looking for does not exist or you do not have access to it.'}</p>
          <Link to="/transactions" className="btn btn-primary">
            Back to Transactions
          </Link>
        </div>
      </div>
    );
  }

  return (
    <div className="transaction-detail-page">
      <div className="transaction-detail">
        {/* Back Navigation */}
        <button
          onClick={() => navigate('/transactions')}
          className="back-button"
          type="button"
        >
          <span className="back-arrow">←</span>
          Back to Transactions
        </button>

        {/* Transaction Header */}
        <div className={`transaction-header ${getDirectionClass(transaction.direction)}`}>
          <div className="header-content">
            <div className="type-badge">{transaction.type_label}</div>
            <div className="primary-amount">
              {getDirectionPrefix(transaction.direction)}{transaction.display_amount}
            </div>
            {transaction.usd_value && (
              <div className="usd-value">
                ≈ ${parseFloat(transaction.usd_value).toLocaleString('en-US', {
                  minimumFractionDigits: 2,
                  maximumFractionDigits: 2,
                })} USD
              </div>
            )}
          </div>
          <div className={`status-indicator ${transaction.status.toLowerCase()}`}>
            {transaction.status === 'COMPLETED' ? '✓' : '✕'} {transaction.status}
          </div>
        </div>

        {/* Transaction Details Card */}
        <div className="details-card">
          <h3>Details</h3>
          <dl className="details-grid">
            <div className="detail-item">
              <dt>Wallet</dt>
              <dd>{transaction.wallet_name}</dd>
            </div>
            <div className="detail-item">
              <dt>Asset</dt>
              <dd>{transaction.asset_symbol}</dd>
            </div>
            <div className="detail-item">
              <dt>Occurred</dt>
              <dd>{formatDate(transaction.occurred_at)}</dd>
            </div>
            <div className="detail-item">
              <dt>Recorded</dt>
              <dd>{formatDate(transaction.recorded_at)}</dd>
            </div>
            <div className="detail-item">
              <dt>Source</dt>
              <dd>{transaction.source}</dd>
            </div>
            {transaction.external_id && (
              <div className="detail-item full-width">
                <dt>External ID</dt>
                <dd className="mono">{transaction.external_id}</dd>
              </div>
            )}
          </dl>
          {transaction.notes && (
            <div className="notes-section">
              <h4>Notes</h4>
              <p>{transaction.notes}</p>
            </div>
          )}
        </div>

        {/* Ledger Entries Card */}
        <div className="entries-card">
          <h3>Ledger Entries</h3>
          <p className="entries-description">
            Double-entry accounting: every transaction creates balanced debit and credit entries.
          </p>
          <div className="entries-table-wrapper">
            <table className="entries-table">
              <thead>
                <tr>
                  <th>Account</th>
                  <th>Type</th>
                  <th className="text-right">Debit</th>
                  <th className="text-right">Credit</th>
                  <th className="text-right">USD Value</th>
                </tr>
              </thead>
              <tbody>
                {transaction.entries.map((entry: LedgerEntry) => (
                  <tr key={entry.id}>
                    <td>
                      <div className="account-cell">
                        <span className="account-label">{entry.account_label}</span>
                        <span className="account-code">{entry.account_code}</span>
                      </div>
                    </td>
                    <td>
                      <span className={`entry-type ${entry.entry_type}`}>
                        {entry.entry_type.replace('_', ' ')}
                      </span>
                    </td>
                    <td className="text-right mono">
                      {entry.debit_credit === 'DEBIT' ? entry.display_amount : '-'}
                    </td>
                    <td className="text-right mono">
                      {entry.debit_credit === 'CREDIT' ? entry.display_amount : '-'}
                    </td>
                    <td className="text-right mono">
                      {entry.usd_value ? `$${parseFloat(entry.usd_value).toLocaleString('en-US', { minimumFractionDigits: 2, maximumFractionDigits: 2 })}` : '-'}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </div>

        {/* Transaction ID */}
        <div className="transaction-id">
          <span className="label">Transaction ID:</span>
          <span className="id mono">{transaction.id}</span>
        </div>
      </div>
    </div>
  );
};

export default TransactionDetail;
