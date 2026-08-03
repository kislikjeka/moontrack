import { describe, it, expect } from 'vitest'
import {
  EM_DASH,
  formatCrypto,
  formatPercent,
  formatUSD,
  formatUSDCompact,
} from '../../src/lib/format'

// The core of #79: the formatters are the last place where "we do not know this
// value" can quietly become "$0.00". The backend now sends null for an unresolved
// price, so every one of these must keep absence and zero apart. These are pure
// string functions, testable without a DOM.
describe('formatUSD', () => {
  it('renders an unknown value as an em dash, never as a zero', () => {
    // This is the defect verbatim: a lot the backend could not price used to
    // read "$0.00" — indistinguishable from a lot genuinely acquired for free.
    expect(formatUSD(null)).toBe(EM_DASH)
    expect(formatUSD(undefined)).toBe(EM_DASH)
    expect(formatUSD('')).toBe(EM_DASH)
    expect(formatUSD('not a number')).toBe(EM_DASH)
    expect(formatUSD(NaN)).toBe(EM_DASH)
  })

  it('still renders a genuine zero as $0.00', () => {
    // The other half of the contract. A real zero is a real assertion about
    // money and must survive the change untouched, in every spelling the
    // backend uses for it.
    expect(formatUSD(0)).toBe('$0.00')
    expect(formatUSD('0')).toBe('$0.00')
    expect(formatUSD('0.00')).toBe('$0.00')
    expect(formatUSD('0.000000')).toBe('$0.00')
  })

  it('formats ordinary and negative amounts', () => {
    expect(formatUSD('1566')).toBe('$1,566.00')
    expect(formatUSD(1234.567)).toBe('$1,234.57')
    expect(formatUSD('-42.5')).toBe('-$42.50')
  })
})

describe('formatUSDCompact', () => {
  it('separates unknown from zero', () => {
    expect(formatUSDCompact(null)).toBe(EM_DASH)
    expect(formatUSDCompact('')).toBe(EM_DASH)
    expect(formatUSDCompact(0)).toBe('$0')
    expect(formatUSDCompact('0')).toBe('$0')
  })
})

describe('formatPercent', () => {
  it('separates unknown from zero', () => {
    // An unknown percentage rendered as "+0%" claimed a flat return that was
    // never measured.
    expect(formatPercent(null)).toBe(EM_DASH)
    expect(formatPercent(undefined)).toBe(EM_DASH)
    expect(formatPercent('')).toBe(EM_DASH)
    expect(formatPercent(0)).toBe('+0.00%')
    expect(formatPercent('-3.5')).toBe('-3.50%')
  })
})

describe('formatCrypto', () => {
  it('separates unknown from zero', () => {
    expect(formatCrypto(null)).toBe(EM_DASH)
    expect(formatCrypto('')).toBe(EM_DASH)
    expect(formatCrypto(0)).toBe('0')
    expect(formatCrypto('0')).toBe('0')
  })
})
