import { render, screen, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { BrowserRouter } from 'react-router-dom';
import { vi } from 'vitest';
import Dashboard from '../../src/features/dashboard/Dashboard';
import * as portfolioService from '../../src/services/portfolio';

// Mock the portfolio service
vi.mock('../../src/services/portfolio', () => ({
  getPortfolioSummary: vi.fn(),
}));

const createWrapper = () => {
  const queryClient = new QueryClient({
    defaultOptions: {
      queries: { retry: false },
      mutations: { retry: false },
    },
  });

  return ({ children }) => (
    <QueryClientProvider client={queryClient}>
      <BrowserRouter>{children}</BrowserRouter>
    </QueryClientProvider>
  );
};

const mockPortfolioData = {
  total_usd_value: '1256785000000000', // $12,567,850.00 scaled by 10^8
  total_assets: 3,
  asset_holdings: [
    {
      asset_id: 'BTC',
      total_amount: '200000000', // 2 BTC in satoshis
      usd_value: '9000000000000000', // $90,000.00 scaled
      current_price: '4500000000000000', // $45,000.00 scaled
    },
    {
      asset_id: 'ETH',
      total_amount: '15000000000000000000', // 15 ETH in wei
      usd_value: '4500000000000000', // $45,000.00 scaled
      current_price: '300000000000000', // $3,000.00 scaled
    },
  ],
  wallet_balances: [],
  last_updated: '2026-01-11T10:30:00Z',
};

describe('Dashboard', () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  test('renders dashboard with loading state initially', () => {
    // Make the promise never resolve to keep loading state
    portfolioService.getPortfolioSummary.mockImplementation(() => new Promise(() => {}));

    render(<Dashboard />, { wrapper: createWrapper() });
    expect(screen.getByText(/Loading your portfolio/i)).toBeInTheDocument();
  });

  test('renders portfolio summary and asset breakdown when data loads', async () => {
    portfolioService.getPortfolioSummary.mockResolvedValueOnce(mockPortfolioData);

    render(<Dashboard />, { wrapper: createWrapper() });

    await waitFor(() => {
      expect(screen.getByText(/Portfolio Overview/i)).toBeInTheDocument();
    });

    // Should show the total value
    expect(screen.getByText(/Total Portfolio Value/i)).toBeInTheDocument();

    // Should show asset holdings section
    expect(screen.getByText(/Asset Holdings/i)).toBeInTheDocument();
    expect(screen.getByText('BTC')).toBeInTheDocument();
    expect(screen.getByText('ETH')).toBeInTheDocument();
  });

  test('displays empty state for new users with no assets', async () => {
    const emptyPortfolio = {
      total_usd_value: '0',
      total_assets: 0,
      asset_holdings: [],
      wallet_balances: [],
      last_updated: '2026-01-11T10:30:00Z',
    };
    portfolioService.getPortfolioSummary.mockResolvedValueOnce(emptyPortfolio);

    render(<Dashboard />, { wrapper: createWrapper() });

    await waitFor(() => {
      expect(screen.getByText(/Welcome to MoonTrack!/i)).toBeInTheDocument();
    });

    expect(screen.getByText(/You haven't added any assets yet/i)).toBeInTheDocument();
    expect(screen.getByText(/Add Your First Wallet/i)).toBeInTheDocument();
  });

  test('displays error message when portfolio fails to load', async () => {
    portfolioService.getPortfolioSummary.mockRejectedValueOnce(new Error('Network error'));

    render(<Dashboard />, { wrapper: createWrapper() });

    await waitFor(() => {
      expect(screen.getByText(/Failed to load portfolio/i)).toBeInTheDocument();
    });

    expect(screen.getByText(/Network error/i)).toBeInTheDocument();
    expect(screen.getByRole('button', { name: /Retry/i })).toBeInTheDocument();
  });

  test('renders refresh button in loaded state', async () => {
    portfolioService.getPortfolioSummary.mockResolvedValueOnce(mockPortfolioData);

    render(<Dashboard />, { wrapper: createWrapper() });

    await waitFor(() => {
      expect(screen.getByText(/Portfolio Overview/i)).toBeInTheDocument();
    });

    expect(screen.getByRole('button', { name: /Refresh/i })).toBeInTheDocument();
  });

  test('shows last updated timestamp', async () => {
    portfolioService.getPortfolioSummary.mockResolvedValueOnce(mockPortfolioData);

    render(<Dashboard />, { wrapper: createWrapper() });

    await waitFor(() => {
      expect(screen.getByText(/Last updated:/i)).toBeInTheDocument();
    });
  });

  test('shows disclaimer about price accuracy', async () => {
    portfolioService.getPortfolioSummary.mockResolvedValueOnce(mockPortfolioData);

    render(<Dashboard />, { wrapper: createWrapper() });

    await waitFor(() => {
      expect(screen.getByText(/Prices are indicative/i)).toBeInTheDocument();
    });
  });
});
