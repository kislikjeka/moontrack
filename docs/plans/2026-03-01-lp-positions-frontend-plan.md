# LP Positions Frontend — Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Display LP positions as expandable cards in the wallet detail page, with status filtering and LP transaction type badges.

**Architecture:** New TypeScript types, API service, and TanStack Query hook for LP positions data layer. Two new components (`LPPositionsSection`, `LPPositionCard`) added inside `features/wallets/components/`. Existing `TransactionTypeBadge` extended with 3 LP types. A `formatTokenAmount` utility added for token-decimal formatting.

**Tech Stack:** React 18, TypeScript, TanStack Query, shadcn/ui (Tabs, Card, Badge, Skeleton), Tailwind CSS, Axios, Lucide icons.

**Design doc:** `docs/plans/2026-03-01-lp-positions-frontend-design.md`

---

### Task 1: Add LP Position Types and Transaction Type Extensions

**Files:**
- Create: `apps/frontend/src/types/lpPosition.ts`
- Modify: `apps/frontend/src/types/transaction.ts`

**Step 1: Create LP position type file**

```typescript
// apps/frontend/src/types/lpPosition.ts
export interface LPPosition {
  id: string
  wallet_id: string
  chain_id: string
  protocol: string
  nft_token_id?: string
  contract_address?: string
  token0_symbol: string
  token1_symbol: string
  token0_decimals: number
  token1_decimals: number
  total_deposited_usd: string
  total_withdrawn_usd: string
  total_claimed_fees_usd: string
  remaining_token0: string
  remaining_token1: string
  status: 'open' | 'closed'
  opened_at: string
  closed_at?: string
  realized_pnl_usd?: string
  apr_bps?: number
}
```

**Step 2: Add LP types to TransactionType union**

In `apps/frontend/src/types/transaction.ts`, update the `TransactionType` union:

```typescript
export type TransactionType =
  | 'transfer_in'
  | 'transfer_out'
  | 'internal_transfer'
  | 'asset_adjustment'
  | 'lp_deposit'
  | 'lp_withdraw'
  | 'lp_claim_fees'
```

**Step 3: Verify frontend compiles**

Run: `cd apps/frontend && npx tsc --noEmit`

**Step 4: Commit**

```
feat(types): add LP position type and LP transaction types
```

---

### Task 2: Add formatTokenAmount Utility

**Files:**
- Modify: `apps/frontend/src/lib/format.ts`

**Step 1: Add formatTokenAmount function**

At the end of `apps/frontend/src/lib/format.ts`, add:

```typescript
/**
 * Format a raw big number string to human-readable token amount
 * by dividing by 10^decimals.
 * Example: formatTokenAmount("2100000000", 6) → "2,100.00"
 */
export function formatTokenAmount(value: string, decimals: number): string {
  if (!value || value === '0') return '0'

  // Handle the division by padding/splitting the string
  const isNegative = value.startsWith('-')
  const abs = isNegative ? value.slice(1) : value
  const padded = abs.padStart(decimals + 1, '0')
  const intPart = padded.slice(0, padded.length - decimals) || '0'
  const fracPart = decimals > 0 ? padded.slice(padded.length - decimals) : ''

  // Trim trailing zeros but keep at least 2 decimal places for readability
  const trimmed = fracPart.replace(/0+$/, '')
  const displayFrac = trimmed.length < 2 ? fracPart.slice(0, 2) : trimmed
  const displayFracClean = displayFrac.length < 2
    ? displayFrac.padEnd(2, '0')
    : displayFrac

  // Format integer part with commas
  const intFormatted = parseInt(intPart, 10).toLocaleString('en-US')

  const result = displayFracClean
    ? `${intFormatted}.${displayFracClean}`
    : intFormatted

  return isNegative ? `-${result}` : result
}
```

**Step 2: Verify frontend compiles**

Run: `cd apps/frontend && npx tsc --noEmit`

**Step 3: Commit**

```
feat(format): add formatTokenAmount utility for LP token display
```

---

### Task 3: Add LP Position API Service and Query Hook

**Files:**
- Create: `apps/frontend/src/services/lpPosition.ts`
- Create: `apps/frontend/src/hooks/useLPPositions.ts`

