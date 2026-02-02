import { render, screen } from '@testing-library/react';
import AssetBreakdown from '../../src/features/dashboard/AssetBreakdown';

describe('AssetBreakdown', () => {
  // Mock asset holdings matching the AssetHolding interface
  // USD values are scaled by 10^8 for precision (bigint string format)
  // total_amount is in native units (satoshis for BTC, wei for ETH, etc.)
  const mockAssetHoldings = [
    {
      asset_id: 'BTC',
      total_amount: '200000000', // 2 BTC in satoshis (8 decimals)
      usd_value: '9000000000000', // $90,000.00 scaled by 10^8
      current_price: '4500000000000', // $45,000.00 per BTC scaled by 10^8
    },
    {
      asset_id: 'ETH',
      total_amount: '15000000000000000000', // 15 ETH in wei (18 decimals)
      usd_value: '4500000000000', // $45,000.00 scaled by 10^8
      current_price: '300000000000', // $3,000.00 per ETH scaled by 10^8
    },
    {
      asset_id: 'USDC',
      total_amount: '1000000000', // 1000 USDC (6 decimals)
      usd_value: '100000000000', // $1,000.00 scaled by 10^8
      current_price: '100000000', // $1.00 per USDC scaled by 10^8
    },
  ];

  test('renders all assets in the breakdown', () => {
    render(<AssetBreakdown assetHoldings={mockAssetHoldings} />);
    expect(screen.getByText('BTC')).toBeInTheDocument();
    expect(screen.getByText('ETH')).toBeInTheDocument();
    expect(screen.getByText('USDC')).toBeInTheDocument();
  });

  test('renders Asset Holdings header', () => {
    render(<AssetBreakdown assetHoldings={mockAssetHoldings} />);
    expect(screen.getByText('Asset Holdings')).toBeInTheDocument();
  });

  test('displays asset amounts correctly', () => {
    render(<AssetBreakdown assetHoldings={mockAssetHoldings} />);
    // BTC: 2 BTC
    expect(screen.getByText(/2 BTC/)).toBeInTheDocument();
    // ETH: 15 ETH
    expect(screen.getByText(/15 ETH/)).toBeInTheDocument();
    // USDC: 1000 USDC
    expect(screen.getByText(/1000 USDC/)).toBeInTheDocument();
  });

  test('displays USD values for each asset', () => {
    render(<AssetBreakdown assetHoldings={mockAssetHoldings} />);
    // BTC value: $90,000.00
    expect(screen.getByText('$90,000.00')).toBeInTheDocument();
    // ETH value and BTC price are both $45,000.00
    expect(screen.getAllByText('$45,000.00')).toHaveLength(2);
    // USDC value: $1,000.00
    expect(screen.getByText('$1,000.00')).toBeInTheDocument();
  });

  test('displays current price for each asset', () => {
    render(<AssetBreakdown assetHoldings={mockAssetHoldings} />);
    // BTC price: $45,000.00 (same as ETH value, so test presence)
    // ETH price: $3,000.00
    expect(screen.getByText('$3,000.00')).toBeInTheDocument();
    // USDC price: $1.00
    expect(screen.getByText('$1.00')).toBeInTheDocument();
  });

  test('returns null when no assets (empty array)', () => {
    const { container } = render(<AssetBreakdown assetHoldings={[]} />);
    expect(container.firstChild).toBeNull();
  });

  test('displays asset icons', () => {
    render(<AssetBreakdown assetHoldings={mockAssetHoldings} />);
    // Check for asset icons (symbols)
    expect(screen.getByText('\u20BF')).toBeInTheDocument(); // Bitcoin symbol
    expect(screen.getByText('\u039E')).toBeInTheDocument(); // Ethereum symbol
  });

  test('displays value and price labels', () => {
    render(<AssetBreakdown assetHoldings={mockAssetHoldings} />);
    // Should have Value: and Price: labels
    const valueLabels = screen.getAllByText('Value:');
    const priceLabels = screen.getAllByText('Price:');
    expect(valueLabels.length).toBe(3);
    expect(priceLabels.length).toBe(3);
  });

  test('handles single asset', () => {
    const singleAsset = [mockAssetHoldings[0]];
    render(<AssetBreakdown assetHoldings={singleAsset} />);
    expect(screen.getByText('BTC')).toBeInTheDocument();
    expect(screen.queryByText('ETH')).not.toBeInTheDocument();
  });
});
