import { render, screen } from '@testing-library/react';
import { PortfolioSummary } from '../../src/features/dashboard/PortfolioSummary';

describe('PortfolioSummary', () => {
  // Mock portfolio data matching the PortfolioSummary type interface
  // Values are human-readable decimal strings (formatted by backend)
  const mockPortfolio = {
    total_usd_value: '125678.50',
    total_assets: 3,
    asset_holdings: [],
    wallet_balances: [],
    last_updated: '2026-01-11T10:30:00Z',
  };

  test('renders total portfolio value', () => {
    render(<PortfolioSummary portfolio={mockPortfolio} />);
    expect(screen.getByText(/\$125,678\.50/i)).toBeInTheDocument();
  });

  test('renders Total Value label', () => {
    render(<PortfolioSummary portfolio={mockPortfolio} />);
    expect(screen.getByText(/Total Value/i)).toBeInTheDocument();
  });

  test('displays asset count', () => {
    render(<PortfolioSummary portfolio={mockPortfolio} />);
    // The component displays the count under "Assets" label
    expect(screen.getByText('3')).toBeInTheDocument();
    expect(screen.getByText('Assets')).toBeInTheDocument();
  });

  test('displays singular asset count for one asset', () => {
    const singleAssetPortfolio = {
      ...mockPortfolio,
      total_assets: 1,
    };
    render(<PortfolioSummary portfolio={singleAssetPortfolio} />);
    expect(screen.getByText('1')).toBeInTheDocument();
  });

  test('displays zero balance for empty portfolio', () => {
    const emptyPortfolio = {
      total_usd_value: '0',
      total_assets: 0,
      asset_holdings: [],
      wallet_balances: [],
      last_updated: '2026-01-11T10:30:00Z',
    };
    render(<PortfolioSummary portfolio={emptyPortfolio} />);
    expect(screen.getByText(/\$0\.00/i)).toBeInTheDocument();
  });

  test('formats large numbers with commas', () => {
    const largePortfolio = {
      total_usd_value: '1250000000.00',
      total_assets: 5,
      asset_holdings: [],
      wallet_balances: [],
      last_updated: '2026-01-11T10:30:00Z',
    };
    render(<PortfolioSummary portfolio={largePortfolio} />);
    expect(screen.getByText(/\$1,250,000,000\.00/i)).toBeInTheDocument();
  });

  test('handles invalid total_usd_value gracefully', () => {
    const invalidPortfolio = {
      total_usd_value: 'invalid',
      total_assets: 0,
      asset_holdings: [],
      wallet_balances: [],
      last_updated: '2026-01-11T10:30:00Z',
    };
    render(<PortfolioSummary portfolio={invalidPortfolio} />);
    expect(screen.getByText(/\$0\.00/i)).toBeInTheDocument();
  });

  test('shows message when no portfolio data', () => {
    render(<PortfolioSummary />);
    expect(screen.getByText(/No portfolio data available/i)).toBeInTheDocument();
  });
});