**Step 1: Create API service**

Follow the pattern from `services/portfolio.ts`:

```typescript
// apps/frontend/src/services/lpPosition.ts
import api from './api'
import type { LPPosition } from '@/types/lpPosition'

export const listLPPositions = async (
  walletId: string,
  status?: string
): Promise<LPPosition[]> => {
  const params: Record<string, string> = { wallet_id: walletId }
  if (status) params.status = status
  const response = await api.get<LPPosition[]>('/lp/positions', { params })
  return response.data
}

export const getLPPosition = async (id: string): Promise<LPPosition> => {
  const response = await api.get<LPPosition>(`/lp/positions/${id}`)
  return response.data
}
```

**Step 2: Create query hook**

Follow the pattern from `hooks/usePortfolio.ts`:

```typescript
// apps/frontend/src/hooks/useLPPositions.ts
import { useQuery } from '@tanstack/react-query'
import { listLPPositions } from '@/services/lpPosition'
import type { LPPosition } from '@/types/lpPosition'

export function useLPPositions(walletId: string, status?: string) {
  return useQuery<LPPosition[]>({
    queryKey: ['lp-positions', walletId, status],
    queryFn: () => listLPPositions(walletId, status),
    staleTime: 1000 * 60 * 2, // 2 minutes
    enabled: !!walletId,
  })
}
```

**Step 3: Verify frontend compiles**

Run: `cd apps/frontend && npx tsc --noEmit`

**Step 4: Commit**

```
feat(lp): add LP position API service and query hook
```

---

### Task 4: Update TransactionTypeBadge with LP Types

**Files:**
- Modify: `apps/frontend/src/components/domain/TransactionTypeBadge.tsx`

**Step 1: Add LP type entries to typeConfig**

Add imports for new icons at the top:

```typescript
import { ArrowDownLeft, ArrowUpRight, ArrowLeftRight, RefreshCw, ArrowDownToLine, ArrowUpFromLine, Coins } from 'lucide-react'
```

Add 3 new entries to the `typeConfig` Record. Update the Record type to use `string` keys instead of strict `TransactionType` (since the backend may return types not yet in the union), or extend the type. The simplest approach: add the entries to the existing `typeConfig` object:

```typescript
lp_deposit: {
  label: 'LP Deposit',
  icon: ArrowDownToLine,
  variant: 'liquidity',
},
lp_withdraw: {
  label: 'LP Withdraw',
  icon: ArrowUpFromLine,
  variant: 'liquidity',
},
lp_claim_fees: {
  label: 'LP Claim',
  icon: Coins,
  variant: 'liquidity',
},
```

Update the variant type in the Record to include `'liquidity'`:

```typescript
const typeConfig: Record<
  TransactionType,
  {
    label: string
    icon: React.ElementType
    variant: 'profit' | 'loss' | 'transfer' | 'liquidity'
  }
> = {
```

**Step 2: Add fallback for unknown types**

Update the component to handle types not in typeConfig gracefully:

```typescript
export function TransactionTypeBadge({
  type,
  size = 'default',
  showLabel = true,
  className,
}: TransactionTypeBadgeProps) {
  const config = typeConfig[type]
  if (!config) {
    return (
      <Badge variant="secondary" className={cn('gap-1', sizeConfig[size].text, className)}>
        {showLabel && <span>{type}</span>}
      </Badge>
    )
  }
  const sizes = sizeConfig[size]
  const Icon = config.icon

  return (
    <Badge
      variant={config.variant}
      className={cn('gap-1', sizes.text, className)}
    >
      <Icon className={sizes.iconSize} />
      {showLabel && <span>{config.label}</span>}
    </Badge>
  )
}
```

**Step 3: Verify frontend compiles**

Run: `cd apps/frontend && npx tsc --noEmit`

**Step 4: Commit**

```
feat(ui): add LP transaction type badges to TransactionTypeBadge
```

---

### Task 5: Create LPPositionCard Component

**Files:**
- Create: `apps/frontend/src/features/wallets/components/LPPositionCard.tsx`

**Step 1: Create the expandable card component**

