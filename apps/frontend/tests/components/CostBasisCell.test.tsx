import { describe, it, expect } from 'vitest'
import { fireEvent, render, screen } from '@testing-library/react'
import { CostBasisCell } from '../../src/components/domain/CostBasisCell'

// The rendered half of #79. formatMoney.test.ts pins the string contract; this
// pins what a user actually sees in the Cost/Unit column, which is where the
// defect was reported: two lots side by side, one priced and one not, both
// reading as dollar amounts.
describe('CostBasisCell', () => {
  it('renders a known cost basis as a dollar amount', () => {
    render(<CostBasisCell value="1566.00" />)
    expect(screen.getByText('$1,566.00')).toBeTruthy()
  })

  it('renders a genuine zero as $0.00, not as a dash', () => {
    render(<CostBasisCell value="0.00" />)
    expect(screen.getByText('$0.00')).toBeTruthy()
  })

  it('renders an unpriced lot as a dash rather than a zero', () => {
    // The reported bug: auto_cost_basis_source=lending_carry_over with no price
    // showed "$0.00" next to a real "$1,566.00" and read as "worth nothing".
    const { container } = render(<CostBasisCell value={null} status="pending" />)
    expect(container.textContent).toContain('—')
    expect(container.textContent).not.toContain('$')
  })

  it('distinguishes a price that has not arrived from one that never will', async () => {
    // Both statuses render the same dash, so the tooltip is the only thing
    // telling the user whether to wait for a backfill or to enter the cost
    // basis by hand. The two messages must not collapse into one.
    // Radix opens the tooltip on focus as well as on hover, and focus is the
    // one @testing-library/react can drive without pulling in user-event.
    // Radix renders the open bubble plus an offscreen copy for screen readers,
    // so the copy legitimately appears more than once — findAll, not find.
    const pending = render(<CostBasisCell value={null} status="pending" />)
    fireEvent.focus(pending.getByText('—'))
    expect((await screen.findAllByText(/not fetched yet/i)).length).toBeGreaterThan(0)
    pending.unmount()

    const unpriceable = render(<CostBasisCell value={null} status="unpriceable" />)
    fireEvent.focus(unpriceable.getByText('—'))
    expect((await screen.findAllByText(/no price source/i)).length).toBeGreaterThan(0)
    expect(screen.queryAllByText(/not fetched yet/i)).toHaveLength(0)
  })

  it('marks an overridden value so it is not read as a fetched price', () => {
    const { container } = render(
      <CostBasisCell value="10.00" isOverridden />
    )
    expect(container.textContent).toContain('$10.00')
    expect(container.textContent).toContain('override')
  })
})
