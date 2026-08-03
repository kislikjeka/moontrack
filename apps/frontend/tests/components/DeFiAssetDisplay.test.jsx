import { describe, expect, test } from 'vitest'
import { render, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { WalletHoldings } from '@/features/wallets/WalletHoldings'
import { LendingPositionCard } from '@/features/wallets/components/LendingPositionCard'

// These cover the frontend half of #57: the DeFi-prefix parser is gone, so both
// views must render the symbol the backend sent, verbatim.
//
// The parser used to decode `aBasWETH` into "WETH" + a Supplied badge by
// stripping a lowercase `a` and then a chain prefix from a hardcoded list
// (['Eth','Bas','Arb','Opt','Pol','Ava','Mat']) — an operation with no
// counterpart anywhere in the backend. It is unnecessary now: a protocol
// receipt never reaches the ledger, so no receipt ticker can arrive here, and
// what does arrive is already the principal symbol.

// The symbol is what gets rendered; asset_id is the registry UUID and is only
// the key (#42). Passing the ticker as asset_id — as this fixture did before —
// would no longer exercise the label at all.
function holdingGroup(symbol) {
  return {
    asset_id: `id-of-${symbol}`,
    asset_symbol: symbol,
    asset_contract: '0x4200000000000000000000000000000000000006',
    symbol_ambiguous: false,
    total_amount: '1.5',
    total_usd_value: '4500.00',
    price: '3000.00',
    chains: [{ chain_id: 'base', amount: '1.5', usd_value: '4500.00' }],
  }
}

describe('WalletHoldings — no DeFi prefix parsing', () => {
  test('renders the asset symbol exactly as sent', () => {
    render(
      <MemoryRouter>
        <WalletHoldings walletId="w1" holdings={[holdingGroup('WETH')]} />
      </MemoryRouter>
    )

    expect(screen.getByText('WETH')).toBeInTheDocument()
  })

  test('does not decode a receipt-shaped ticker or add a Supplied/Borrowed badge', () => {
    // Even if such a ticker somehow arrived, the view must show it as-is
    // rather than inventing an underlying symbol from its spelling.
    render(
      <MemoryRouter>
        <WalletHoldings walletId="w1" holdings={[holdingGroup('aBasWETH')]} />
      </MemoryRouter>
    )

    expect(screen.getByText('aBasWETH')).toBeInTheDocument()
    expect(screen.queryByText('Supplied')).not.toBeInTheDocument()
    expect(screen.queryByText('Borrowed')).not.toBeInTheDocument()
  })
})

describe('LendingPositionCard — principal symbol shown verbatim', () => {
  const position = {
    id: 'p1',
    wallet_id: 'w1',
    chain_id: 'base',
    protocol: 'Aave V3',
    interest_earned_usd: '0',
    status: 'active',
    opened_at: '2026-07-01T12:00:00Z',
    assets: [
      {
        side: 'supply',
        asset: 'cbBTC',
        amount: '199560',
        decimals: 8,
        total_in: '199560',
        total_out: '0',
        total_in_usd: '0',
        total_out_usd: '0',
      },
    ],
  }

  test('shows the principal asset the position is recorded against', () => {
    render(<LendingPositionCard position={position} />)

    // The supplied asset is the principal, not a decoded receipt.
    expect(screen.getByText('cbBTC')).toBeInTheDocument()
    expect(screen.getByText('Supplied')).toBeInTheDocument()
    expect(screen.getByText('Aave V3')).toBeInTheDocument()
  })
})