```typescript
// apps/frontend/src/features/wallets/components/LPPositionCard.tsx
import { useState } from 'react'
import { ChevronDown } from 'lucide-react'
import { Card, CardContent } from '@/components/ui/card'
import { Badge } from '@/components/ui/badge'
import { AssetIcon } from '@/components/domain/AssetIcon'
import { ChainIcon } from '@/components/domain/ChainIcon'
import { PnLValue } from '@/components/domain/PnLValue'
import { formatUSD, formatDate, formatTokenAmount } from '@/lib/format'
import { cn } from '@/lib/utils'
import type { LPPosition } from '@/types/lpPosition'

interface LPPositionCardProps {
  position: LPPosition
}

export function LPPositionCard({ position }: LPPositionCardProps) {
  const [isExpanded, setIsExpanded] = useState(false)

  const depositedUSD = parseFloat(position.total_deposited_usd) / 1e8
  const withdrawnUSD = parseFloat(position.total_withdrawn_usd) / 1e8
  const feesUSD = parseFloat(position.total_claimed_fees_usd) / 1e8
  const aprPercent = position.apr_bps != null ? position.apr_bps / 100 : null

  return (
    <Card
      className={cn(
        'cursor-pointer transition-colors hover:bg-accent/50',
        isExpanded && 'bg-accent/30'
      )}
      onClick={() => setIsExpanded(!isExpanded)}
    >
      <CardContent className="p-4">
        {/* Collapsed: always visible */}
        <div className="flex items-start justify-between">
          <div className="flex items-center gap-3">
            <div className="flex -space-x-2">
              <AssetIcon symbol={position.token0_symbol} size="sm" />
              <AssetIcon symbol={position.token1_symbol} size="sm" />
            </div>
            <div>
              <div className="flex items-center gap-2">
                <span className="font-medium">
                  {position.token0_symbol} / {position.token1_symbol}
                </span>
                <Badge variant={position.status === 'open' ? 'profit' : 'secondary'}>
                  {position.status === 'open' ? 'Open' : 'Closed'}
                </Badge>
                <ChainIcon chainId={position.chain_id} size="xs" showTooltip />
              </div>
              <p className="text-sm text-muted-foreground">{position.protocol}</p>
            </div>
          </div>
          <ChevronDown
            className={cn(
              'h-4 w-4 text-muted-foreground transition-transform',
              isExpanded && 'rotate-180'
            )}
          />
        </div>

        {/* Metrics row */}
        <div className="mt-3 grid grid-cols-3 gap-4">
          <div>
            <p className="text-xs text-muted-foreground">Deposited</p>
            <p className="text-sm font-medium font-mono">{formatUSD(depositedUSD)}</p>
          </div>
          <div>
            <p className="text-xs text-muted-foreground">Fees Earned</p>
            <p className="text-sm font-medium font-mono">{formatUSD(feesUSD)}</p>
          </div>
          <div>
            <p className="text-xs text-muted-foreground">APR</p>
            <p className="text-sm font-medium font-mono">
              {aprPercent != null ? `${aprPercent.toFixed(2)}%` : '—'}
            </p>
          </div>
        </div>

        {/* Expanded: additional details */}
        {isExpanded && (
          <div className="mt-4 border-t pt-4 space-y-4">
            {/* USD breakdown */}
            <div className="grid grid-cols-3 gap-4">
              <div>
                <p className="text-xs text-muted-foreground">Deposited</p>
                <p className="text-sm font-mono">{formatUSD(depositedUSD)}</p>
              </div>
              <div>
                <p className="text-xs text-muted-foreground">Withdrawn</p>
                <p className="text-sm font-mono">{formatUSD(withdrawnUSD)}</p>
              </div>
              <div>
                <p className="text-xs text-muted-foreground">Fees Claimed</p>
                <p className="text-sm font-mono">{formatUSD(feesUSD)}</p>
              </div>
            </div>

            {/* Token breakdown */}
            <div>
              <p className="text-xs text-muted-foreground mb-2">Remaining Tokens</p>
              <div className="space-y-1">
                <div className="flex items-center justify-between text-sm">
                  <div className="flex items-center gap-2">
                    <AssetIcon symbol={position.token0_symbol} size="sm" />
                    <span>{position.token0_symbol}</span>
                  </div>
                  <span className="font-mono">
                    {formatTokenAmount(position.remaining_token0, position.token0_decimals)}
                  </span>
                </div>
                <div className="flex items-center justify-between text-sm">
                  <div className="flex items-center gap-2">
                    <AssetIcon symbol={position.token1_symbol} size="sm" />
                    <span>{position.token1_symbol}</span>
                  </div>
                  <span className="font-mono">
                    {formatTokenAmount(position.remaining_token1, position.token1_decimals)}
                  </span>
                </div>
              </div>
            </div>

            {/* Metadata */}
            <div className="flex flex-wrap gap-x-6 gap-y-1 text-sm text-muted-foreground">
              <span>Opened: {formatDate(position.opened_at)}</span>
              {position.closed_at && (
                <span>Closed: {formatDate(position.closed_at)}</span>
              )}
              {position.nft_token_id && (
                <span>NFT ID: #{position.nft_token_id}</span>
              )}
            </div>

            {/* PnL for closed positions */}
            {position.status === 'closed' && position.realized_pnl_usd != null && (
              <div className="flex items-center gap-2">
                <span className="text-sm text-muted-foreground">Realized PnL:</span>
                <PnLValue
                  value={parseFloat(position.realized_pnl_usd) / 1e8}
                  showIcon
                  size="sm"
                />
              </div>
            )}
          </div>
        )}
      </CardContent>
    </Card>
  )
}
```

