# Zerion API Research Report for MoonTrack Integration

## 1. API Overview

**Base URL:** `https://api.zerion.io/v1`

Zerion API provides decoded, human-readable blockchain data across 38+ chains via a single REST API. It covers wallet balances, DeFi positions (8,000+ protocols), decoded transactions, historical prices, PnL, NFTs, and real-time webhook notifications.

---

## 2. Authentication

**Method:** HTTP Basic Authentication

The API key (format `zk_dev_*` or `zk_prod_*`) is used as the username with an empty password. Append `:` to the key and base64-encode it.

```bash
# Encode API key
echo -n 'zk_dev_your_key_here:' | base64

# Use in request
curl -H "Authorization: Basic <base64_encoded_key>" \
  'https://api.zerion.io/v1/wallets/0x.../positions/'

# Or with curl --user
curl --user 'zk_dev_your_key_here:' --globoff \
  'https://api.zerion.io/v1/wallets/0x.../positions/'
```

**Dashboard:** https://dashboard.zerion.io/ (create org, generate keys instantly)

---

## 3. Rate Limits & Pricing

| Plan       | Cost       | Requests          | RPS  |
|------------|------------|--------------------|------|
| Developer  | $0/month   | 2,000 requests/day | 10   |
| Builder    | $149/month | 250K requests/month| 50   |
| Startup    | $499/month | 1M requests/month  | 150  |
| Enterprise | Custom     | 2M+ requests/month | 1000+|

**Error codes:** 429 when rate limit exceeded.

---

## 4. Complete Endpoint Catalog

### 4.1 Wallet Endpoints

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/wallets/{address}/portfolio` | Portfolio overview (total value, distribution by chain/type) |
| GET | `/wallets/{address}/positions/` | Fungible positions (wallet + DeFi) |
| GET | `/wallets/{address}/transactions/` | Decoded transaction history |
| GET | `/wallets/{address}/chart` | Balance chart over time |
| GET | `/wallets/{address}/pnl` | Profit and loss |
| GET | `/wallets/{address}/nft-positions` | NFT positions |
| GET | `/wallets/{address}/nft-collections` | NFT collections |
| GET | `/wallets/{address}/nft-portfolio` | NFT portfolio overview |

### 4.2 Fungible Asset Endpoints

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/fungibles` | List/search fungible assets |
| GET | `/fungibles/{id}` | Get fungible by ID |
| GET | `/fungibles/{id}/charts/{period}` | Historical price chart |
| GET | `/fungibles/implementation/{address}` | Get fungible by contract address |
| GET | `/fungibles/implementation/{address}/chart` | Chart by contract address |

