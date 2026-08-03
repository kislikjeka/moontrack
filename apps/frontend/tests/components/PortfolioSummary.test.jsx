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

  // Updated for #79: an unreadable total is not a total of zero. This used to
  // assert "$0.00", which is precisely the confusion the issue removes — a
  // portfolio whose value could not be determined must not report itself as
  // empty. A real zero is covered by the other cases here.
  test('renders an unreadable total as a dash, not as $0.00', () => {
    const invalidPortfolio = {
      total_usd_value: 'invalid',
      total_assets: 0,
      asset_holdings: [],
      wallet_balances: [],
      last_updated: '2026-01-11T10:30:00Z',
    };
    render(<PortfolioSummary portfolio={invalidPortfolio} />);
    expect(screen.getByText('—')).toBeInTheDocument();
    expect(screen.queryByText(/\$0\.00/i)).not.toBeInTheDocument();
  });

  // The partial-total notice (#79). The backend counts the lots it could not
  // price; without surfacing them the user reads a confident total that silently
  // omits some holdings — the same defect as the "$0.00" dash, one level up.
  describe('partial total notice', () => {
    test('stays silent when every lot is priced', () => {
      render(
        <PortfolioSummary
          portfolio={{
            ...mockPortfolio,
            pnl_is_partial: false,
            pending_lot_count: 0,
            unpriceable_lot_count: 0,
          }}
        />
      );
      expect(screen.queryByText(/partial/i)).not.toBeInTheDocument();
    });

    test('names both causes when lots are pending and unpriceable', () => {
      render(
        <PortfolioSummary
          portfolio={{
            ...mockPortfolio,
            pnl_is_partial: true,
            pending_lot_count: 3,
            unpriceable_lot_count: 1,
          }}
        />
      );
      expect(screen.getByText(/excludes 4 lots/i)).toBeInTheDocument();
      expect(screen.getByText(/3 awaiting pricing/i)).toBeInTheDocument();
      expect(screen.getByText(/1 with no price source/i)).toBeInTheDocument();
    });

    test('still warns when lots are only unpriceable', () => {
      // The backend derives pnl_is_partial from PENDING lots alone, so it is
      // false here. Gating the notice on that flag would hide the one case that
      // never resolves on its own — so the notice keys off the counts.
      render(
        <PortfolioSummary
          portfolio={{
            ...mockPortfolio,
            pnl_is_partial: false,
            pending_lot_count: 0,
            unpriceable_lot_count: 5,
          }}
        />
      );
      expect(screen.getByText(/excludes 5 lots/i)).toBeInTheDocument();
      expect(screen.getByText(/no price source/i)).toBeInTheDocument();
    });

    test('stays silent for a backend that does not send the counts', () => {
      // Absent is read as "nothing known to be missing", not as a warning.
      render(<PortfolioSummary portfolio={mockPortfolio} />);
      expect(screen.queryByText(/partial/i)).not.toBeInTheDocument();
    });
  });

  test('shows message when no portfolio data', () => {
    render(<PortfolioSummary />);
    expect(screen.getByText(/No portfolio data available/i)).toBeInTheDocument();
  });
});
