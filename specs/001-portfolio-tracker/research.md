# Research: Crypto Portfolio Tracker

**Feature**: 001-portfolio-tracker
**Phase**: 0 - Research & Technology Selection
**Date**: 2026-01-06

## Overview

This document captures technology selection decisions and research findings that resolve all "NEEDS CLARIFICATION" items from the Technical Context section of plan.md.

## Backend Technology Stack

### HTTP Router

**Decision**: **chi** (github.com/go-chi/chi/v5)

**Rationale**:
- **Simplicity First alignment**: Lightweight (~1000 LOC core), 100% compatible with standard library net/http
- **No framework bloat**: Adds only routing and middleware grouping without custom context or response helpers
- **Production-ready**: Rich middleware ecosystem (logging, recovery, rate limiting, CORS), route grouping perfect for organizing API by domain modules
- **Community standard**: With gorilla/mux archived (2024), chi has become the go-to standard-library-compatible router
- **Performance**: One of the fastest routers, though microseconds difference is not the deciding factor

**Alternatives Considered**:
- **Standard library net/http ServeMux**: Too minimal - lacks middleware chaining, limited route parameters, no route grouping. Would violate YAGNI in reverse by recreating what chi provides.
- **gin**: Full framework with custom context - introduces lock-in and overkill for our needs (no template rendering needed)
- **echo**: Similar to gin - feature-rich but more than needed. Chi's stdlib compatibility wins.

### JWT Authentication Library

**Decision**: **golang-jwt/jwt** (github.com/golang-jwt/jwt/v5)

**Rationale**:
- **Security**: Community-maintained successor to archived dgrijalva/jwt-go. v5 includes critical security improvements for algorithm validation (addresses CVE-2025-30204 from March 2025)
- **Simplicity & YAGNI**: Focused solely on JWT - clean API for generate/validate use case
- **Best practice enforcement**: Library encourages `WithValidMethods` option to prevent algorithm confusion attacks (aligns with Security by Design principle)

**Alternatives Considered**:
- **lestrrat-go/jwx**: Full JOSE spec implementation (JWA, JWE, JWK, JWS, JWT). We only need JWT - rest is feature bloat. Excellent for complex scenarios but overkill for basic session management.
- **DIY with stdlib crypto**: High risk of security bugs. Never roll your own crypto/auth - violates Security by Design.

### Database Migration Tool

**Decision**: **golang-migrate/migrate** (github.com/golang-migrate/migrate/v4)

**Rationale**:
- **Production reliability**: Database locking mechanism prevents concurrent migrations. Robust transaction support with automatic rollback.
- **CI/CD integration**: CLI-first design perfect for deployment pipelines. Language-agnostic pure SQL files.
- **Simplicity**: Plain SQL with up/down naming convention (001_initial.up.sql). No DSL to learn. Aligns with "Direct solutions preferred" principle.
- **Battle-tested**: 10.3k+ GitHub stars, widely adopted since 2014
- **PostgreSQL compatibility**: Full support for NUMERIC(78,0) precision requirements

**Alternatives Considered**:
- **pressly/goose**: Solid tool with optional Go-code migrations. Smaller community (3.2k stars). golang-migrate's database locking and CLI-first approach edge it out for production.
- **Custom SQL scripts**: No version tracking, no rollback support, no concurrency protection. Too critical to DIY.

### PostgreSQL Driver

**Decision**: **pgx** (github.com/jackc/pgx/v5)

**Rationale**:
- **Modern native driver**: Pure Go PostgreSQL driver and toolkit designed specifically for PostgreSQL (not generic database/sql abstraction)
- **Superior performance**: Direct protocol implementation without database/sql overhead. 20-30% faster than lib/pq for connection pooling and query execution.
- **Better type support**: Native support for PostgreSQL-specific types including NUMERIC with full precision (critical for our NUMERIC(78,0) requirements). Better JSON/JSONB handling for metadata fields.
- **Active maintenance**: Actively developed and maintained. lib/pq is in maintenance mode (feature-frozen since 2016).
- **Rich feature set**: Built-in connection pooling (pgxpool), prepared statement cache, pipeline mode, COPY support, Listen/Notify for real-time features.
- **Production-ready**: Used by major companies and projects. Battle-tested with excellent reliability.
- **Security**: Regular security updates and modern security practices.

