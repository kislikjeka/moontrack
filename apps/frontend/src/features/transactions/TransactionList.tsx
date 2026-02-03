import React, { useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { Link } from 'react-router-dom';
import transactionService from '../../services/transaction';
import TransactionItem from './TransactionItem';
import './Transactions.css';

interface TransactionListProps {
  walletId?: string; // Optional filter by wallet
  limit?: number; // Optional limit for embedded lists
}

export const TransactionList: React.FC<TransactionListProps> = ({
  walletId,
  limit,
}) => {
  const [page, setPage] = useState(1);
  const [filters, setFilters] = useState({
    wallet_id: walletId || '',
    asset_id: '',
    type: '',
    start_date: '',
    end_date: '',
  });

  const pageSize = limit || 20;

  // Fetch transactions with filters and pagination
  const { data, isLoading, isError, error } = useQuery({
    queryKey: ['transactions', page, pageSize, filters],
    queryFn: () =>
      transactionService.list({
        ...filters,
        page,
        page_size: pageSize,
        wallet_id: filters.wallet_id || undefined,
        asset_id: filters.asset_id || undefined,
        type: filters.type || undefined,
        start_date: filters.start_date || undefined,
        end_date: filters.end_date || undefined,
      }),
  });

  const handleFilterChange = (e: React.ChangeEvent<HTMLInputElement | HTMLSelectElement>) => {
    const { name, value } = e.target;
    setFilters((prev) => ({ ...prev, [name]: value }));
    setPage(1); // Reset to first page when filters change
  };

  const handleClearFilters = () => {
    setFilters({
      wallet_id: walletId || '',
      asset_id: '',
      type: '',
      start_date: '',
      end_date: '',
    });
    setPage(1);
  };

  const handlePageChange = (newPage: number) => {
    setPage(newPage);
    window.scrollTo({ top: 0, behavior: 'smooth' });
  };

  if (isLoading) {
    return (
      <div className="transaction-list loading">
        <div className="spinner"></div>
        <p>Loading transactions...</p>
      </div>
    );
  }

  if (isError) {
    return (
      <div className="transaction-list error">
        <p className="error-message">
          Failed to load transactions: {(error as Error).message}
        </p>
      </div>
    );
  }

  const transactions = data?.transactions || [];
  const totalPages = data ? Math.ceil(data.total / pageSize) : 0;

  return (
    <div className="transaction-list">
      <div className="transaction-list-header">
        <div className="header-title">
          <h2>Transaction History</h2>
          {data && (
            <p className="transaction-count">
              Showing {transactions.length} of {data.total} transactions
            </p>
          )}
        </div>
        <Link to="/transactions/new" className="btn btn-primary">
          + Add Transaction
        </Link>
      </div>

      {/* Filters */}
      {!limit && (
        <div className="transaction-filters">
          <div className="filter-group">
            <label htmlFor="filter-asset">Asset</label>
            <input
              type="text"
              id="filter-asset"
              name="asset_id"
              value={filters.asset_id}
              onChange={handleFilterChange}
              placeholder="e.g., BTC, ETH"
            />
          </div>

          <div className="filter-group">
            <label htmlFor="filter-type">Type</label>
            <select
              id="filter-type"
              name="type"
              value={filters.type}
              onChange={handleFilterChange}
            >
              <option value="">All Types</option>
              <option value="manual_income">Income</option>
              <option value="manual_outcome">Outcome</option>
              <option value="asset_adjustment">Adjustment</option>
            </select>
          </div>

          <div className="filter-group">
            <label htmlFor="filter-start-date">From Date</label>
            <input
              type="date"
              id="filter-start-date"
              name="start_date"
              value={filters.start_date}
              onChange={handleFilterChange}
            />
          </div>

          <div className="filter-group">
            <label htmlFor="filter-end-date">To Date</label>
            <input
              type="date"
              id="filter-end-date"
              name="end_date"
              value={filters.end_date}
              onChange={handleFilterChange}
            />
          </div>

          <button
            type="button"
            className="btn btn-secondary"
            onClick={handleClearFilters}
          >
            Clear Filters
          </button>
        </div>
      )}

      {/* Transaction List */}
      {transactions.length === 0 ? (
        <div className="empty-state">
          <p>No transactions found.</p>
          <p className="help-text">
            {filters.wallet_id || filters.asset_id || filters.type
              ? 'Try adjusting your filters.'
              : 'Record your first transaction to get started.'}
          </p>
        </div>
      ) : (
        <>
          <div className="transactions">
            {transactions.map((transaction) => (
              <TransactionItem key={transaction.id} transaction={transaction} />
            ))}
          </div>

          {/* Pagination */}
          {!limit && totalPages > 1 && (
            <div className="pagination">
              <button
                onClick={() => handlePageChange(page - 1)}
                disabled={page === 1}
                className="btn btn-secondary"
              >
                Previous
              </button>

              <span className="page-info">
                Page {page} of {totalPages}
              </span>

              <button
                onClick={() => handlePageChange(page + 1)}
                disabled={page === totalPages}
                className="btn btn-secondary"
              >
                Next
              </button>
            </div>
          )}
        </>
      )}
    </div>
  );
};

export default TransactionList;