### 4.3 Reference Endpoints

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/chains` | List all supported chains |
| GET | `/chains/{id}` | Get chain by ID |
| GET | `/gas` | Gas prices |
| GET | `/dapps` | List DeFi protocols |
| GET | `/dapps/{id}` | Get DApp details |

### 4.4 Webhook/Subscription Endpoints

| Method | Endpoint | Description |
|--------|----------|-------------|
| POST | `/tx-subscriptions` | Create transaction subscription |
| GET/PUT/DELETE | `/tx-subscriptions/{id}` | Manage subscriptions |

---

## 5. Wallet Transactions Endpoint (Detailed)

### Request

```
GET https://api.zerion.io/v1/wallets/{address}/transactions/
```

### Query Parameters

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `currency` | string | `usd` | Currency for values (usd, eth, btc, eur, etc.) |
| `page[after]` | string | - | Cursor for pagination (omit for first page) |
| `page[size]` | integer | 100 | Items per page (max 100) |
| `filter[operation_types]` | string | - | Comma-separated operation types |
| `filter[asset_types]` | string | - | `fungible`, `nft` |
| `filter[chain_ids]` | string | - | Comma-separated chain IDs |
| `filter[fungible_ids]` | string | - | Comma-separated asset IDs (max 25) |
| `filter[min_mined_at]` | integer | - | Start timestamp (milliseconds) |
| `filter[max_mined_at]` | integer | - | End timestamp (milliseconds) |
| `filter[search_query]` | string | - | Full-text search (2-64 chars) |
| `filter[trash]` | string | - | `only_trash`, `only_non_trash`, `no_filter` |
| `filter[fungible_implementations]` | string | - | `chain:address` pairs |
| `X-Env` header | string | - | Set to `testnet` for testnets |

### Operation Type Enum Values

```
approve | burn | claim | delegate | deploy | deposit | execute |
mint | receive | revoke | revoke_delegation | send | trade | withdraw
```

### Response Schema

```json
{
  "links": {
    "self": "https://api.zerion.io/v1/wallets/0x.../transactions/",
    "next": "https://api.zerion.io/v1/wallets/0x.../transactions/?page[after]=cursor_token"
  },
  "data": [
    {
      "type": "transactions",
      "id": "unique_transaction_id",
      "attributes": {
        "hash": "0xabc123...",
        "mined_at_block": 18500000,
        "mined_at": "2024-01-15T12:30:00Z",
        "sent_from": "0x...",
        "sent_to": "0x...",
        "status": "confirmed",
        "nonce": 42,
        "operation_type": "trade",
        "fee": {
          "fungible_info": {
            "name": "Ethereum",
            "symbol": "ETH",
            "icon": { "url": "https://..." },
            "flags": { "verified": true },
            "implementations": [
              { "chain_id": "ethereum", "address": null, "decimals": 18 }
            ]
          },
          "quantity": {
            "int": "5000000000000000",
            "decimals": 18,
            "float": 0.005,
            "numeric": "0.005"
          },
          "price": 3000.0,
          "value": 15.0
        },
        "transfers": [
          {
            "fungible_info": {
              "name": "USD Coin",
              "symbol": "USDC",
              "icon": { "url": "https://..." },
              "flags": { "verified": true },
              "implementations": [
                { "chain_id": "ethereum", "address": "0xa0b8...", "decimals": 6 }
              ]
            },
            "direction": "out",
            "quantity": {
              "int": "1000000000",
              "decimals": 6,
              "float": 1000.0,
              "numeric": "1000.0"
            },
            "value": 1000.0,
            "price": 1.0,
            "sender": "0x...",
            "recipient": "0x...",
            "act_id": "act_1"
          },
          {
            "fungible_info": {
              "name": "Ethereum",
              "symbol": "ETH",
              "icon": { "url": "https://..." },
              "flags": { "verified": true },
              "implementations": [
                { "chain_id": "ethereum", "address": null, "decimals": 18 }
              ]
            },
            "direction": "in",
            "quantity": {
              "int": "333000000000000000",
              "decimals": 18,
              "float": 0.333,
              "numeric": "0.333"
            },
            "value": 999.0,
            "price": 3000.0,
            "sender": "0x...",
            "recipient": "0x...",
            "act_id": "act_1"
          }
        ],
        "approvals": [],
        "collection_approvals": [],
        "acts": [
          {
            "id": "act_1",
            "type": "trade",
            "application_metadata": {
              "name": "Uniswap V3",
              "icon": { "url": "https://..." }
            }
          }
        ]
      },
      "relationships": {
        "chain": {
          "data": { "type": "chains", "id": "ethereum" }
        }
      }
    }
  ]
}
```

### Key Schema Details

**`quantity` object** (used in transfers, fee, approvals):
- `int` (string): Raw integer value (no decimals applied)
- `decimals` (integer): Number of decimal places
- `float` (number): Floating-point approximation
- `numeric` (string): String representation with decimals applied

**`fungible_info` object:**
- `name` (string): Token name
- `symbol` (string): Token symbol
- `description` (string, nullable): Description
- `icon.url` (string, nullable): Icon URL
- `flags.verified` (boolean): Verified token flag
- `implementations[]`: Array of chain deployments with `chain_id`, `address`, `decimals`
- `market_data` (nullable): Current market data with `price`, `market_cap`, `circulating_supply`, `changes`

**`nft_info` object** (alternative to fungible_info in transfers):
- `contract_address`, `token_id`, `name`
- `interface`: `erc721` | `erc1155`
- `content`: preview/detail/audio/video URLs
- `flags.is_spam`

**`acts[]` array** -- groups transfers/approvals by semantic action:
- `id`: Reference ID (matches `act_id` in transfers/approvals)
- `type`: Same enum as operation_type + `bid`
- `application_metadata`: DApp name and icon if applicable

### Mapping operation_type to MoonTrack Ledger

| Zerion operation_type | MoonTrack Handler | Ledger Pattern |
|-----------------------|-------------------|----------------|
| `send` | Transfer Out | DR Asset (dest wallet) / CR Asset (source wallet) |
| `receive` | Transfer In | DR Asset (dest wallet) / CR Asset (source wallet) |
| `trade` | Swap | DR Asset-In / CR Asset-Out + Realized PnL |
| `deposit` | DeFi Deposit | DR Protocol-Position / CR Wallet-Asset |
| `withdraw` | DeFi Withdraw | DR Wallet-Asset / CR Protocol-Position |
| `claim` | Reward Claim | DR Wallet-Asset / CR Income:Rewards |
| `approve` | (No ledger entry) | Informational only |
| `mint` | Mint/Receive | DR Asset / CR Income:Mint |
| `burn` | Burn | DR Expense:Burn / CR Asset |
| `execute` | Generic/Complex | Analyze transfers to determine pattern |

---

## 6. Wallet Positions Endpoint (Detailed)

### Request

```
GET https://api.zerion.io/v1/wallets/{address}/positions/
```

### Key Parameters

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `filter[positions]` | string | `only_simple` | `only_simple` (wallet only), `only_complex` (DeFi only), `no_filter` (all) |
| `filter[position_types]` | string | - | `deposit`, `loan`, `locked`, `staked`, `reward`, `wallet`, `investment` |
| `filter[chain_ids]` | string | - | Comma-separated chain IDs |
| `filter[dapp_ids]` | string | - | Filter by DeFi protocol |
| `filter[trash]` | string | - | `only_trash`, `only_non_trash`, `no_filter` |
| `sort` | string | `-value` | `-value` (desc) or `value` (asc) |
| `sync` | boolean | false | Wait up to 30s for data refresh |
| `currency` | string | `usd` | Denomination currency |

### Position Type Enum

| Value | Description |
|-------|-------------|
| `wallet` | Direct wallet holdings (not in any protocol) |
| `deposit` | Assets deposited in DeFi protocols (lending, vaults, LPs) |
| `loan` | Borrowed assets (debt) |
| `locked` | Time-locked or vote-escrowed tokens |
| `staked` | Staked for rewards or consensus |
| `reward` | Unclaimed earned rewards |
| `investment` | Tokenized funds, indices, structured products |

### Response Schema

```json
{
  "links": {
    "self": "...",
    "next": "..."
  },
  "data": [
    {
      "type": "positions",
      "id": "position_unique_id",
      "attributes": {
        "name": "Asset",
        "position_type": "deposit",
        "quantity": {
          "int": "1000000000000000000",
          "decimals": 18,
          "float": 1.0,
          "numeric": "1.0"
        },
        "value": 3000.0,
        "price": 3000.0,
        "changes": {
          "absolute_1d": 50.0,
          "percent_1d": 1.7
        },
        "updated_at": "2024-01-15T12:00:00Z",
        "updated_at_block": 18500000,
        "protocol": "Aave V3",
        "protocol_module": "lending",
        "pool_address": "0x...",
        "group_id": "group_abc",
        "fungible_info": {
          "name": "Ethereum",
          "symbol": "ETH",
          "icon": { "url": "https://..." },
          "flags": { "verified": true },
          "implementations": [
            { "chain_id": "ethereum", "address": null, "decimals": 18 }
          ]
        },
        "flags": {
          "displayable": true,
          "is_trash": false
        }
      },
      "relationships": {
        "chain": {
          "data": { "type": "chains", "id": "ethereum" }
        },
        "fungible": {
          "data": { "type": "fungibles", "id": "eth" }
        },
        "dapp": {
          "data": { "type": "dapps", "id": "aave-v3" }
        }
      }
    }
  ]
}
```

### DeFi Position Grouping

Liquidity pool positions (Uniswap, Curve, Balancer) return **multiple position objects** -- one per token in the pool. They share the same `group_id` for client-side aggregation. Example: a Uniswap V2 ETH/USDC pool returns two positions (ETH + USDC) with identical `group_id`.

### Protocol Module Categories

```
deposit | lending | yield | liquidity_pool | staked | farming |
locked | vesting | rewards | investment
```

---

## 7. Historical Prices Endpoint

### Request

```
GET https://api.zerion.io/v1/fungibles/{fungible_id}/charts/{chart_period}
```

### Path Parameters

| Parameter | Values |
|-----------|--------|
| `fungible_id` | Asset ID from positions/fungibles endpoints (max 44 chars) |
| `chart_period` | `hour`, `day`, `week`, `month`, `3months`, `6months`, `year`, `5years`, `max` |

### Query Parameters

| Parameter | Default | Options |
|-----------|---------|---------|
| `currency` | `usd` | eth, btc, usd, eur, krw, rub, gbp, aud, cad, inr, jpy, nzd, try, zar, cny, chf |

### Response Schema

```json
{
  "links": {
    "self": "https://api.zerion.io/v1/fungibles/eth/charts/month"
  },
  "data": {
    "type": "fungible_charts",
    "id": "eth-month",
    "attributes": {
      "begin_at": "2024-01-01T00:00:00Z",
      "end_at": "2024-01-31T23:59:59Z",
      "points": [
        [1704067200, 2500.50],
        [1704153600, 2520.75],
        [1704240000, 2480.30]
      ]
    }
  }
}
```

Each point is `[unix_timestamp_seconds, price_float]`.

**Note:** For MoonTrack, this replaces/complements CoinGecko for historical price lookups. The `fungible_id` from positions/transactions can be used directly.

---

## 8. Portfolio Summary Endpoint

### Request

```
GET https://api.zerion.io/v1/wallets/{address}/portfolio
```

### Response Schema

```json
{
  "data": {
    "type": "portfolio",
    "id": "0x...",
    "attributes": {
      "positions_distribution_by_type": {
        "wallet": 50000.0,
        "deposited": 20000.0,
        "borrowed": -5000.0,
        "locked": 3000.0,
        "staked": 10000.0
      },
      "positions_distribution_by_chain": {
        "ethereum": 60000.0,
        "polygon": 15000.0,
        "arbitrum": 8000.0
      },
      "total": {
        "positions": 78000.0
      },
      "changes": {
        "absolute_1d": 1200.0,
        "percent_1d": 1.56
      }
    }
  }
}
```

---

## 9. Pagination & Sync Strategy

### Cursor-Based Pagination

All list endpoints use **cursor-based pagination** via `page[after]`:

```
# First page
GET /v1/wallets/{addr}/transactions/?page[size]=100

