# DeFi Transaction Parsing: Comprehensive Research

## Table of Contents
1. [What Data DeFi Transactions Produce](#1-what-data-defi-transactions-produce)
2. [Provider Capabilities for DeFi Parsing](#2-provider-capabilities-for-defi-parsing)
3. [Key Architecture Question: Which Approach?](#3-key-architecture-question-which-approach)
4. [Recommendation for MoonTrack](#4-recommendation-for-moontrack)

---

## 1. What Data DeFi Transactions Produce

### 1.1 Uniswap V3 Events

#### Core Pool Events (emitted by the Pool contract)

**Swap Event:**
```solidity
event Swap(
    address indexed sender,      // The address that initiated the swap (typically the router)
    address indexed recipient,   // The address that received the output tokens
    int256 amount0,             // Delta of token0 balance (negative = token0 was sent out of pool)
    int256 amount1,             // Delta of token1 balance (negative = token1 was sent out of pool)
    uint160 sqrtPriceX96,       // New sqrt(price) after the swap
    uint128 liquidity,          // Pool liquidity after the swap
    int24 tick                  // New tick after the swap
)
// Topic: 0xc42079f94a6350d7e6235f29174924f928cc2ac818eb64fed8004e115fbcca67
```

**Reconstructing "User swapped X ETH for Y USDC" from raw Swap event:**
1. Look at `amount0` and `amount1` -- one is positive (in), one is negative (out)
2. Map token0/token1 to actual token addresses by reading the pool's `token0()` and `token1()` methods
3. The negative amount is what the user received, positive is what they paid
4. The `sender` is usually the router, NOT the user -- you need the `recipient` or trace the `msg.sender` of the outer transaction to find the actual user
5. **Critical complexity**: Multi-hop swaps go through multiple pools, so a single user transaction emits multiple Swap events across different pools

**Mint Event (liquidity added):**
```solidity
event Mint(
    address sender,           // Who called mint (router/position manager)
    address indexed owner,    // Position owner (NonfungiblePositionManager for V3 NFT positions)
    int24 indexed tickLower,  // Lower tick of the position
    int24 indexed tickUpper,  // Upper tick of the position
    uint128 amount,           // Amount of liquidity minted
    uint256 amount0,          // Amount of token0 deposited
    uint256 amount1           // Amount of token1 deposited
)
```

**Burn Event (liquidity removed):**
```solidity
event Burn(
    address indexed owner,
    int24 indexed tickLower,
    int24 indexed tickUpper,
    uint128 amount,           // Amount of liquidity burned
    uint256 amount0,          // Amount of token0 withdrawn
    uint256 amount1           // Amount of token1 withdrawn
)
```

**Collect Event (fees collected):**
```solidity
event Collect(
    address indexed owner,
    address recipient,
    int24 indexed tickLower,
    int24 indexed tickUpper,
    uint128 amount0,          // Amount of token0 fees collected
    uint128 amount1           // Amount of token1 fees collected
)
```

#### NonfungiblePositionManager Events (NFT-based LP positions)

**IncreaseLiquidity:**
```solidity
event IncreaseLiquidity(
    uint256 indexed tokenId,  // NFT position ID
    uint128 liquidity,        // How much liquidity was added
    uint256 amount0,          // token0 deposited
    uint256 amount1           // token1 deposited
)
// Also emitted when a NEW position is minted
```

**DecreaseLiquidity:**
```solidity
event DecreaseLiquidity(
    uint256 indexed tokenId,
    uint128 liquidity,
    uint256 amount0,
    uint256 amount1
)
```

**Collect (NonfungiblePositionManager):**
```solidity
event Collect(
    uint256 indexed tokenId,
    address recipient,
    uint256 amount0,
    uint256 amount1
)
```

#### What Internal Transfers Happen During a Uniswap V3 Swap

A typical Uniswap V3 swap produces these token transfers:
1. **ERC-20 Transfer**: User's token0 -> Pool contract (what user pays)
2. **ERC-20 Transfer**: Pool contract -> User address (what user receives)
3. If multi-hop: intermediate pool-to-pool transfers
4. If ETH involved: WETH wrap/unwrap adds another Transfer event

**This is the key insight for Option A (raw transfers approach)**: For a simple swap, Alchemy's `getAssetTransfers` will show:
- Token A leaving user's wallet (outgoing ERC-20 transfer)
- Token B entering user's wallet (incoming ERC-20 transfer)
- Both happen in the same `tx_hash`

By grouping transfers by `tx_hash`, you CAN infer "user swapped A for B" even without decoded Swap events.

#### Complexity Rating: Uniswap = MEDIUM
- Swap events are well-structured and standardized
- LP positions require tracking NFT tokenIds
- Multi-hop swaps add complexity but are manageable
- Fee collection is a separate event from liquidity management

---

### 1.2 GMX V2 (Synthetics) Events

GMX V2 uses a fundamentally different event architecture from Uniswap.

#### The EventEmitter Pattern

All GMX V2 events are emitted through a centralized **EventEmitter** contract rather than individual contract events. This design means:

```solidity
// All events go through a single contract
contract EventEmitter {
    event EventLog(
        address msgSender,
        string eventName,           // e.g. "PositionIncrease", "OrderCreated"
        string indexed eventNameHash,
        EventUtils.EventLogData eventData  // Generic structured data blob
    );

    event EventLog1(
        address msgSender,
        string eventName,
        string indexed eventNameHash,
        bytes32 indexed topic1,     // One indexed topic for filtering
        EventUtils.EventLogData eventData
    );

    event EventLog2(
        address msgSender,
        string eventName,
        string indexed eventNameHash,
        bytes32 indexed topic1,
        bytes32 indexed topic2,
        EventUtils.EventLogData eventData
    );
}
```

#### EventData Structure

The `EventUtils.EventLogData` is a complex nested structure containing typed arrays:
- `addressItems` (keys like "account", "market", "collateralToken")
- `uintItems` (keys like "sizeDeltaUsd", "collateralAmount", "executionPrice")
- `intItems` (keys like "pnlToPoolFactor")
- `boolItems` (keys like "isLong")
- `bytes32Items` (keys like "orderKey", "positionKey")
- `stringItems`

#### Key GMX V2 Event Names

**Position Events:**
- `PositionIncrease` - Position opened or size increased
- `PositionDecrease` - Position reduced or closed
- `PositionFeesCollected` - Fees charged
- `OrderCreated` - New order placed
- `OrderExecuted` - Order filled
- `OrderCancelled` - Order cancelled

**Swap Events:**
- `SwapInfo` - Swap details within an order execution

**Liquidity/GM Token Events:**
- `DepositCreated` - GM token minting initiated
- `DepositExecuted` - GM tokens minted
- `WithdrawalCreated` - GM token burning initiated
- `WithdrawalExecuted` - GM tokens burned

#### Tracking P&L from GMX Events

To calculate P&L for a GMX position, you need to:
1. Track `PositionIncrease` events to record entry size and collateral
2. Track `PositionDecrease` events which include realized PnL data
3. The `eventData` contains fields like `basePnlUsd`, `sizeDeltaUsd`, `collateralDeltaAmount`
4. For ongoing positions, read the contract state to get unrealized PnL

#### What Internal Transfers Happen During GMX Operations

**Opening a leveraged position:**
1. ERC-20 Transfer: User -> OrderVault (collateral deposit)
2. Order execution happens asynchronously (keeper executes)
3. On execution: internal accounting updates, no additional token transfers visible to user

**Closing a position with profit:**
1. ERC-20 Transfer: Pool -> User (collateral + profit returned)
2. If loss: only partial collateral returned

**GM Token minting (providing liquidity):**
1. ERC-20 Transfer: User -> DepositVault (USDC/ETH deposited)
2. ERC-20 Transfer: GMX -> User (GM tokens minted)

#### Complexity Rating: GMX = HIGH
- The EventEmitter pattern with generic EventData requires custom ABI decoding
- Events contain generic key-value pairs, not typed parameters
- Position tracking requires state across multiple events
- Asynchronous order execution (two-step: create + execute by keeper)
- P&L calculation requires understanding GMX's funding rate and borrowing fee mechanics
- Much harder to parse than Uniswap's strongly-typed events

---

## 2. Provider Capabilities for DeFi Parsing

### 2.1 Alchemy

**What it provides:**
- `getAssetTransfers()` returns ERC-20/ETH/internal transfers, NOT decoded DeFi events
- Shows raw token movements (Token A out, Token B in) but does NOT label them as "Uniswap Swap" or "GMX Position Open"
- Internal transfers available only on Ethereum and Polygon mainnet
- Webhooks can notify on Address Activity (token in/out) but do not decode protocol interactions
- Historical Prices API available for USD rates at any timestamp

**What it does NOT provide:**
- No decoded "Swap" context from DeFi protocols
- No LP position tracking
- No GMX position state
- No protocol labeling ("this was a Uniswap swap")
- Valuation, categorization, and labeling are left to the developer

**DeFi verdict**: Alchemy gives you raw ingredients. You can group transfers by tx_hash and infer swaps, but you build all DeFi logic yourself. Good as a data layer, not as a DeFi decoder.

**Pricing**: Free tier 30M compute units/month (~1.8M requests). Paid starts at $49/mo.

### 2.2 Moralis

**What it provides:**
- **Transaction Decoding & Labeling**: Automatically decodes raw transaction input data into human-readable events. Labels transactions as "Transfer", "Swap", "Deposit", etc.
- **Wallet History Endpoint**: Full transaction history of a wallet, with human-readable category tags, address labels, and event summaries in a single API call
- **Token Swap Transactions API**: Dedicated endpoint for swap transactions on EVM and Solana
- **DeFi Protocol Positions**: Query DeFi positions including TVL, unclaimed rewards, total rewards claimed
- **Uniswap v4 support**: Comprehensive data for token prices, pair analytics, transaction tracking, OHLCV data
- **DeFi API**: Liquidity reserves and pair data across multiple blockchains

**Limitations:**
- May require "client-side orchestration to build polished, financial-grade views"
- 819 inconsistent data points in a 24-hour benchmark (vs. 0 for Alchemy) -- data accuracy concern
- GMX V2 support is unclear -- protocol coverage varies
- Less transparent about which specific DeFi protocols are decoded vs just labeled

**DeFi verdict**: Best pre-built DeFi decoding among general blockchain APIs. Transaction labeling saves massive development time. But data accuracy issues are a concern for financial applications.

**Pricing**: Free tier available with lower rate limits. Paid plans billed annually. Cost-efficient per call (~$0.000882 for balance+metadata+price vs $0.0049189 on Alchemy for same data).

### 2.3 Etherscan

**What it provides:**
- Transaction Decoder page for individual transactions (web UI)
- `txlistinternal` API for internal transactions
- ABI retrieval for verified contracts
- V2 API unifies 60+ chains under single key
- Method ID detection (first 4 bytes of input data)

**What it does NOT provide:**
- No batch decoded DeFi transaction history
- No DeFi position tracking
- No swap labeling in API responses
- Rate limited heavily on free tier (5 calls/sec)
- Designed as a block explorer API, not a DeFi data provider

**DeFi verdict**: Useful as a supplementary tool for ABI fetching and individual transaction inspection. Not suitable as a primary DeFi data source.

**Pricing**: Free tier at 5 calls/sec. Pro plans available.

### 2.4 Covalent/GoldRush

**What it provides:**
- **Transactions V3 API**: Multichain transaction histories with decoded event logs and traces
- **GoldRush Decoder**: Open-source decoder that transforms raw event logs into structured data
- Supports 100+ chains with full archival data from genesis block
- Decoded log events with proper parameter names and types
- Designed for "wallet activity feeds, crypto tax and accounting tools"

**Limitations:**
- "Latency and normalization can sometimes lag behind newer solutions"
- Less DeFi-position-aware than Zerion/DeBank (decoded logs != DeFi positions)
- Decoder is open-source but requires you to add protocol-specific decoders

**DeFi verdict**: Strong for decoded event logs and historical data. The open-source decoder is interesting for custom protocol support. Better for tax/accounting use cases than real-time position tracking.

**Pricing**: Free tier with limited credits. Paid starts at ~$50/mo.

### 2.5 The Graph / Subgraphs

**Uniswap Official Subgraphs:**
- V3 subgraph provides: Swaps, Mints, Burns, Collects, Pool data, Token data, Position data
- Query by user address: `positions(where: { owner: $owner })` returns tokenId, owner, pool, liquidity, tickLower, tickUpper
- Query swaps: Can filter by sender, recipient, pool address
- Historical data available since deployment
- V4 subgraph also available

**Example queries:**
```graphql
# Get all positions for a user
query GetPositions($owner: String!) {
  positions(where: { owner: $owner }) {
    tokenId
    owner
    pool { token0 { symbol } token1 { symbol } feeTier }
    liquidity
    tickLower { tickIdx }
    tickUpper { tickIdx }
  }
}

# Get swaps for a specific pool
query GetSwaps($pool: String!) {
  swaps(where: { pool: $pool }, orderBy: timestamp, orderDirection: desc) {
    sender
    recipient
    amount0
    amount1
    timestamp
    transaction { id }
  }
}
```

**Limitations for user-address queries:**
- Swaps are indexed by pool, not by user address directly
- To find all swaps BY a specific user, you need to filter by `origin` (tx sender) which is not always indexed
- The hosted service has been deprecated; now using decentralized network (costs GRT tokens per query)
- Query costs can add up for heavy usage

**GMX Subgraph:**
- GMX maintains official subgraphs on The Graph
- Available on Arbitrum network in The Graph Explorer
- Schema includes: positions, orders, trades, fees
- Can query positions by account address

**DeFi verdict**: Best for protocol-specific deep queries. Uniswap subgraph is well-maintained. GMX subgraph exists but with less documentation. Cost per query on decentralized network. Not ideal for "all DeFi activity for a wallet" -- you'd need to query each protocol's subgraph separately.

### 2.6 Custom Indexing Frameworks

#### Ponder
- **What**: Open-source TypeScript framework for building blockchain indexers
- **Strengths**: Hot reloading, type-safe event handlers, runs locally, great DX
- **Approach**: You write TypeScript handlers for specific contract events, Ponder handles the indexing pipeline
- **DeFi use**: You'd define Uniswap/GMX event handlers yourself with full control over data transformation
- **Setup complexity**: LOW for basic use, MEDIUM for production deployment
- **Limitation**: Self-hosted only, operational overhead for production

#### Envio
- **What**: High-performance EVM blockchain indexing framework
- **Strengths**: Fastest sync speeds in benchmarks, multi-chain support
- **Approach**: Similar to Ponder but focused on performance
- **DeFi use**: Good for high-throughput indexing of many DeFi events
- **Setup complexity**: MEDIUM -- requires DevOps expertise for production
- **Limitation**: Teams must manage infrastructure, scaling, monitoring

#### Goldsky
- **What**: Managed blockchain data platform focused on event streaming
- **Strengths**: Mirror pipelines for data sourcing, processing, and storage
- **Approach**: Event extraction + streaming to your own database/warehouse
- **DeFi use**: Good for building custom DeFi data pipelines
- **Setup complexity**: LOW-MEDIUM (managed service)
- **Limitation**: Shifts responsibility for reorg handling and state reconstruction to client

#### Subsquid (SQD)
- **What**: Flexible data pipeline framework ("squids")
- **Strengths**: Historical data batch processing, custom data extraction/transformation
- **Approach**: Define data sources and transformation logic, SQD handles the pipeline
- **DeFi use**: Best for granular control over indexing logic, easy to combine on-chain + off-chain data
- **Setup complexity**: MEDIUM
- **Limitation**: More complex than simpler alternatives

**Overall verdict on custom indexing**: Maximum flexibility and control, but significant development and operational overhead. Only worth it if you need to index protocol events that no API provider decodes for you (like GMX V2's complex EventEmitter pattern).

### 2.7 Zerion API (Wallet-Focused DeFi API)

**What it provides:**
- **DeFi Positions**: Single endpoint returns all DeFi positions for a wallet across 8,000+ protocols, normalized and valued in USD
- **Decoded Transactions**: Returns decoded transactions with contextual labels (trades, transfers, approvals)
- **PnL Tracking**: Built-in profit/loss calculations
- **GMX Support**: Explicitly supports GMX on Arbitrum
- **LP Positions**: Full LP position data with rewards

**Strengths:**
- "Portfolio-ready" data -- minimal integration effort
- Sub-second latency, 99.9% uptime SLA
- 38+ blockchain support including Arbitrum
- Used by Uniswap, Base, Farcaster, Kraken

**Pricing**: Free tier ~3,000 requests/day. Growth plan $499/mo for 1M requests.

**DeFi verdict**: Best turnkey solution for a portfolio tracker. Gives you pre-decoded DeFi positions with USD values. Minimal development needed.

### 2.8 DeBank Cloud API

**What it provides:**
- DeFi protocol positions (staking, lending, LP)
- User token balances and transaction history
- 108+ EVM network support
- Deep DeFi position coverage -- "among the best in the industry" for lending/borrowing/LP

**Limitations:**
- Returns current prices only, NOT historical USD at transaction time
- Free tier not well documented
- Rate limit: 100 RPS on Pro plan
- GMX support unconfirmed

**DeFi verdict**: Excellent for current DeFi position snapshots. Poor for historical P&L calculation due to lack of historical pricing. Not suitable as sole data source for a portfolio tracker that needs transaction-level P&L.

### 2.9 Direct Contract Event Parsing (DIY)

**What's needed to build your own parser in Go:**

1. **ABI files**: Download from Etherscan for verified contracts (Uniswap Router, Pool, NonfungiblePositionManager, GMX EventEmitter)

2. **Go-Ethereum tools**:
   - `ethclient.FilterLogs()` with topic filters to get specific events
   - `abi.JSON()` + `abi.Unpack()` for decoding event data
   - `abigen` for generating type-safe Go bindings from ABIs

3. **For Uniswap parsing**:
   ```
   // Filter for Swap events on specific pool
   query := ethereum.FilterQuery{
       Addresses: []common.Address{poolAddress},
       Topics:    [][]common.Hash{{swapEventTopic}},
       FromBlock: big.NewInt(startBlock),
       ToBlock:   big.NewInt(endBlock),
   }
   logs, _ := client.FilterLogs(ctx, query)
   ```

4. **For GMX V2 parsing**:
   - Much harder due to EventEmitter pattern
   - Need to decode the generic EventLogData structure
   - Then interpret the key-value pairs based on eventName
   - Essentially building a custom decoder for GMX's proprietary event format

5. **Go packages available**:
   - `github.com/mingjingc/abi-decoder` -- general ABI decoder
   - `github.com/uniswapv3-go/uniswapv3-universal-router-decoder-go` -- Uniswap V3 specific
   - `github.com/defiweb/go-eth` -- comprehensive Ethereum toolkit with ABI support

**Effort estimate**:
- Uniswap swap parsing: 2-3 days
- Uniswap LP position tracking: 3-5 days
- GMX V2 position parsing: 5-10 days (EventEmitter complexity)
- Ongoing maintenance when contracts upgrade: significant

---

## 3. Key Architecture Question: Which Approach?

### Option A: Raw Transfers + Inference

**How it works**: Provider (Alchemy) gives you raw token transfers. You group by tx_hash and infer the operation.

```
tx_hash: 0xabc...
  Transfer OUT: 1.5 ETH from user
  Transfer IN:  3,200 USDC to user
  → Infer: User swapped 1.5 ETH for 3,200 USDC
```

**Pros:**
- Already partially built in MoonTrack (current sync infrastructure)
- Portfolio balances are always correct (every token movement tracked)
- Simple, no protocol-specific logic needed for basic tracking
- Works with any DeFi protocol without specific support

**Cons:**
- Cannot distinguish swap from airdrop from liquidation
- No LP position tracking (you see tokens leave but don't know they're in a liquidity pool)
- No GMX perpetual position tracking (collateral goes in, but you can't see leverage/PnL)
- Multi-hop swaps look like multiple unrelated transfers
- Gas fees mixed in with other outgoing transfers
- Cannot calculate DeFi-specific P&L (impermanent loss, funding rates, etc.)

**Best for**: Simple "where are my tokens" portfolio tracking.

### Option B: Provider Gives Decoded Events

**How it works**: Provider (Zerion/Moralis) returns labeled, decoded transactions.

```
Transaction: 0xabc...
  Type: "trade"
  Protocol: "Uniswap V3"
  Sent: 1.5 ETH ($4,800)
  Received: 3,200 USDC ($3,200)
  Fee: 0.003 ETH ($9.60)
```

**Pros:**
- Minimal development effort -- provider does the heavy lifting
- DeFi positions tracked automatically (LP, lending, staking)
- Protocol labeling included
- USD values at transaction time (depending on provider)
- Handles multi-hop, aggregator swaps, etc.

**Cons:**
- Dependent on provider's protocol coverage (what if they don't support a specific protocol?)
- Data accuracy varies (Moralis showed inconsistencies in benchmarks)
- Pricing can get expensive at scale
- Less control over data interpretation
- Provider outages affect your entire system

**Best for**: Portfolio trackers that need to display "what happened" in human-readable form.

### Option C: Custom Protocol Indexing

**How it works**: You run your own indexer (Ponder/The Graph/custom) watching specific protocol contracts.

```
EventEmitter.EventLog:
  eventName: "PositionIncrease"
  account: 0xuser...
  market: 0xETH-USD...
  sizeDeltaUsd: 50000000000000000000000 (50,000 USD)
  isLong: true
  → Stored in your DB with full fidelity
```

**Pros:**
- Full control and maximum data fidelity
- Can handle any protocol, including ones not supported by API providers
- No per-query costs after initial setup
- Real-time or near-real-time data
- Can combine on-chain data with off-chain context

**Cons:**
- Significant development effort (especially GMX V2)
- Infrastructure costs (running indexer, RPC nodes)
- Must handle reorgs, missed blocks, RPC failures
- Must maintain when contracts upgrade
- Each new protocol requires new handler code

**Best for**: Projects that need deep DeFi analytics or support protocols not covered by API providers.

### Hybrid Approach (Recommended for MoonTrack)

```
┌──────────────────────────────────────────────────────────────┐
│                    DATA SOURCES                                │
│                                                                │
│  Layer 1: Alchemy (existing)                                   │
│  └─ Raw transfers → Portfolio balances (already working)       │
│                                                                │
│  Layer 2: Zerion API (new)                                     │
│  └─ Decoded transactions → Human-readable history              │
│  └─ DeFi positions → LP pools, staking, lending snapshots      │
│  └─ GMX positions → GM token tracking                          │
│                                                                │
│  Layer 3: Protocol-specific (future, only if needed)           │
│  └─ GMX V2 EventEmitter → Deep perp position tracking          │
│  └─ Uniswap subgraph → Detailed LP analytics                   │
│                                                                │
│  Layer 4: Historical prices                                    │
│  └─ Alchemy Prices API / CoinGecko → USD at tx time            │
├──────────────────────────────────────────────────────────────┤
│                    MOONTRACK PROCESSING                        │
│                                                                │
│  ┌─────────────┐   ┌─────────────┐   ┌──────────────────┐    │
│  │ Sync Service │   │ DeFi Service│   │ Position Service │    │
│  │ (existing)   │   │ (new)       │   │ (future)         │    │
│  │ Transfers    │   │ Zerion API  │   │ GMX/Uni events   │    │
│  └──────┬──────┘   └──────┬──────┘   └────────┬─────────┘    │
│         └──────────────────┴──────────────────┘               │
│                           │                                    │
│                    ┌──────▼──────┐                             │
│                    │   Ledger    │                             │
│                    │   (core)    │                             │
│                    └─────────────┘                             │
└──────────────────────────────────────────────────────────────┘
```

---

## 4. Recommendation for MoonTrack

### Phase 1: Keep What Works + Add Zerion (Weeks 1-2)

**Alchemy (keep)**: Continue using for raw transfer sync. Portfolio balances work.

**Zerion (add)**:
- Get DeFi positions for all wallets (single API call per wallet)
- Get decoded transaction history with labels
- GMX GM token tracking included
- LP position snapshots with USD values

**Why this combination:**
- Zerion's free tier (3K requests/day) is sufficient for a pet project
- Minimal code changes -- add a ZerionClient alongside AlchemyClient
- Covers 8,000+ protocols out of the box
- Your existing Alchemy sync continues to provide accurate token balances
- Zerion enriches the data with DeFi context

### Phase 2: Fix Historical USD (Week 3)

Replace current "get price at sync time" with "get price at transaction time" using:
- Alchemy Historical Prices API, or
- CoinGecko `/coins/{id}/history?date=DD-MM-YYYY`

### Phase 3: Deep GMX Integration (Only If Needed)

If Zerion's GMX coverage is insufficient for perpetual position P&L:
- Use GMX's official subgraph on Arbitrum for position queries
- Or parse GMX EventEmitter events directly using `eth_getLogs`
- This is HIGH effort and should only be done if the simpler approach fails

### What NOT to Do

1. **Do NOT build a custom indexer** (Ponder/Envio/etc.) for a pet project. The operational overhead is not justified.
2. **Do NOT try to parse GMX V2 EventEmitter events yourself** unless absolutely necessary. The generic EventData structure is a maintenance nightmare.
3. **Do NOT rely solely on raw transfers** for DeFi tracking. You'll never be able to distinguish a swap from an airdrop without protocol-specific logic.
4. **Do NOT use DeBank** as your primary API -- lacks historical USD pricing which is critical for your P&L requirements.

---

## Provider Comparison Summary

| Feature | Alchemy | Moralis | Zerion | Covalent | DeBank | The Graph | Custom Index |
|---------|---------|---------|--------|----------|--------|-----------|-------------|
| Raw Transfers | Yes | Yes | Yes | Yes | No | No | Yes |
| Decoded DeFi Events | No | Yes (labels) | Yes (labels) | Yes (logs) | No | Yes (per protocol) | Yes |
| DeFi Position Snapshots | No | Partial | Yes (8000+ protocols) | Yes | Yes (best) | Per protocol | Yes |
| GMX V2 Support | No | Unclear | Yes (confirmed) | Yes | Unclear | Yes (subgraph) | Full control |
| Historical USD | Yes (API) | Yes | Yes | Yes (archival) | No | No | DIY |
| LP Position Tracking | No | Partial | Yes | Yes | Yes | Yes (Uni subgraph) | Yes |
| Free Tier | 30M CU/mo | Available | 3K req/day | Limited | Unclear | GRT costs | Infra costs |
| Dev Effort | Low (raw) | Low-Medium | Low | Low-Medium | Low | Medium | High |
| Data Accuracy | Highest | Lower (benchmark) | High | Good | High | High | Depends |
| Go SDK | Yes | JS/Python | REST | REST | REST | GraphQL | Full control |

---

## Sources

- [Uniswap V3 Pool Events](https://docs.uniswap.org/contracts/v3/reference/core/interfaces/pool/IUniswapV3PoolEvents)
- [Uniswap V3 NonfungiblePositionManager](https://docs.uniswap.org/contracts/v3/reference/periphery/interfaces/INonfungiblePositionManager)
- [Uniswap Subgraph Query Examples](https://docs.uniswap.org/api/subgraph/guides/v3-examples)
- [GMX V2 Synthetics Contracts](https://github.com/gmx-io/gmx-synthetics)
- [GMX V2 EventEmitter](https://github.com/gmx-io/gmx-synthetics/blob/main/contracts/event/EventEmitter.sol)
- [GMX API - Bitquery](https://docs.bitquery.io/docs/examples/Arbitrum/gmx-api/)
- [GMX Contracts Reference](https://docs.gmx.io/docs/api/contracts/)
- [Alchemy getAssetTransfers](https://docs.alchemy.com/reference/alchemy-getassettransfers)
- [Moralis Transaction Decoding & Labeling](https://docs.moralis.com/changelog/2023-02-16-transaction-labeling.md)
- [Moralis Token Swap API](https://docs.moralis.com/changelog/token-swap-transactions-api)
- [Zerion API](https://zerion.io/api)
- [Zerion Wallet Data APIs Comparison](https://zerion.io/blog/top-10-crypto-wallet-data-apis-2025-guide/)
- [Covalent GoldRush Decoder](https://github.com/covalenthq/goldrush-decoder)
- [Covalent Transactions V3 API](https://goldrush.dev/docs/unified-api/guides/introducing-transactions-v3-apis/)
- [DeBank Cloud API](https://docs.cloud.debank.com/en/readme/open-api)
- [Best Blockchain Indexers 2026](https://blog.ormilabs.com/best-blockchain-indexers-in-2025-real-time-web3-data-and-subgraph-platforms-compared/)
- [Ponder Framework](https://ponder.sh/)
- [Subsquid DeFi Case Study](https://blog.sqd.dev/how-a-defi-builder-cut-indexing-time-from-months-to-days-with-sqd/)
- [Event-Driven DeFi Portfolio Tracker on AWS](https://aws.amazon.com/blogs/web3/implementing-an-event-driven-defi-portfolio-tracker-on-aws/)
- [Go-Ethereum Event Reading](https://goethereumbook.org/en/event-read/)
- [QuickNode Uniswap Swaps Detector](https://www.quicknode.com/docs/functions/functions-library/uniswap-swaps-detector)
- [Etherscan APIs](https://etherscan.io/apis)
