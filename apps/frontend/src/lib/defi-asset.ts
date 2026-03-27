export type DeFiAssetType = 'supplied' | 'borrowed'

export interface DeFiAssetInfo {
  underlyingSymbol: string
  type: DeFiAssetType
}

const CHAIN_PREFIXES = ['Eth', 'Bas', 'Arb', 'Opt', 'Pol', 'Ava', 'Mat']

function stripChainPrefix(remainder: string): string {
  for (const prefix of CHAIN_PREFIXES) {
    if (remainder.startsWith(prefix) && remainder.length > prefix.length) {
      return remainder.slice(prefix.length)
    }
  }
  return remainder
}

export function parseDeFiAsset(symbol: string): DeFiAssetInfo | null {
  if (!symbol) return null

  // variableDebt tokens → borrowed
  if (symbol.startsWith('variableDebt')) {
    const remainder = symbol.slice('variableDebt'.length)
    return {
      underlyingSymbol: stripChainPrefix(remainder),
      type: 'borrowed',
    }
  }

  // stableDebt tokens → borrowed
  if (symbol.startsWith('stableDebt')) {
    const remainder = symbol.slice('stableDebt'.length)
    return {
      underlyingSymbol: stripChainPrefix(remainder),
      type: 'borrowed',
    }
  }

  // aTokens → supplied
  // Must start with lowercase 'a' followed by an uppercase letter (chain prefix)
  // This avoids matching regular tokens like "AAVE"
  if (
    symbol.length > 1 &&
    symbol[0] === 'a' &&
    symbol[1] >= 'A' &&
    symbol[1] <= 'Z'
  ) {
    const remainder = symbol.slice(1)
    return {
      underlyingSymbol: stripChainPrefix(remainder),
      type: 'supplied',
    }
  }

  return null
}
