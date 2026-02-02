import React from 'react';
import { useQuery } from '@tanstack/react-query';
import { getPortfolioSummary } from '../../services/portfolio';
import PortfolioSummary from './PortfolioSummary';
import AssetBreakdown from './AssetBreakdown';
import './Dashboard.css';

/**
 * Dashboard component - Main portfolio overview page
 * Shows total portfolio value and breakdown by assets
 */
const Dashboard: React.FC = () => {
  const {
    data: portfolio,
    isLoading,
    isError,
    error,
    refetch,
  } = useQuery({
    queryKey: ['portfolio'],
    queryFn: getPortfolioSummary,
    refetchInterval: 60000, // Refetch every 60 seconds
    staleTime: 30000, // Consider data stale after 30 seconds
  });

  if (isLoading) {
    return (
      <div className="dashboard">
        <div className="dashboard-loading">
          <div className="spinner"></div>
          <p>Loading your portfolio...</p>
        </div>
      </div>
    );
  }

  if (isError) {
    return (
      <div className="dashboard">
        <div className="dashboard-error">
          <h2>Failed to load portfolio</h2>
          <p>{error instanceof Error ? error.message : 'An error occurred'}</p>
          <button onClick={() => refetch()} className="btn btn-primary">
            Retry
          </button>
        </div>
      </div>
    );
  }

  // Empty state - no assets
  if (!portfolio || portfolio.total_assets === 0) {
    return (
      <div className="dashboard">
        <div className="dashboard-empty">
          <div className="empty-icon">📊</div>
          <h2>Welcome to MoonTrack!</h2>
          <p>You haven't added any assets yet.</p>
          <p>Get started by creating your first wallet and adding some cryptocurrency holdings.</p>
          <a href="/wallets/new" className="btn btn-primary">
            Add Your First Wallet
          </a>
        </div>
      </div>
    );
  }

  return (
    <div className="dashboard">
      <div className="dashboard-header">
        <h1>Portfolio Overview</h1>
        <button onClick={() => refetch()} className="btn btn-secondary btn-refresh">
          <span className="refresh-icon">🔄</span> Refresh
        </button>
      </div>

      <PortfolioSummary portfolio={portfolio} />

      <AssetBreakdown assetHoldings={portfolio.asset_holdings} />

      <div className="dashboard-footer">
        <p className="last-updated">
          Last updated: {new Date(portfolio.last_updated).toLocaleString()}
        </p>
        <p className="disclaimer">
          Prices are indicative and may not reflect real-time market values.
        </p>
      </div>
    </div>
  );
};

export default Dashboard;