# Response includes next link
"links": {
  "next": "https://api.zerion.io/v1/wallets/.../transactions/?page[after]=cursor_abc123"
}

# Subsequent pages
GET /v1/wallets/{addr}/transactions/?page[after]=cursor_abc123&page[size]=100
```

- No `page[after]` = first page
- `links.next` = null when no more pages
- Max `page[size]` = 100

### Time-Based Filtering for Incremental Sync

Use `filter[min_mined_at]` (milliseconds timestamp) to only fetch transactions after a certain point:

```
GET /v1/wallets/{addr}/transactions/?filter[min_mined_at]=1704067200000
```

This is the primary mechanism for incremental sync -- store the `mined_at` of the last processed transaction and use it as `min_mined_at` on subsequent syncs.

### Sync Parameter

The `sync=true` parameter on positions endpoints triggers a fresh data refresh (waits up to 30 seconds). Use sparingly due to latency impact. Recommended polling with 2-minute timeout.

---

## 10. Chain Support

### Confirmed Chain IDs (from documentation)

| Chain ID | Network |
|----------|---------|
| `ethereum` | Ethereum Mainnet |
| `polygon` | Polygon (MATIC) |
| `arbitrum` | Arbitrum One |
| `optimism` | Optimism |
| `base` | Base |
| `avalanche` | Avalanche C-Chain |
| `binance-smart-chain` | BNB Chain |
| `fantom` | Fantom |
| `linea` | Linea |
| `zksync-era` | zkSync Era |
| `aurora` | Aurora (NEAR) |
| `celo` | Celo |
| `xdai` | Gnosis Chain |
| `solana` | Solana |

**Total:** 38+ chains supported. Full list available via `GET /v1/chains`.

Chain IDs are **lowercase hyphenated strings** (not numeric chain IDs). Use `GET /v1/chains` endpoint to dynamically discover all supported chains.

### Solana Limitations
- No protocol/DeFi positions support
- No NFT transaction support
- Wallet positions and transactions work

---

## 11. Webhook Subscriptions (Real-Time)

### Create Subscription

```
POST https://api.zerion.io/v1/tx-subscriptions

