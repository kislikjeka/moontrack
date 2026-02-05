---
name: frontend-development
description: This skill should be used when the user asks to "create a new page", "add a component", "build a form", "add a feature to frontend", "create UI component", "design a screen", "add route", "style component", "use design tokens", "add shadcn component", or needs guidance on React frontend architecture, Tailwind styling, component patterns, or design system for MoonTrack.
---

# Frontend Development for MoonTrack

Create pages, components, and features following MoonTrack's frontend architecture and design system.

## When to Use

- Creating new pages or routes
- Building UI components (domain or generic)
- Adding forms with validation
- Styling with Tailwind and design tokens
- Working with React Query hooks
- Implementing responsive layouts

## Architecture Overview

MoonTrack frontend uses a feature-based architecture with shared UI components:

```
apps/frontend/src/
├── app/              # App shell (App.tsx, providers.tsx)
├── components/
│   ├── ui/           # shadcn/ui primitives (Button, Card, Input...)
│   ├── domain/       # Business components (WalletCard, TransactionTypeBadge...)
│   └── layout/       # Layout components (Sidebar, Header, Layout)
├── features/         # Feature modules by domain
│   ├── auth/         # LoginPage, RegisterPage, AuthContext
│   ├── dashboard/    # DashboardPage, PortfolioSummary, charts
│   ├── wallets/      # WalletsPage, WalletDetailPage, dialogs
│   ├── transactions/ # TransactionsPage, TransactionFormPage
│   └── settings/     # SettingsPage, ProfileSection
├── hooks/            # React Query hooks (useWallets, useTransactions)
├── services/         # API clients (api.ts, wallet.ts, transaction.ts)
├── types/            # TypeScript types (wallet.ts, transaction.ts)
├── lib/              # Utilities (utils.ts, format.ts)
└── styles/           # Global styles (globals.css with design tokens)
```

**Key constraints:**
- Features are self-contained modules with pages and feature-specific components
- UI components are generic, domain components know about business types
- All styling via Tailwind classes, no custom CSS files
- TypeScript strict mode, no `any` types

## Creating a New Page

### Step 1: Create the Page Component

Create page in appropriate feature folder:

```tsx
// features/portfolio/PortfolioPage.tsx
import { usePortfolio } from '@/hooks/usePortfolio'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Skeleton } from '@/components/ui/skeleton'

export default function PortfolioPage() {
  const { data: portfolio, isLoading, error } = usePortfolio()

  if (isLoading) {
    return <PortfolioSkeleton />
  }

  if (error) {
    return <div className="text-destructive">Failed to load portfolio</div>
  }

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-2xl font-bold tracking-tight">Portfolio</h1>
        <p className="text-muted-foreground">Your asset overview</p>
      </div>
      {/* Page content */}
    </div>
  )
}

function PortfolioSkeleton() {
  return (
    <div className="space-y-6">
      <Skeleton className="h-8 w-48" />
      <Skeleton className="h-64 w-full" />
    </div>
  )
}
```

### Step 2: Add Route in App.tsx

```tsx
// app/App.tsx
import PortfolioPage from '@/features/portfolio/PortfolioPage'

// Inside Routes:
<Route
  path="/portfolio"
  element={
    <ProtectedRoute>
      <Layout>
        <PortfolioPage />
      </Layout>
    </ProtectedRoute>
  }
/>
```

### Step 3: Add Navigation Link (if needed)

Update Sidebar.tsx navItems array:

```tsx
const navItems = [
  { to: '/dashboard', label: 'Dashboard', icon: LayoutDashboard },
  { to: '/portfolio', label: 'Portfolio', icon: PieChart },  // Add new item
  // ...
]
```

## Creating Components

### UI Components (components/ui/)

Generic, reusable primitives built on Radix UI. Follow shadcn/ui patterns:

```tsx
// components/ui/progress.tsx
import * as React from 'react'
import * as ProgressPrimitive from '@radix-ui/react-progress'
import { cn } from '@/lib/utils'

const Progress = React.forwardRef<
  React.ElementRef<typeof ProgressPrimitive.Root>,
  React.ComponentPropsWithoutRef<typeof ProgressPrimitive.Root>
>(({ className, value, ...props }, ref) => (
  <ProgressPrimitive.Root
    ref={ref}
    className={cn(
      'relative h-2 w-full overflow-hidden rounded-full bg-primary/20',
      className
    )}
    {...props}
  >
    <ProgressPrimitive.Indicator
      className="h-full bg-primary transition-all"
      style={{ width: `${value || 0}%` }}
    />
  </ProgressPrimitive.Root>
))
Progress.displayName = 'Progress'

export { Progress }
```

### Domain Components (components/domain/)

Business-aware components that know about MoonTrack types:

```tsx
// components/domain/AssetBalance.tsx
import { formatAmount, formatUSD } from '@/lib/format'
import { cn } from '@/lib/utils'
import type { AssetHolding } from '@/types/portfolio'

interface AssetBalanceProps {
  holding: AssetHolding
  showUSD?: boolean
  className?: string
}

export function AssetBalance({ holding, showUSD = true, className }: AssetBalanceProps) {
  return (
    <div className={cn('flex flex-col', className)}>
      <span className="font-medium font-mono">
        {formatAmount(holding.amount)} {holding.symbol}
      </span>
      {showUSD && (
        <span className="text-sm text-muted-foreground">
          {formatUSD(holding.usd_value)}
        </span>
      )}
    </div>
  )
}
```

## Design System

### Color Tokens

Primary colors use cyan/teal scheme. Access via Tailwind classes:

| Token | Light | Dark | Usage |
|-------|-------|------|-------|
| `primary` | cyan-600 | cyan-400 | Buttons, links, focus rings |
| `profit` | green-600 | green-400 | Positive values, gains |
| `loss` | red-500 | red-400 | Negative values, losses |
| `muted-foreground` | slate-500 | slate-400 | Secondary text |

### Transaction Type Colors

Each transaction type has dedicated colors:

```tsx
// Usage in components
<Badge className="bg-tx-swap-bg text-tx-swap">Swap</Badge>
<Badge className="bg-tx-transfer-bg text-tx-transfer">Transfer</Badge>
```

Available types: `tx-swap`, `tx-transfer`, `tx-bridge`, `tx-liquidity`, `tx-gm-pool`

### Spacing and Layout

Use consistent spacing scale:

```tsx
// Page container
<div className="space-y-6">  {/* 24px gap between sections */}

// Card content
<CardContent className="p-4">  {/* 16px padding */}

// Form fields
<div className="space-y-4">  {/* 16px gap between fields */}

// Inline items
<div className="flex items-center gap-3">  {/* 12px gap */}
```

### Typography

```tsx
// Page title
<h1 className="text-2xl font-bold tracking-tight">Title</h1>

// Page subtitle
<p className="text-muted-foreground">Description text</p>

// Card title
<CardTitle className="text-base">Card Title</CardTitle>

// Monospace for numbers/addresses
<span className="font-mono">0x1234...5678</span>
```

## React Query Patterns

### Creating a Hook

```tsx
// hooks/useAssets.ts
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import * as assetService from '@/services/asset'
import type { Asset } from '@/types/asset'

export function useAssets() {
  return useQuery({
    queryKey: ['assets'],
    queryFn: assetService.getAssets,
  })
}

export function useAsset(id: string) {
  return useQuery({
    queryKey: ['assets', id],
    queryFn: () => assetService.getAsset(id),
    enabled: !!id,
  })
}

export function useCreateAsset() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: assetService.createAsset,
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['assets'] })
    },
  })
}
```

### Using Hooks in Components

```tsx
function AssetList() {
  const { data: assets, isLoading, error } = useAssets()
  const createAsset = useCreateAsset()

  const handleCreate = async (data: CreateAssetRequest) => {
    try {
      await createAsset.mutateAsync(data)
      toast.success('Asset created')
    } catch {
      toast.error('Failed to create asset')
    }
  }

  // ...
}
```

## Form Patterns

### Basic Form with Validation

```tsx
import { useState } from 'react'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { toast } from 'sonner'

function CreateForm() {
  const [name, setName] = useState('')
  const [isSubmitting, setIsSubmitting] = useState(false)

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault()

    if (!name.trim()) {
      toast.error('Name is required')
      return
    }

    setIsSubmitting(true)
    try {
      await createItem({ name })
      toast.success('Created successfully')
    } catch {
      toast.error('Failed to create')
    } finally {
      setIsSubmitting(false)
    }
  }

  return (
    <form onSubmit={handleSubmit} className="space-y-4">
      <div className="space-y-2">
        <Label htmlFor="name">Name</Label>
        <Input
          id="name"
          value={name}
          onChange={(e) => setName(e.target.value)}
          placeholder="Enter name"
        />
      </div>
      <Button type="submit" disabled={isSubmitting}>
        {isSubmitting ? 'Creating...' : 'Create'}
      </Button>
    </form>
  )
}
```

### Form in Dialog

```tsx
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from '@/components/ui/dialog'

function CreateDialog() {
  const [open, setOpen] = useState(false)

  const handleSuccess = () => {
    setOpen(false)
    toast.success('Created')
  }

  return (
    <Dialog open={open} onOpenChange={setOpen}>
      <DialogTrigger asChild>
        <Button>Create New</Button>
      </DialogTrigger>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>Create Item</DialogTitle>
        </DialogHeader>
        <CreateForm onSuccess={handleSuccess} />
      </DialogContent>
    </Dialog>
  )
}
```

## Available UI Components

Located in `components/ui/`:

| Component | Usage |
|-----------|-------|
| `Button` | Actions, links (variant: default/outline/ghost/destructive) |
| `Card` | Content containers (Card, CardHeader, CardContent, CardTitle) |
| `Input` | Text inputs |
| `Label` | Form labels |
| `Select` | Dropdown selects |
| `Dialog` | Modal dialogs |
| `Sheet` | Side panels (mobile nav) |
| `Table` | Data tables |
| `Tabs` | Tabbed content |
| `Badge` | Status indicators |
| `DropdownMenu` | Context menus |
| `Skeleton` | Loading placeholders |
| `Separator` | Visual dividers |
| `Tooltip` | Hover hints |
| `ScrollArea` | Scrollable containers |

## Validation Checklist

Before committing frontend changes:

1. **TypeScript** - No `any` types, proper interfaces defined
2. **Styling** - Only Tailwind classes, no inline styles or CSS files
3. **Components** - UI components generic, domain components in domain/
4. **Hooks** - React Query for data fetching, proper loading/error states
5. **Accessibility** - Labels for inputs, aria-labels for icon buttons
6. **Responsive** - Mobile-first, test at 375px width
7. **Theme** - Works in both light and dark mode

## Additional Resources

### Reference Files

For detailed component examples and patterns:
- **`references/design-tokens.md`** - Complete CSS variables and color system
- **`references/component-catalog.md`** - All available components with examples

### Example Files

Working examples in `examples/`:
- **`examples/feature-page.tsx`** - Complete page template with loading states
- **`examples/form-dialog.tsx`** - Form in dialog pattern
