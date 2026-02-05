# MoonTrack Component Catalog

Complete reference of all UI and domain components available in MoonTrack frontend.

## UI Components (components/ui/)

Generic, reusable components built on Radix UI primitives following shadcn/ui patterns.

### Button

Interactive button with multiple variants.

```tsx
import { Button } from '@/components/ui/button'

// Variants
<Button>Default</Button>
<Button variant="outline">Outline</Button>
<Button variant="ghost">Ghost</Button>
<Button variant="destructive">Destructive</Button>

// Sizes
<Button size="sm">Small</Button>
<Button size="default">Default</Button>
<Button size="lg">Large</Button>
<Button size="icon"><Icon /></Button>

// With loading state
<Button disabled={isLoading}>
  {isLoading ? (
    <>
      <Loader2 className="mr-2 h-4 w-4 animate-spin" />
      Loading...
    </>
  ) : (
    'Submit'
  )}
</Button>

// As link
<Button asChild>
  <Link to="/path">Navigate</Link>
</Button>
```

### Card

Content container with optional header and footer.

```tsx
import { Card, CardContent, CardHeader, CardTitle, CardDescription, CardFooter } from '@/components/ui/card'

<Card>
  <CardHeader>
    <CardTitle>Title</CardTitle>
    <CardDescription>Optional description</CardDescription>
  </CardHeader>
  <CardContent>
    Main content here
  </CardContent>
  <CardFooter>
    <Button>Action</Button>
  </CardFooter>
</Card>

// Compact card (no header)
<Card>
  <CardContent className="p-4">
    Content only
  </CardContent>
</Card>

// Clickable card
<Card className="cursor-pointer hover:border-border-hover transition-colors">
  <CardContent>Clickable</CardContent>
</Card>
```

### Input

Text input field.

```tsx
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'

// Basic
<Input placeholder="Enter text" />

// With label
<div className="space-y-2">
  <Label htmlFor="email">Email</Label>
  <Input id="email" type="email" placeholder="name@example.com" />
</div>

// With icon
<div className="relative">
  <Search className="absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-muted-foreground" />
  <Input className="pl-10" placeholder="Search..." />
</div>

// Number input
<Input type="number" step="any" placeholder="0.00" />

// Disabled
<Input disabled value="Cannot edit" />
```

### Select

Dropdown selection.

```tsx
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'

<Select value={selected} onValueChange={setSelected}>
  <SelectTrigger>
    <SelectValue placeholder="Select option" />
  </SelectTrigger>
  <SelectContent>
    <SelectItem value="option1">Option 1</SelectItem>
    <SelectItem value="option2">Option 2</SelectItem>
    <SelectItem value="option3">Option 3</SelectItem>
  </SelectContent>
</Select>

// With label
<div className="space-y-2">
  <Label>Category</Label>
  <Select>...</Select>
</div>
```

### Dialog

Modal dialog for forms and confirmations.

```tsx
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
  DialogFooter,
  DialogTrigger,
  DialogClose,
} from '@/components/ui/dialog'

// Controlled dialog
const [open, setOpen] = useState(false)

<Dialog open={open} onOpenChange={setOpen}>
  <DialogTrigger asChild>
    <Button>Open Dialog</Button>
  </DialogTrigger>
  <DialogContent>
    <DialogHeader>
      <DialogTitle>Dialog Title</DialogTitle>
      <DialogDescription>Optional description text.</DialogDescription>
    </DialogHeader>
    <div className="py-4">
      {/* Dialog body content */}
    </div>
    <DialogFooter>
      <DialogClose asChild>
        <Button variant="outline">Cancel</Button>
      </DialogClose>
      <Button onClick={handleSubmit}>Confirm</Button>
    </DialogFooter>
  </DialogContent>
</Dialog>
```

### Sheet

Side panel, typically for mobile navigation.

```tsx
import {
  Sheet,
  SheetContent,
  SheetHeader,
  SheetTitle,
  SheetTrigger,
} from '@/components/ui/sheet'

<Sheet>
  <SheetTrigger asChild>
    <Button variant="ghost" size="icon">
      <Menu className="h-5 w-5" />
    </Button>
  </SheetTrigger>
  <SheetContent side="left">
    <SheetHeader>
      <SheetTitle>Navigation</SheetTitle>
    </SheetHeader>
    <nav className="mt-4">
      {/* Navigation items */}
    </nav>
  </SheetContent>
</Sheet>
```

### Table

Data table for lists.

```tsx
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'

<Table>
  <TableHeader>
    <TableRow>
      <TableHead>Name</TableHead>
      <TableHead>Amount</TableHead>
      <TableHead className="text-right">Value</TableHead>
    </TableRow>
  </TableHeader>
  <TableBody>
    {items.map((item) => (
      <TableRow key={item.id}>
        <TableCell className="font-medium">{item.name}</TableCell>
        <TableCell className="font-mono">{item.amount}</TableCell>
        <TableCell className="text-right">{formatUSD(item.value)}</TableCell>
      </TableRow>
    ))}
  </TableBody>
</Table>

// Empty state
{items.length === 0 && (
  <TableRow>
    <TableCell colSpan={3} className="text-center text-muted-foreground py-8">
      No items found
    </TableCell>
  </TableRow>
)}
```