{
  "addresses": ["0x42b9dF65B219B3dD36FF330A4dD8f327A6Ada990"],
  "callback_url": "https://your-server.com/webhooks/zerion",
  "chain_ids": ["ethereum", "base", "arbitrum"]
}
```

- `chain_ids`: optional, empty = all chains
- `callback_url`: webhook.site works immediately; custom URLs need approval
- Zerion sends POST with full transaction data to callback URL
- Delivery order not guaranteed
- 3 failed delivery attempts then stops
- `price` and `value` fields may be `null` in webhook payloads -- fetch via transactions endpoint for values

---

## 12. Key Data Types for MoonTrack Integration

### Quantity Object (Financial Precision)

```json
{
  "int": "1000000000000000000",  // Raw integer (string) -- USE THIS for NUMERIC(78,0)
  "decimals": 18,                // Decimal places
  "float": 1.0,                  // Float approximation -- DO NOT USE for accounting
  "numeric": "1.0"               // String with decimals applied
}
```

**Critical for MoonTrack:** Use `quantity.int` (string) with `quantity.decimals` (int) for precision. Map `int` directly to `math/big.Int` in Go. Never use `float` for financial calculations.

### Fungible ID

The `fungible_id` is an abstract string (e.g., `"eth"`, `"0xa0b86991c6218b36c1d19d4a2e9eb0ce3606eb48"`) that uniquely identifies a token across all chains. Same token on different chains shares the same `fungible_id`. Use this as the canonical asset identifier in MoonTrack.

---

## 13. Integration Architecture Considerations for MoonTrack

### Sync Strategy

1. **Initial Sync:** Paginate through all transactions using cursor pagination
2. **Incremental Sync:** Use `filter[min_mined_at]` with last-seen timestamp
3. **Real-Time:** Webhook subscription for new transactions (with fallback polling)
4. **Position Snapshot:** Periodically fetch positions for reconciliation

### Transaction-to-Ledger Mapping

Each Zerion transaction contains `acts[]` which semantically group `transfers[]`. For MoonTrack:

1. Read `operation_type` (or `acts[].type`) to determine handler
2. Read `transfers[]` with `direction` (in/out/self) to determine debits/credits
3. Use `quantity.int` + `decimals` for precise amounts
4. Use `value` (USD) for cost basis tracking
5. Use `relationships.chain.data.id` for chain identification
6. Use `fungible_info.implementations` to resolve token addresses per chain

### DeFi Position Tracking

1. Fetch `filter[positions]=only_complex` for all DeFi positions
2. Group by `group_id` for LP positions
3. Track `position_type` to categorize (deposit/loan/staked/reward)
4. Use `dapp` relationship for protocol identification
5. Store snapshots for position value tracking over time

### Rate Limit Management

At Developer tier (free): 2,000 req/day, 10 RPS
- A full sync of a wallet with 1,000 transactions = ~10 paginated requests
- 20 wallets full sync = ~200 requests
- Daily incremental sync of 20 wallets = ~20 requests
- Position snapshots = ~20 requests
- **Sufficient for MVP** with careful request management
- Consider Builder tier ($149/mo) for production with multiple users

---

## Sources

- [Zerion API Endpoints and Schema Details](https://developers.zerion.io/reference/endpoints-and-schema-details)
- [List Wallet Transactions](https://developers.zerion.io/reference/listwallettransactions)
- [List Wallet Positions](https://developers.zerion.io/reference/listwalletpositions)
- [Get Wallet Portfolio](https://developers.zerion.io/reference/getwalletportfolio)
- [Get Fungible Chart](https://developers.zerion.io/reference/getfungiblechart)
- [List Fungible Assets](https://developers.zerion.io/reference/listfungibles)
- [List Chains](https://developers.zerion.io/reference/listchains)
- [Authentication](https://developers.zerion.io/reference/intro/authentication)
- [Getting Started](https://developers.zerion.io/reference/intro/getting-started)
- [Zerion API Overview](https://zerion.io/api)
- [DeFi Positions Blog Post](https://zerion.io/blog/how-to-fetch-multichain-defi-positions-for-wallet-with-zerion-api/)
- [Cross-Chain Transaction History Blog](https://zerion.io/blog/how-to-track-cross-chain-transaction-history/)
- [Webhook/Subscription Blog](https://zerion.io/blog/how-to-create-real-time-ethereum-transaction-notifications-with-zerion-api/)
