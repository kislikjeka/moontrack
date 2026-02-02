import React from 'react';
import { PortfolioSummary as PortfolioSummaryType } from '../../services/portfolio';
import './PortfolioSummary.css';

interface PortfolioSummaryProps {
  portfolio: PortfolioSummaryType;
}

/**
 * PortfolioSummary component - Displays total portfolio balance
 * Shows the total USD value of all assets
 */
const PortfolioSummary: React.FC<PortfolioSummaryProps> = ({ portfolio }) => {
  // Convert big.Int string to formatted USD value
  // Assuming total_usd_value is scaled by 10^8
  const formatUSD = (value: string): string => {
    try {
      const bigIntValue = BigInt(value);
      const dollars = Number(bigIntValue) / 100000000; // Divide by 10^8
      return new Intl.NumberFormat('en-US', {
        style: 'currency',
        currency: 'USD',
        minimumFractionDigits: 2,
        maximumFractionDigits: 2,
      }).format(dollars);
    } catch (e) {
      return '$0.00';
    }
  };

  return (
    <div className="portfolio-summary">
      <div className="summary-card">
        <div className="summary-label">Total Portfolio Value</div>
        <div className="summary-value">{formatUSD(portfolio.total_usd_value)}</div>
        <div className="summary-meta">
          <span className="asset-count">
            {portfolio.total_assets} {portfolio.total_assets === 1 ? 'Asset' : 'Assets'}
          </span>
        </div>
      </div>
    </div>
  );
};

export default PortfolioSummary;