### Tabs

Tabbed content navigation.

```tsx
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'

<Tabs defaultValue="tab1">
  <TabsList>
    <TabsTrigger value="tab1">Tab 1</TabsTrigger>
    <TabsTrigger value="tab2">Tab 2</TabsTrigger>
    <TabsTrigger value="tab3">Tab 3</TabsTrigger>
  </TabsList>
  <TabsContent value="tab1">
    Content for tab 1
  </TabsContent>
  <TabsContent value="tab2">
    Content for tab 2
  </TabsContent>
  <TabsContent value="tab3">
    Content for tab 3
  </TabsContent>
</Tabs>

// Full-width tabs
<TabsList className="grid w-full grid-cols-3">
  <TabsTrigger value="tab1">Tab 1</TabsTrigger>
  <TabsTrigger value="tab2">Tab 2</TabsTrigger>
  <TabsTrigger value="tab3">Tab 3</TabsTrigger>
</TabsList>
```

### Badge

Status indicator or label.

```tsx
import { Badge } from '@/components/ui/badge'

// Variants
<Badge>Default</Badge>
<Badge variant="secondary">Secondary</Badge>
<Badge variant="outline">Outline</Badge>
<Badge variant="destructive">Destructive</Badge>

// Custom colors (for transaction types)
<Badge className="bg-tx-swap-bg text-tx-swap border-0">Swap</Badge>
<Badge className="bg-profit-bg text-profit border-0">+5.2%</Badge>
```

### DropdownMenu

Context menu for actions.

```tsx
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'

<DropdownMenu>
  <DropdownMenuTrigger asChild>
    <Button variant="ghost" size="icon">
      <MoreHorizontal className="h-4 w-4" />
    </Button>
  </DropdownMenuTrigger>
  <DropdownMenuContent align="end">
    <DropdownMenuLabel>Actions</DropdownMenuLabel>
    <DropdownMenuSeparator />
    <DropdownMenuItem onClick={handleEdit}>
      <Edit className="mr-2 h-4 w-4" />
      Edit
    </DropdownMenuItem>
    <DropdownMenuItem onClick={handleDelete} className="text-destructive">
      <Trash className="mr-2 h-4 w-4" />
      Delete
    </DropdownMenuItem>
  </DropdownMenuContent>
</DropdownMenu>
```

### Skeleton

Loading placeholder.

```tsx
import { Skeleton } from '@/components/ui/skeleton'

// Text skeleton
<Skeleton className="h-4 w-48" />

// Card skeleton
<Skeleton className="h-32 w-full" />

// Avatar skeleton
<Skeleton className="h-10 w-10 rounded-full" />

// Full card loading state
function CardSkeleton() {
  return (
    <Card>
      <CardHeader>
        <Skeleton className="h-5 w-32" />
      </CardHeader>
      <CardContent>
        <Skeleton className="h-8 w-24 mb-2" />
        <Skeleton className="h-4 w-48" />
      </CardContent>
    </Card>
  )
}
```

### Tooltip

Hover hint.

```tsx
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from '@/components/ui/tooltip'

<TooltipProvider>
  <Tooltip>
    <TooltipTrigger asChild>
      <Button variant="ghost" size="icon">
        <Info className="h-4 w-4" />
      </Button>
    </TooltipTrigger>
    <TooltipContent>
      Helpful information here
    </TooltipContent>
  </Tooltip>
</TooltipProvider>

// With side positioning
<TooltipContent side="right">Content</TooltipContent>
```

### Separator

Visual divider.

```tsx
import { Separator } from '@/components/ui/separator'

<Separator />  {/* Horizontal */}
<Separator orientation="vertical" className="h-6" />  {/* Vertical */}
```

### ScrollArea

Scrollable container with custom scrollbar.

```tsx
import { ScrollArea } from '@/components/ui/scroll-area'

<ScrollArea className="h-72">
  {/* Long content here */}
</ScrollArea>
```

---

## Domain Components (components/domain/)

Business-aware components that understand MoonTrack types.

### TransactionTypeBadge

Colored badge for transaction types.

```tsx
import { TransactionTypeBadge } from '@/components/domain'

<TransactionTypeBadge type="manual_income" />
<TransactionTypeBadge type="manual_outcome" />
<TransactionTypeBadge type="asset_adjustment" />
<TransactionTypeBadge type="swap" />
<TransactionTypeBadge type="transfer" />
```

**Supported types:**
- `manual_income` - Green "Income" badge
- `manual_outcome` - Red "Outcome" badge
- `asset_adjustment` - Orange "Adjustment" badge
- `swap` - Green swap badge
- `transfer` - Gray transfer badge
- `bridge` - Cyan bridge badge

### PnLValue

Profit/loss value with automatic coloring.

