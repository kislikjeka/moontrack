import { describe, expect, test } from 'vitest'
import { parseDeFiAsset } from '@/lib/defi-asset'

describe('parseDeFiAsset', () => {
  describe('aTokens (supplied)', () => {
    test('aEthWETH → WETH supplied', () => {
      expect(parseDeFiAsset('aEthWETH')).toEqual({
        underlyingSymbol: 'WETH',
        type: 'supplied',
      })
    })

    test('aBasUSDC → USDC supplied', () => {
      expect(parseDeFiAsset('aBasUSDC')).toEqual({
        underlyingSymbol: 'USDC',
        type: 'supplied',
      })
    })

    test('aArbDAI → DAI supplied', () => {
      expect(parseDeFiAsset('aArbDAI')).toEqual({
        underlyingSymbol: 'DAI',
        type: 'supplied',
      })
    })

    test('aOptUSDT → USDT supplied', () => {
      expect(parseDeFiAsset('aOptUSDT')).toEqual({
        underlyingSymbol: 'USDT',
        type: 'supplied',
      })
    })

    test('aPolWMATIC → WMATIC supplied', () => {
      expect(parseDeFiAsset('aPolWMATIC')).toEqual({
        underlyingSymbol: 'WMATIC',
        type: 'supplied',
      })
    })
  })

  describe('variableDebt tokens (borrowed)', () => {
    test('variableDebtBasUSDC → USDC borrowed', () => {
      expect(parseDeFiAsset('variableDebtBasUSDC')).toEqual({
        underlyingSymbol: 'USDC',
        type: 'borrowed',
      })
    })

    test('variableDebtEthWETH → WETH borrowed', () => {
      expect(parseDeFiAsset('variableDebtEthWETH')).toEqual({
        underlyingSymbol: 'WETH',
        type: 'borrowed',
      })
    })
  })

  describe('stableDebt tokens (borrowed)', () => {
    test('stableDebtEthDAI → DAI borrowed', () => {
      expect(parseDeFiAsset('stableDebtEthDAI')).toEqual({
        underlyingSymbol: 'DAI',
        type: 'borrowed',
      })
    })
  })

  describe('regular assets (no match)', () => {
    test('WETH → null', () => {
      expect(parseDeFiAsset('WETH')).toBeNull()
    })

    test('USDC → null', () => {
      expect(parseDeFiAsset('USDC')).toBeNull()
    })

    test('AAVE → null (should not match despite starting with A)', () => {
      expect(parseDeFiAsset('AAVE')).toBeNull()
    })

    test('BTC → null', () => {
      expect(parseDeFiAsset('BTC')).toBeNull()
    })

    test('empty string → null', () => {
      expect(parseDeFiAsset('')).toBeNull()
    })
  })
})