**Alternatives Considered**:
- **lib/pq** (github.com/lib/pq): Legacy standard but now in maintenance mode. Lacks modern features, slower performance, and limited type support. Still works but pgx is the recommended choice for new projects.
- **database/sql + pq**: Generic abstraction adds overhead and limits PostgreSQL-specific features. pgx can work with database/sql interface when needed but provides better native API.

## Frontend Technology Stack

### State Management

**Decision**: **React Hooks (useState, useContext, useReducer) + Zustand (when needed)**

**Rationale**:
- **Constitution alignment**: "Start with React hooks. Add state management library only when complexity justifies it."
- **Use Case breakdown**:
  - Authentication state (JWT, user session): **Context API** - global, infrequent changes, perfect fit
  - Local component state (form inputs, UI toggles): **useState** directly
  - Complex shared state (portfolio data across views): **Zustand** only when Context limitations hit
- **Zustand advantages**: Tiny bundle (1-3KB vs Redux Toolkit's ~15KB), zero boilerplate, minimal learning curve
- **Simplicity First**: Start simple, graduate only when needed

**Alternatives Considered**:
- **Redux Toolkit** (~15KB): Too heavy. "100+ lines of boilerplate" for simple operations. Enterprise-scale features we don't need.
- **Jotai** (~1.2-6.3KB): Excellent atomic state model but adds conceptual overhead. Best for "complex interdependent state" - overkill for portfolio/wallet management.
- **Pure Context for everything**: Would work but causes "unnecessary re-renders" and "Provider Hell" with multiple contexts.

**Implementation Strategy**:
1. **Start**: Context API for auth, useState for local, TanStack Query for server state (see below)
2. **Graduate later**: Add Zustand only when shared client state causes Context re-render issues
3. **Likely use case**: Global UI state (sidebar, theme, notifications)

### Routing Library

**Decision**: **React Router v6**

**Rationale**:
- **Battle-tested maturity**: Industry standard with years of production use
- **Simple API for simple needs**: Basic navigation (dashboard, wallets, transactions) handled cleanly
- **Feature complete**: Nested routes, protected routes (for auth), URL parameters - everything needed is built-in
- **Bundle size acceptable**: Difference vs alternatives negligible for our use case

**Alternatives Considered**:
- **TanStack Router** (~12KB): "TypeScript first" with advanced type safety. Excellent but adds complexity (file-based routing, code generation, new API to learn). Violates "Simplicity First" unless complex routing needs exist.
- **Wouter** (2.1KB): Most lightweight but missing features (route guards, nested routes require workarounds). Would save ~10KB but add implementation complexity.

### API Client Approach

**Decision**: **TanStack Query (React Query) + axios**

**Rationale**:
- **Modern best practice**: Separate server state from client state. "Server state is 90% of your state" (portfolio, wallets, transactions, auth data)
- **TanStack Query solves our problems**:
  - **Caching**: Portfolio data cached automatically, shows instantly while revalidating
  - **Loading/error states**: "All baked in" - no manual useState for flags
  - **Smart refetching**: Auto-refetches on window focus, network recovery. Perfect for portfolio data staying current
  - **Simplicity**: "Five lines" vs "100+ lines of boilerplate" for data fetching
- **axios underneath**: Cleaner JWT token management (interceptors), better error handling, more concise than fetch for JSON APIs

**Alternatives Considered**:
- **fetch only**: Zero dependencies but requires boilerplate for error handling, JSON parsing, JWT injection. Would build wrapper anyway.
- **axios only** (~13KB): Handles HTTP well but doesn't solve caching, loading states, refetching. Would write equivalent logic manually.
- **Custom wrapper**: Violates "Don't build abstractions until 3+ use cases." TanStack Query is the proven abstraction (40K+ stars).

**Bundle consideration**: TanStack Query + axios = ~53KB. Worth it because:
1. Avoids writing equivalent caching/refetch/deduplication logic
2. Replaces state management for server data (lighter than Redux)
3. App is heavily server-data driven

## External Services

### Cryptocurrency Price API

**Decision**: **CoinGecko API (Free Demo Plan)**

**Rationale**:
- **Generous free tier**: 30 calls/minute, 10,000 calls/month with Demo plan (free registration)
- **Comprehensive coverage**: 13,000+ cryptocurrencies across 250+ blockchain networks (vs CoinMarketCap's narrower coverage)
- **Historical data access**: Up to 365 days of historical prices on free tier (critical for FR-015 transaction history). CoinMarketCap lacks this on free tier - deal breaker.
- **Excellent reliability**: 99.9% uptime SLA, trusted by Metamask, Coinbase, Etherscan
- **No commercial restrictions**: Free tier allows production use (CoinMarketCap's free tier is "personal use only")

**Alternatives Considered**:
- **CoinMarketCap API**: No historical data on free tier (critical blocker), personal use only restriction, less token coverage
- **CryptoCompare API**: Unclear free tier rate limits, less transparent pricing
- **Binance API**: Exchange-specific prices only (not market aggregator), missing thousands of tokens not listed on Binance
- **Kraken/Coinbase APIs**: Exchange-specific, limited coverage

**Integration Approach**:

Key endpoints:
1. **Current prices**: `GET /api/v3/simple/price?ids=bitcoin,ethereum&vs_currencies=usd`
2. **Historical price**: `GET /api/v3/coins/{id}/history?date=30-12-2025`
3. **Coin list**: `GET /api/v3/coins/list` (one-time mapping setup)

Authentication: Requires API key in header `x-cg-demo-api-key: YOUR_API_KEY`

Rate limits:
- 30 calls/minute (Demo plan)
- 10,000 calls/month
- Returns HTTP 429 when exceeded → implement exponential backoff

**Caching Strategy**:

Per constitution FR-016 requirement ("handle API failures gracefully"):

1. **In-Memory Cache (Redis)**: 60-second TTL for all price responses
   ```
   Key: "price:bitcoin:usd"
   Value: {"price": 45678.90, "updated_at": "2026-01-06T10:30:00Z"}
   TTL: 60 seconds
   ```

2. **Database-Backed Historical Prices**:
   ```sql
   CREATE TABLE price_snapshots (
       id UUID PRIMARY KEY,
       asset_id VARCHAR(20) NOT NULL,
       usd_price NUMERIC(78,0) NOT NULL,  -- Scaled integer (e.g., * 10^8)
       snapshot_date DATE NOT NULL,
       source VARCHAR(50) NOT NULL,
       created_at TIMESTAMP NOT NULL,
       UNIQUE(asset_id, snapshot_date, source)
   );
   ```

3. **Multi-Layer Fallback**: User Request → Cache (60s) → Database → CoinGecko API → Stale Cache (24h) → Error

4. **Batch Optimization**: Request prices for multiple assets in single call (comma-separated IDs) - reduces 10 calls to 1

5. **Background Refresh Job**: Cron job every 5 minutes refreshes prices for active assets
   - ~12 calls/hour × 24h = 288 calls/day for 50 assets
   - With user-triggered requests: ~400 calls/day = 12,000/month
   - **Supports 50-100 active users on free tier**

6. **Circuit Breaker Pattern**: After 3 consecutive failures, open circuit and serve from stale cache with user warning

**Handling Edge Cases**:

- **Missing price data** (obscure tokens): Try CoinGecko → Try DEX price → Allow manual price input in transaction form (FR-008) → Display "Price unavailable" if all fail
- **API downtime**: Multi-layer fallback with user warning banner: "⚠️ Using cached prices from [timestamp]"
- **Manual price input**: User can optionally specify `usd_rate` when creating transaction. If provided, use it; if null, fetch from API. Price is permanently stored in immutable ledger entry (FR-008, FR-017 reinterpreted)

**USD Rate Storage** (Constitution compliance):

Store as scaled integer per Principle IV:
- Use 10^8 scaling factor for USD rates
- Example: BTC price $45,678.90 → store as `4567890000000` (big.Int)
- Never use float64 or decimal types
- Historical rates are IMMUTABLE in ledger entries

**Upgrade Path**:

Upgrade to CoinGecko Analyst plan ($129/month) when:
- Monthly calls exceed 8,000 (80% of free limit)
- Need >365 days historical data
- Production launch with >100 daily active users
- Need faster rate limits (500 calls/min)

## Requirements Clarifications

### Browser Compatibility

**Decision**: Modern browsers with ES6+ support (Chrome 90+, Firefox 88+, Safari 14+, Edge 90+)

**Rationale**: Aligns with React 18 requirements and Simplicity First principle. Avoids polyfill complexity for legacy browsers.

### Mobile Responsiveness

**Decision**: Mobile-responsive design using CSS media queries

**Rationale**: Feature spec requires "simple web interface" accessible from any device. Responsive CSS is simpler than building separate mobile apps.

### Production Scale Targets

**Decision**: Initial MVP targets (can scale later if needed)
- 100-1,000 users
- Up to 50 wallets per user
- Up to 1,000 transactions per user
- Support for top 100 cryptocurrencies by market cap
- Database queries optimized for these scales

**Rationale**: Simplicity First principle - build for current needs, not hypothetical future scale. Database indexes and query optimization sufficient for these numbers.

## Technology Stack Summary

| Category | Decision | Bundle/Size | Rationale |
|----------|----------|-------------|-----------|
| **Backend Router** | chi v5 | ~1000 LOC | Stdlib-compatible, lightweight, YAGNI-aligned |
| **Backend JWT** | golang-jwt/jwt v5 | Minimal | Security-focused, actively maintained |
| **Backend DB Driver** | jackc/pgx v5 | Native driver | Modern, high-performance, active maintenance |
| **Backend Migrations** | golang-migrate/migrate v4 | CLI tool | Production-proven, database locking |
| **Frontend State** | React Hooks + Zustand | 1-3KB | Start simple, graduate when needed |
| **Frontend Routing** | React Router v6 | ~10-15KB | Battle-tested, feature-complete |
| **Frontend API** | TanStack Query + axios | ~53KB | Server state separation, caching built-in |
| **Price API** | CoinGecko (Free Demo) | External | Best free tier, historical data access |

## Resolution Summary

All NEEDS CLARIFICATION items from plan.md Technical Context:

1. ✅ HTTP router library choice → **chi (github.com/go-chi/chi/v5)**
2. ✅ JWT library choice → **golang-jwt/jwt (v5)**
3. ✅ Database migration tool choice → **golang-migrate/migrate (v4)**
4. ✅ State management library → **React Hooks + Zustand (when needed)**
5. ✅ Routing library → **React Router v6**
6. ✅ API client approach → **TanStack Query + axios**
7. ✅ Price API provider selection → **CoinGecko API (Free Demo Plan)**
8. ✅ Browser compatibility requirements → Modern browsers (Chrome 90+, Firefox 88+, Safari 14+, Edge 90+)
9. ✅ Mobile responsiveness requirements → Responsive CSS design
10. ✅ Expected production scale metrics → 100-1K users, 50 wallets/user, 1K transactions/user

## Implementation Checklist

### Backend Setup
- [ ] Initialize Go modules: `go mod init github.com/yourusername/moontrack`
- [ ] Install dependencies: chi, golang-jwt/jwt, golang-migrate, jackc/pgx/v5
- [ ] Set up project structure per plan.md (internal/ledger, internal/modules, cmd/api)
- [ ] Create first migration with golang-migrate
- [ ] Configure JWT secret in environment variables (never commit)

### Frontend Setup
- [ ] Initialize React 18+ project with Vite or Create React App
- [ ] Install dependencies: react-router-dom, @tanstack/react-query, axios
- [ ] Set up project structure per plan.md (features/, components/, services/)
- [ ] Configure axios interceptor for JWT token management
- [ ] Set up TanStack Query provider

### External Services
- [ ] Register for CoinGecko Demo API key
- [ ] Store API key in environment variables
- [ ] Set up Redis for price caching (60s TTL)
- [ ] Create price_snapshots table for historical prices
- [ ] Implement circuit breaker for API failures
- [ ] Create background job for price refresh (every 5 minutes)
- [ ] Implement optional manual price input in transaction creation (usd_rate field)

## Next Steps

After completing this research phase:
1. ✅ Update plan.md Technical Context to replace NEEDS CLARIFICATION items
2. → Proceed to Phase 1: Design (data-model.md, contracts/, quickstart.md)
3. → Run agent context update script
4. → Re-evaluate Constitution Check post-design
