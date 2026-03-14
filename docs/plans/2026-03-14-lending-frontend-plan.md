# Lending Frontend Integration Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add frontend support for displaying lending positions and lending transaction types (supply, withdraw, borrow, repay, claim).

**Architecture:** Mirror the LP Positions pattern — dedicated types, service, hook, and components. Lending positions appear as a new tab in wallet detail page. Transaction types get badges and filter entries.

**Tech Stack:** React 18, TanStack Query, Tailwind, Radix UI (shadcn), Lucide icons, Vite

---

### Task 1: Add lending transaction types

**Files:**
- Modify: `apps/frontend/src/types/transaction.ts:2-9`

**Step 1: Add lending types to TransactionType union**

In `apps/frontend/src/types/transaction.ts`, replace the `TransactionType` union (lines 2-9) with:

```typescript
export type TransactionType =
  | 'transfer_in'
  | 'transfer_out'
  | 'internal_transfer'
  | 'asset_adjustment'
  | 'lp_deposit'
  | 'lp_withdraw'
  | 'lp_claim_fees'
  | 'lending_supply'
  | 'lending_withdraw'
  | 'lending_borrow'
  | 'lending_repay'
  | 'lending_claim'
```

**Step 2: Verify build**

Run: `cd apps/frontend && bun run build`
Expected: SUCCESS (no type errors)

**Step 3: Commit**

```bash
git add apps/frontend/src/types/transaction.ts
git commit -m "feat(frontend): add lending transaction types"
```

---

### Task 2: Add lending badges to TransactionTypeBadge

**Files:**
- Modify: `apps/frontend/src/components/domain/TransactionTypeBadge.tsx:1,13-56`

**Step 1: Add lending entries to typeConfig**

In `apps/frontend/src/components/domain/TransactionTypeBadge.tsx`:

Add `Landmark` to the lucide import (line 1):

```typescript
import { ArrowDownLeft, ArrowUpRight, ArrowLeftRight, RefreshCw, ArrowDownToLine, ArrowUpFromLine, Coins, Landmark } from 'lucide-react'
```

Add these entries to `typeConfig` after the `lp_claim_fees` entry (after line 55):

```typescript
  lending_supply: {
    label: 'Supply',
    icon: ArrowDownToLine,
    variant: 'liquidity',
  },
  lending_withdraw: {
    label: 'Withdraw',
    icon: ArrowUpFromLine,
    variant: 'liquidity',
  },
  lending_borrow: {
    label: 'Borrow',
    icon: ArrowDownLeft,
    variant: 'profit',
  },
  lending_repay: {
    label: 'Repay',
    icon: ArrowUpRight,
    variant: 'loss',
  },
  lending_claim: {
    label: 'Claim',
    icon: Coins,
    variant: 'liquidity',
  },
```

**Step 2: Verify build**

Run: `cd apps/frontend && bun run build`
Expected: SUCCESS

**Step 3: Commit**

```bash
git add apps/frontend/src/components/domain/TransactionTypeBadge.tsx
git commit -m "feat(frontend): add lending transaction type badges"
```

---

### Task 3: Add lending + LP types to TransactionFilters

**Files:**
- Modify: `apps/frontend/src/features/transactions/TransactionFilters.tsx:17-22`

**Step 1: Expand the transactionTypes array**

In `apps/frontend/src/features/transactions/TransactionFilters.tsx`, replace the `transactionTypes` array (lines 17-22) with:

```typescript
const transactionTypes: { value: TransactionType; label: string }[] = [
  { value: 'transfer_in', label: 'Transfer In' },
  { value: 'transfer_out', label: 'Transfer Out' },
  { value: 'internal_transfer', label: 'Internal Transfer' },
  { value: 'asset_adjustment', label: 'Adjustment' },
  { value: 'lp_deposit', label: 'LP Deposit' },
  { value: 'lp_withdraw', label: 'LP Withdraw' },
  { value: 'lp_claim_fees', label: 'LP Claim' },
  { value: 'lending_supply', label: 'Supply' },
  { value: 'lending_withdraw', label: 'Withdraw' },
  { value: 'lending_borrow', label: 'Borrow' },
  { value: 'lending_repay', label: 'Repay' },
  { value: 'lending_claim', label: 'Claim' },
]
```

**Step 2: Verify build**

Run: `cd apps/frontend && bun run build`
Expected: SUCCESS

**Step 3: Commit**

```bash
git add apps/frontend/src/features/transactions/TransactionFilters.tsx
git commit -m "feat(frontend): add lending and LP types to transaction filters"
```

---

### Task 4: Create LendingPosition type

**Files:**
- Create: `apps/frontend/src/types/lendingPosition.ts`

**Step 1: Create the type file**

Create `apps/frontend/src/types/lendingPosition.ts`:

