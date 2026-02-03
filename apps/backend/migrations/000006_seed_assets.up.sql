-- Seed initial asset list (top 50 cryptocurrencies + multi-chain stablecoins)
INSERT INTO assets (symbol, name, coingecko_id, decimals, asset_type, chain_id, contract_address, market_cap_rank, is_active)
VALUES
    -- Top 20 by market cap (native L1 coins - chain_id NULL)
    ('BTC', 'Bitcoin', 'bitcoin', 8, 'crypto', NULL, NULL, 1, true),
    ('ETH', 'Ethereum', 'ethereum', 18, 'crypto', NULL, NULL, 2, true),
    ('BNB', 'BNB', 'binancecoin', 18, 'crypto', NULL, NULL, 4, true),
    ('SOL', 'Solana', 'solana', 9, 'crypto', NULL, NULL, 5, true),
    ('XRP', 'XRP', 'ripple', 6, 'crypto', NULL, NULL, 6, true),
    ('ADA', 'Cardano', 'cardano', 6, 'crypto', NULL, NULL, 9, true),
    ('DOGE', 'Dogecoin', 'dogecoin', 8, 'crypto', NULL, NULL, 10, true),
    ('TRX', 'TRON', 'tron', 6, 'crypto', NULL, NULL, 11, true),
    ('AVAX', 'Avalanche', 'avalanche-2', 18, 'crypto', NULL, NULL, 12, true),
    ('TON', 'Toncoin', 'the-open-network', 9, 'crypto', NULL, NULL, 13, true),
    ('LINK', 'Chainlink', 'chainlink', 18, 'crypto', NULL, NULL, 14, true),
    ('DOT', 'Polkadot', 'polkadot', 10, 'crypto', NULL, NULL, 15, true),
    ('MATIC', 'Polygon', 'matic-network', 18, 'crypto', NULL, NULL, 16, true),
    ('LTC', 'Litecoin', 'litecoin', 8, 'crypto', NULL, NULL, 17, true),
    ('BCH', 'Bitcoin Cash', 'bitcoin-cash', 8, 'crypto', NULL, NULL, 18, true),
    ('SHIB', 'Shiba Inu', 'shiba-inu', 18, 'crypto', NULL, NULL, 19, true),
    ('ATOM', 'Cosmos', 'cosmos', 6, 'crypto', NULL, NULL, 20, true),
    ('UNI', 'Uniswap', 'uniswap', 18, 'crypto', NULL, NULL, 21, true),
    ('ETC', 'Ethereum Classic', 'ethereum-classic', 18, 'crypto', NULL, NULL, 22, true),
    ('XLM', 'Stellar', 'stellar', 7, 'crypto', NULL, NULL, 23, true),

    -- USDT on multiple chains
    ('USDT', 'Tether', 'tether', 6, 'crypto', 'ethereum', '0xdAC17F958D2ee523a2206206994597C13D831ec7', 3, true),
    ('USDT', 'Tether (Solana)', 'tether-solana', 6, 'crypto', 'solana', 'Es9vMFrzaCERmJfrF4H2FYD4KCoNkY11McCe8BenwNYB', NULL, true),
    ('USDT', 'Tether (Polygon)', 'tether-polygon', 6, 'crypto', 'polygon', '0xc2132D05D31c914a87C6611C10748AEb04B58e8F', NULL, true),
    ('USDT', 'Tether (BNB)', 'tether-bnb', 18, 'crypto', 'bsc', '0x55d398326f99059fF775485246999027B3197955', NULL, true),
    ('USDT', 'Tether (Arbitrum)', 'tether-arbitrum', 6, 'crypto', 'arbitrum', '0xFd086bC7CD5C481DCC9C85ebE478A1C0b69FCbb9', NULL, true),

    -- USDC on multiple chains
    ('USDC', 'USD Coin', 'usd-coin', 6, 'crypto', 'ethereum', '0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48', 7, true),
    ('USDC', 'USD Coin (Solana)', 'usd-coin-solana', 6, 'crypto', 'solana', 'EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v', NULL, true),
    ('USDC', 'USD Coin (Polygon)', 'bridged-usdc-polygon', 6, 'crypto', 'polygon', '0x2791Bca1f2de4661ED88A30C99A7a9449Aa84174', NULL, true),
    ('USDC', 'USD Coin (BNB)', 'usd-coin-bnb', 18, 'crypto', 'bsc', '0x8AC76a51cc950d9822D68b83fE1Ad97B32Cd580d', NULL, true),
    ('USDC', 'USD Coin (Arbitrum)', 'usd-coin-arbitrum', 6, 'crypto', 'arbitrum', '0xaf88d065e77c8cC2239327C5EDb3A432268e5831', NULL, true),

    -- DAI on multiple chains
    ('DAI', 'Dai', 'dai', 18, 'crypto', 'ethereum', '0x6B175474E89094C44Da98b954EescdeCB5BE3830', 24, true),
    ('DAI', 'Dai (Polygon)', 'dai-polygon', 18, 'crypto', 'polygon', '0x8f3Cf7ad23Cd3CaDbD9735AFf958023239c6A063', NULL, true),

    -- Popular DeFi tokens (on Ethereum)
    ('AAVE', 'Aave', 'aave', 18, 'crypto', 'ethereum', '0x7Fc66500c84A76Ad7e9c93437bFc5Ac33E2DDaE9', 25, true),
    ('MKR', 'Maker', 'maker', 18, 'crypto', 'ethereum', '0x9f8F72aA9304c8B593d555F12eF6589cC3A579A2', 26, true),
    ('CRV', 'Curve DAO Token', 'curve-dao-token', 18, 'crypto', 'ethereum', '0xD533a949740bb3306d119CC777fa900bA034cd52', 27, true),
    ('LDO', 'Lido DAO', 'lido-dao', 18, 'crypto', 'ethereum', '0x5A98FcBEA516Cf06857215779Fd812CA3beF1B32', 28, true),
    ('SNX', 'Synthetix', 'havven', 18, 'crypto', 'ethereum', '0xC011a73ee8576Fb46F5E1c5751cA3B9Fe0af2a6F', 29, true),
    ('COMP', 'Compound', 'compound-governance-token', 18, 'crypto', 'ethereum', '0xc00e94Cb662C3520282E6f5717214004A7f26888', 30, true),
    ('GRT', 'The Graph', 'the-graph', 18, 'crypto', 'ethereum', '0xc944E90C64B2c07662A292be6244BDf05Cda44a7', 31, true),

    -- Layer 2 tokens
    ('ARB', 'Arbitrum', 'arbitrum', 18, 'crypto', 'arbitrum', '0x912CE59144191C1204E64559FE8253a0e49E6548', 32, true),
    ('OP', 'Optimism', 'optimism', 18, 'crypto', 'optimism', '0x4200000000000000000000000000000000000042', 33, true),

    -- Other popular tokens
    ('NEAR', 'NEAR Protocol', 'near', 24, 'crypto', NULL, NULL, 34, true),
    ('FIL', 'Filecoin', 'filecoin', 18, 'crypto', NULL, NULL, 35, true),
    ('APT', 'Aptos', 'aptos', 8, 'crypto', NULL, NULL, 36, true),
    ('ICP', 'Internet Computer', 'internet-computer', 8, 'crypto', NULL, NULL, 37, true),
    ('HBAR', 'Hedera', 'hedera-hashgraph', 8, 'crypto', NULL, NULL, 38, true),
    ('VET', 'VeChain', 'vechain', 18, 'crypto', NULL, NULL, 39, true),
    ('IMX', 'Immutable', 'immutable-x', 18, 'crypto', 'ethereum', '0xF57e7e7C23978C3cAEC3C3548E3D615c346e79fF', 40, true),
    ('INJ', 'Injective', 'injective-protocol', 18, 'crypto', NULL, NULL, 41, true),
    ('ALGO', 'Algorand', 'algorand', 6, 'crypto', NULL, NULL, 42, true),
    ('FTM', 'Fantom', 'fantom', 18, 'crypto', NULL, NULL, 43, true),
    ('SAND', 'The Sandbox', 'the-sandbox', 18, 'crypto', 'ethereum', '0x3845badAde8e6dFF049820680d1F14bD3903a5d0', 44, true),
    ('MANA', 'Decentraland', 'decentraland', 18, 'crypto', 'ethereum', '0x0F5D2fB29fb7d3CFeE444a200298f468908cC942', 45, true),
    ('AXS', 'Axie Infinity', 'axie-infinity', 18, 'crypto', 'ethereum', '0xBB0E17EF65F82Ab018d8EDd776e8DD940327B28b', 46, true),
    ('EGLD', 'MultiversX', 'elrond-erd-2', 18, 'crypto', NULL, NULL, 47, true),
    ('THETA', 'Theta Network', 'theta-token', 18, 'crypto', NULL, NULL, 48, true),
    ('FLOW', 'Flow', 'flow', 8, 'crypto', NULL, NULL, 49, true),
    ('XTZ', 'Tezos', 'tezos', 6, 'crypto', NULL, NULL, 50, true)
ON CONFLICT (coingecko_id) DO NOTHING;
