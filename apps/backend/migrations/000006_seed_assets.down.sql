-- Remove seeded assets (keep only user-created ones)
DELETE FROM assets WHERE market_cap_rank IS NOT NULL OR coingecko_id IN (
    'bitcoin', 'ethereum', 'binancecoin', 'solana', 'ripple', 'cardano', 'dogecoin', 'tron',
    'avalanche-2', 'the-open-network', 'chainlink', 'polkadot', 'matic-network', 'litecoin',
    'bitcoin-cash', 'shiba-inu', 'cosmos', 'uniswap', 'ethereum-classic', 'stellar',
    'tether', 'tether-solana', 'tether-polygon', 'tether-bnb', 'tether-arbitrum',
    'usd-coin', 'usd-coin-solana', 'bridged-usdc-polygon', 'usd-coin-bnb', 'usd-coin-arbitrum',
    'dai', 'dai-polygon',
    'aave', 'maker', 'curve-dao-token', 'lido-dao', 'havven', 'compound-governance-token', 'the-graph',
    'arbitrum', 'optimism',
    'near', 'filecoin', 'aptos', 'internet-computer', 'hedera-hashgraph', 'vechain', 'immutable-x',
    'injective-protocol', 'algorand', 'fantom', 'the-sandbox', 'decentraland', 'axie-infinity',
    'elrond-erd-2', 'theta-token', 'flow', 'tezos'
);
