import { describe, it, expect } from 'vitest'
import { formatAssetLabel } from '../../src/lib/format'

// formatAssetLabel is the single rule behind "show the contract only where the
// ticker is ambiguous" (#42). It is pure string logic, so it is testable without
// a DOM — unlike the component tests in this directory, which are blocked on the
// missing DOM environment (#67).
describe('formatAssetLabel', () => {
  const contract = '0x833589fcd6edb6e08f4c7c32d4f71b54bda02913'

  it('shows the bare ticker when it names the asset uniquely', () => {
    expect(formatAssetLabel('USDC', contract, false)).toBe('USDC')
  })

  it('qualifies the ticker with a truncated contract when ambiguous', () => {
    // The whole contract would swamp the ticker it is meant to qualify.
    expect(formatAssetLabel('USDC', contract, true)).toBe('USDC · 0x8335...2913')
  })

  it('gives two same-ticker assets two distinguishable labels', () => {
    // This is the user-facing point: two "USDC" rows with different balances
    // read as an application bug until the labels differ.
    const bridged = '0xd9aaec86b65d86f6a7b5b1b0c42ffa531710b6ca'
    const a = formatAssetLabel('USDC', contract, true)
    const b = formatAssetLabel('USDC', bridged, true)
    expect(a).not.toBe(b)
  })

  it('never truncates the native sentinel', () => {
    // `native` is the registry's spelling for a chain's native coin, not an
    // address; "nati...tive" would be nonsense, and there is exactly one such
    // row per chain so it cannot collide anyway.
    expect(formatAssetLabel('ETH', 'native', true)).toBe('ETH')
    expect(formatAssetLabel('ETH', 'native', false)).toBe('ETH')
  })

  it('falls back to the contract, then to a dash, when there is no symbol', () => {
    // An asset the registry cannot describe has no ticker to show. Rendering
    // the UUID in its place would put an identifier where a label belongs.
    expect(formatAssetLabel('', contract, false)).toBe('0x8335...2913')
    expect(formatAssetLabel('', '', false)).toBe('—')
    expect(formatAssetLabel('', 'native', false)).toBe('—')
  })

  it('shows the bare ticker when ambiguity is unknown or the contract is absent', () => {
    expect(formatAssetLabel('USDC')).toBe('USDC')
    expect(formatAssetLabel('USDC', '', true)).toBe('USDC')
  })
})