> **Note on USD parsing:** The backend stores USD values as `NUMERIC(78,0)` with 8 implied decimal places (i.e., `10000000000` = $100.00). Divide by `1e8` before displaying. Verify this matches the actual backend response format — if the backend already returns human-readable values, remove the `/ 1e8` division.

**Step 2: Verify frontend compiles**

Run: `cd apps/frontend && npx tsc --noEmit`

**Step 3: Commit**

```
feat(wallets): add LPPositionCard component with expand/collapse
```

---

### Task 6: Create LPPositionsSection Component

**Files:**
- Create: `apps/frontend/src/features/wallets/components/LPPositionsSection.tsx`

**Step 1: Create the section component**

```typescript
// apps/frontend/src/features/wallets/components/LPPositionsSection.tsx
import { useState } from 'react'
import { Droplets } from 'lucide-react'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Tabs, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { Skeleton } from '@/components/ui/skeleton'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { useLPPositions } from '@/hooks/useLPPositions'
import { LPPositionCard } from './LPPositionCard'

interface LPPositionsSectionProps {
  walletId: string
}

type StatusFilter = 'all' | 'open' | 'closed'

export function LPPositionsSection({ walletId }: LPPositionsSectionProps) {
  const [statusFilter, setStatusFilter] = useState<StatusFilter>('all')

  const apiStatus = statusFilter === 'all' ? undefined : statusFilter
  const { data: positions, isLoading, error } = useLPPositions(walletId, apiStatus)

  // Sort: open first, then closed; within each group by opened_at desc
  const sortedPositions = positions
    ? [...positions].sort((a, b) => {
        if (a.status !== b.status) {
          return a.status === 'open' ? -1 : 1
        }
        return new Date(b.opened_at).getTime() - new Date(a.opened_at).getTime()
      })
    : []

  return (
    <Card>
      <CardHeader className="pb-3">
        <div className="flex items-center justify-between">
          <CardTitle className="text-base font-medium flex items-center gap-2">
            LP Positions
            {positions && positions.length > 0 && (
              <span className="text-sm font-normal text-muted-foreground">
                ({positions.length})
              </span>
            )}
          </CardTitle>
          <Tabs
            value={statusFilter}
            onValueChange={(v) => setStatusFilter(v as StatusFilter)}
          >
            <TabsList className="h-8">
              <TabsTrigger value="all" className="text-xs px-2.5 h-6">All</TabsTrigger>
              <TabsTrigger value="open" className="text-xs px-2.5 h-6">Open</TabsTrigger>
              <TabsTrigger value="closed" className="text-xs px-2.5 h-6">Closed</TabsTrigger>
            </TabsList>
          </Tabs>
        </div>
      </CardHeader>
      <CardContent>
        {isLoading ? (
          <div className="space-y-3">
            {[...Array(2)].map((_, i) => (
              <Skeleton key={i} className="h-28" />
            ))}
          </div>
        ) : error ? (
          <Alert variant="destructive">
            <AlertDescription>Failed to load LP positions</AlertDescription>
          </Alert>
        ) : sortedPositions.length === 0 ? (
          <div className="flex flex-col items-center justify-center py-8 text-muted-foreground">
            <div className="rounded-full bg-muted p-3 mb-3">
              <Droplets className="h-6 w-6" />
            </div>
            <p>No LP positions found</p>
            <p className="text-sm">
              {statusFilter !== 'all'
                ? 'Try adjusting your filter'
                : 'LP positions will appear here once detected during sync'}
            </p>
          </div>
        ) : (
          <div className="space-y-3">
            {sortedPositions.map((position) => (
              <LPPositionCard key={position.id} position={position} />
            ))}
          </div>
        )}
      </CardContent>
    </Card>
  )
}
```