```tsx
import { PnLValue } from '@/components/domain'

<PnLValue value={1234.56} />    {/* Green +$1,234.56 */}
<PnLValue value={-567.89} />   {/* Red -$567.89 */}
<PnLValue value={0} />         {/* Neutral $0.00 */}

// With percentage
<PnLValue value={5.2} isPercentage />  {/* +5.2% */}

// Custom formatting
<PnLValue value={value} showSign={false} />
```

### StatCard

Dashboard statistic card.

```tsx
import { StatCard } from '@/components/domain'

<StatCard
  title="Total Value"
  value={formatUSD(totalValue)}
  change={percentChange}
  icon={<DollarSign className="h-4 w-4" />}
/>

<StatCard
  title="24h Change"
  value={formatUSD(change24h)}
  trend={change24h > 0 ? 'up' : 'down'}
/>
```

### WalletCard

Wallet display card with chain indicator.

```tsx
import { WalletCard, WalletCardCompact } from '@/components/domain'

// Full card (for wallet list)
<WalletCard
  wallet={wallet}
  totalValue={12345.67}
  assetCount={5}
/>

// Compact card (for dashboard)
<WalletCardCompact
  wallet={wallet}
  totalValue={12345.67}
/>
```

### AssetIcon

Asset icon with fallback.

```tsx
import { AssetIcon } from '@/components/domain'

<AssetIcon
  symbol="BTC"
  iconUrl={asset.icon_url}
  size="sm"  // sm | md | lg
/>

// Sizes: sm=24px, md=32px, lg=40px
```

### AddressDisplay

Truncated blockchain address with copy button.

```tsx
import { AddressDisplay } from '@/components/domain'

<AddressDisplay
  address="0x1234567890abcdef1234567890abcdef12345678"
  truncate
/>

// Full address
<AddressDisplay address={address} />

// With custom styling
<AddressDisplay
  address={address}
  className="text-sm text-muted-foreground"
/>
```

---

## Layout Components (components/layout/)

Application structure components.

### Layout

Main layout wrapper with sidebar.

```tsx
import { Layout } from '@/components/layout/Layout'

<Layout>
  <PageContent />
</Layout>
```

### Sidebar

Desktop navigation sidebar (collapsible).

```tsx
import { Sidebar } from '@/components/layout/Sidebar'

// Used internally by Layout
// Collapsible via useSidebar hook
```

### MobileSidebar

Mobile navigation in sheet.

```tsx
import { MobileSidebar } from '@/components/layout/MobileSidebar'

// Used internally by Layout
// Opens as side sheet on mobile
```

### Header

Top header with mobile menu and theme toggle.

```tsx
import { Header } from '@/components/layout/Header'

// Used internally by Layout
// Contains mobile menu trigger and ThemeToggle
```

### ThemeToggle

Dark/light mode toggle button.

```tsx
import { ThemeToggle } from '@/components/layout/ThemeToggle'

<ThemeToggle />
<ThemeToggle className="absolute top-4 right-4" />
```

---

## Common Patterns

### Page with Loading State

```tsx
function MyPage() {
  const { data, isLoading, error } = useMyData()

  if (isLoading) {
    return <PageSkeleton />
  }

  if (error) {
    return (
      <div className="text-center py-12">
        <p className="text-destructive">Failed to load data</p>
        <Button onClick={refetch} className="mt-4">Retry</Button>
      </div>
    )
  }

  return (
    <div className="space-y-6">
      {/* Page content */}
    </div>
  )
}
```

### Empty State

```tsx
function EmptyState({ title, description, action }) {
  return (
    <div className="text-center py-12">
      <p className="text-lg font-medium">{title}</p>
      <p className="text-muted-foreground mt-1">{description}</p>
      {action && <div className="mt-4">{action}</div>}
    </div>
  )
}

// Usage
<EmptyState
  title="No wallets yet"
  description="Create your first wallet to start tracking"
  action={<CreateWalletDialog />}
/>
```

### List with Actions

```tsx
function ItemList({ items }) {
  return (
    <div className="space-y-2">
      {items.map((item) => (
        <div
          key={item.id}
          className="flex items-center justify-between p-3 rounded-lg border hover:border-border-hover"
        >
          <div className="flex items-center gap-3">
            <ItemIcon item={item} />
            <div>
              <p className="font-medium">{item.name}</p>
              <p className="text-sm text-muted-foreground">{item.description}</p>
            </div>
          </div>
          <DropdownMenu>
            {/* Actions */}
          </DropdownMenu>
        </div>
      ))}
    </div>
  )
}
```

### Form in Card

```tsx
function FormCard({ title, description, children, onSubmit }) {
  return (
    <Card>
      <CardHeader>
        <CardTitle className="text-base">{title}</CardTitle>
        {description && <CardDescription>{description}</CardDescription>}
      </CardHeader>
      <CardContent>
        <form onSubmit={onSubmit} className="space-y-4">
          {children}
        </form>
      </CardContent>
    </Card>
  )
}
```