```typescript
export interface LendingPosition {
  id: string
  wallet_id: string
  chain_id: string
  protocol: string
  supply_asset: string
  supply_amount: string
  supply_decimals: number
  supply_contract?: string
  borrow_asset?: string
  borrow_amount: string
  borrow_decimals?: number
  borrow_contract?: string
  status: 'active' | 'closed'
  opened_at: string
  closed_at?: string
}
```

**Step 2: Verify build**

Run: `cd apps/frontend && bun run build`
Expected: SUCCESS

**Step 3: Commit**

```bash
git add apps/frontend/src/types/lendingPosition.ts
git commit -m "feat(frontend): add LendingPosition type"
```

---

### Task 5: Create lending position service

**Files:**
- Create: `apps/frontend/src/services/lendingPosition.ts`
- Reference: `apps/frontend/src/services/lpPosition.ts`

**Step 1: Create the service file**

Create `apps/frontend/src/services/lendingPosition.ts`:

```typescript
import api from './api'
import type { LendingPosition } from '@/types/lendingPosition'

export const listLendingPositions = async (
  walletId: string,
  status?: string
): Promise<LendingPosition[]> => {
  const params: Record<string, string> = { wallet_id: walletId }
  if (status) params.status = status
  const response = await api.get<LendingPosition[]>('/lending/positions', { params })
  return response.data
}

export const getLendingPosition = async (id: string): Promise<LendingPosition> => {
  const response = await api.get<LendingPosition>(`/lending/positions/${id}`)
  return response.data
}
```

**Step 2: Verify build**

Run: `cd apps/frontend && bun run build`
Expected: SUCCESS

**Step 3: Commit**

```bash
git add apps/frontend/src/services/lendingPosition.ts
git commit -m "feat(frontend): add lending position API service"
```

---

### Task 6: Create lending positions hook

**Files:**
- Create: `apps/frontend/src/hooks/useLendingPositions.ts`
- Reference: `apps/frontend/src/hooks/useLPPositions.ts`

**Step 1: Create the hook file**

Create `apps/frontend/src/hooks/useLendingPositions.ts`:

```typescript
import { useQuery } from '@tanstack/react-query'
import { listLendingPositions } from '@/services/lendingPosition'
import type { LendingPosition } from '@/types/lendingPosition'

export function useLendingPositions(walletId: string, status?: string) {
  return useQuery<LendingPosition[]>({
    queryKey: ['lending-positions', walletId, status],
    queryFn: () => listLendingPositions(walletId, status),
    staleTime: 1000 * 60 * 2,
    enabled: !!walletId,
  })
}
```

**Step 2: Verify build**

Run: `cd apps/frontend && bun run build`
Expected: SUCCESS

**Step 3: Commit**

```bash
git add apps/frontend/src/hooks/useLendingPositions.ts
git commit -m "feat(frontend): add useLendingPositions hook"
```

---

### Task 7: Create LendingPositionCard component

**Files:**
- Create: `apps/frontend/src/features/wallets/components/LendingPositionCard.tsx`
- Reference: `apps/frontend/src/features/wallets/components/LPPositionCard.tsx`

**Step 1: Create the component**

Create `apps/frontend/src/features/wallets/components/LendingPositionCard.tsx`:

```tsx
import { useState } from 'react'
import { ChevronDown } from 'lucide-react'
import { Card, CardContent } from '@/components/ui/card'
import { Badge } from '@/components/ui/badge'
import { AssetIcon } from '@/components/domain/AssetIcon'
import { ChainIcon } from '@/components/domain/ChainIcon'
import { formatDate, formatTokenAmount } from '@/lib/format'
import { cn } from '@/lib/utils'
import type { LendingPosition } from '@/types/lendingPosition'

interface LendingPositionCardProps {
  position: LendingPosition
}

export function LendingPositionCard({ position }: LendingPositionCardProps) {
  const [isExpanded, setIsExpanded] = useState(false)

  const hasBorrow = !!position.borrow_asset

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
            <AssetIcon symbol={position.supply_asset} size="sm" />
            <div>
              <div className="flex items-center gap-2">
                <span className="font-medium">{position.supply_asset}</span>
                {hasBorrow && (
                  <span className="text-muted-foreground">/ {position.borrow_asset}</span>
                )}
                <Badge variant={position.status === 'active' ? 'profit' : 'secondary'}>
                  {position.status === 'active' ? 'Active' : 'Closed'}
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

        {/* Balances row */}
        <div className="mt-3 grid grid-cols-2 gap-4">
          <div>
            <p className="text-xs text-muted-foreground">Supplied</p>
            <p className="text-sm font-medium font-mono">
              {formatTokenAmount(position.supply_amount, position.supply_decimals)} {position.supply_asset}
            </p>
          </div>
          {hasBorrow && (
            <div>
              <p className="text-xs text-muted-foreground">Borrowed</p>
              <p className="text-sm font-medium font-mono">
                {formatTokenAmount(position.borrow_amount, position.borrow_decimals || 0)} {position.borrow_asset}
              </p>
            </div>
          )}
        </div>

        {/* Expanded: additional details */}
        {isExpanded && (
          <div className="mt-4 border-t pt-4">
            <div className="flex flex-wrap gap-x-6 gap-y-1 text-sm text-muted-foreground">
              <span>Opened: {formatDate(position.opened_at)}</span>
              {position.closed_at && (
                <span>Closed: {formatDate(position.closed_at)}</span>
              )}
            </div>
          </div>
        )}
      </CardContent>
    </Card>
  )
}
```