**Step 2: Verify frontend compiles**

Run: `cd apps/frontend && npx tsc --noEmit`

**Step 3: Commit**

```
feat(wallets): add LPPositionsSection with status filtering
```

---

### Task 7: Integrate LPPositionsSection into WalletDetailPage

**Files:**
- Modify: `apps/frontend/src/features/wallets/WalletDetailPage.tsx`

**Step 1: Add import**

At the top of `WalletDetailPage.tsx`, add:

```typescript
import { LPPositionsSection } from './components/LPPositionsSection'
```

**Step 2: Add LP Positions tab**

Add a new `TabsTrigger` after the "Transactions" trigger:

```typescript
<TabsTrigger value="lp">LP Positions</TabsTrigger>
```

Add a new `TabsContent` after the transactions `TabsContent`:

```typescript
<TabsContent value="lp">
  <LPPositionsSection walletId={id!} />
</TabsContent>
```

**Step 3: Verify frontend compiles**

Run: `cd apps/frontend && npx tsc --noEmit`

**Step 4: Verify in browser**

Run: `cd apps/frontend && bun run dev`

Open a wallet detail page and click the "LP Positions" tab. Verify:
- Tab appears after Transactions
- Loading skeleton shows initially
- Empty state shows if no LP positions
- If positions exist, cards render with correct data

**Step 5: Commit**

```
feat(wallets): integrate LP positions tab into wallet detail page
```

---

### Task 8: Visual Verification and Polish

**Step 1: Verify all transaction type badges**

Navigate to the transactions list. Confirm LP transaction types (`lp_deposit`, `lp_withdraw`, `lp_claim_fees`) render with the purple `liquidity` badge variant and correct icons.

**Step 2: Verify LP card expand/collapse**

On a wallet with LP positions:
- Click a card → verify it expands with details
- Click again → verify it collapses
- Verify token amounts use correct decimal formatting
- Verify dates display correctly
- Verify PnL shows for closed positions

**Step 3: Verify status filter**

Click All → Open → Closed tabs. Verify:
- "All" shows all positions
- "Open" shows only open
- "Closed" shows only closed
- Count updates in the header

**Step 4: Verify empty/error states**

- Open a wallet with no LP positions → "No LP positions found" message
- Verify filter message: "Try adjusting your filter" when filter is active

**Step 5: Fix any issues found during verification**

**Step 6: Final commit if any fixes were needed**

```
fix(wallets): polish LP positions UI
```

---

## Task Dependency Graph

```
Task 1 (types) ──────────────────────────┐
                                          │
Task 2 (formatTokenAmount) ──────────────┤
                                          │
Task 3 (service + hook) ─────────────────┤
                                          │
Task 4 (TransactionTypeBadge) ───────────┤
                                          │
Task 5 (LPPositionCard) ◄── 1, 2        │
                                          │
Task 6 (LPPositionsSection) ◄── 3, 5    │
                                          │
Task 7 (WalletDetailPage integration) ◄── 6
                                          │
Task 8 (verification + polish) ◄── 7, 4
```

Tasks 1-4 are independent and can run in parallel. Task 5 depends on 1 and 2. Task 6 depends on 3 and 5. Task 7 depends on 6. Task 8 is final verification.