**Step 2: Verify build**

Run: `cd apps/frontend && bun run build`
Expected: SUCCESS

**Step 3: Commit**

```bash
git add apps/frontend/src/features/wallets/components/LendingPositionCard.tsx
git commit -m "feat(frontend): add LendingPositionCard component"
```

---

### Task 8: Create LendingPositionsSection component

**Files:**
- Create: `apps/frontend/src/features/wallets/components/LendingPositionsSection.tsx`
- Reference: `apps/frontend/src/features/wallets/components/LPPositionsSection.tsx`

**Step 1: Create the component**

Create `apps/frontend/src/features/wallets/components/LendingPositionsSection.tsx`:

```tsx
import { useState } from 'react'
import { Landmark } from 'lucide-react'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Tabs, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { Skeleton } from '@/components/ui/skeleton'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { useLendingPositions } from '@/hooks/useLendingPositions'
import { LendingPositionCard } from './LendingPositionCard'

interface LendingPositionsSectionProps {
  walletId: string
}

type StatusFilter = 'all' | 'active' | 'closed'

export function LendingPositionsSection({ walletId }: LendingPositionsSectionProps) {
  const [statusFilter, setStatusFilter] = useState<StatusFilter>('all')

  const apiStatus = statusFilter === 'all' ? undefined : statusFilter
  const { data: positions, isLoading, error } = useLendingPositions(walletId, apiStatus)

  const sortedPositions = positions
    ? [...positions].sort((a, b) => {
        if (a.status !== b.status) {
          return a.status === 'active' ? -1 : 1
        }
        return new Date(b.opened_at).getTime() - new Date(a.opened_at).getTime()
      })
    : []

  return (
    <Card>
      <CardHeader className="pb-3">
        <div className="flex items-center justify-between">
          <CardTitle className="text-base font-medium flex items-center gap-2">
            Lending Positions
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
              <TabsTrigger value="active" className="text-xs px-2.5 h-6">Active</TabsTrigger>
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
            <AlertDescription>Failed to load lending positions</AlertDescription>
          </Alert>
        ) : sortedPositions.length === 0 ? (
          <div className="flex flex-col items-center justify-center py-8 text-muted-foreground">
            <div className="rounded-full bg-muted p-3 mb-3">
              <Landmark className="h-6 w-6" />
            </div>
            <p>No lending positions found</p>
            <p className="text-sm">
              {statusFilter !== 'all'
                ? 'Try adjusting your filter'
                : 'Lending positions will appear here once detected during sync'}
            </p>
          </div>
        ) : (
          <div className="space-y-3">
            {sortedPositions.map((position) => (
              <LendingPositionCard key={position.id} position={position} />
            ))}
          </div>
        )}
      </CardContent>
    </Card>
  )
}
```

**Step 2: Verify build**

Run: `cd apps/frontend && bun run build`
Expected: SUCCESS

**Step 3: Commit**

```bash
git add apps/frontend/src/features/wallets/components/LendingPositionsSection.tsx
git commit -m "feat(frontend): add LendingPositionsSection component"
```

---

### Task 9: Integrate lending tab into WalletDetailPage

**Files:**
- Modify: `apps/frontend/src/features/wallets/WalletDetailPage.tsx:24,214-215,283-285`

**Step 1: Add import**

In `apps/frontend/src/features/wallets/WalletDetailPage.tsx`, add import after the `LPPositionsSection` import (line 24):

```typescript
import { LendingPositionsSection } from './components/LendingPositionsSection'
```

**Step 2: Add tab trigger**

In the `TabsList` (around line 214), add a new trigger after the LP one:

```tsx
          <TabsTrigger value="lending">Lending</TabsTrigger>
```

**Step 3: Add tab content**

After the LP `TabsContent` (after line 285), add:

```tsx
        <TabsContent value="lending">
          <LendingPositionsSection walletId={id!} />
        </TabsContent>
```

**Step 4: Verify build**

Run: `cd apps/frontend && bun run build`
Expected: SUCCESS

**Step 5: Commit**

```bash
git add apps/frontend/src/features/wallets/WalletDetailPage.tsx
git commit -m "feat(frontend): add Lending tab to wallet detail page"
```

---

### Task 10: Final verification

**Step 1: Run lint**

Run: `cd apps/frontend && bun run lint`
Expected: No new errors

**Step 2: Run tests**

Run: `cd apps/frontend && bun run test --run`
Expected: All tests pass

**Step 3: Build check**

Run: `cd apps/frontend && bun run build`
Expected: SUCCESS
