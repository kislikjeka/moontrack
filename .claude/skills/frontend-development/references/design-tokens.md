# MoonTrack Design Tokens

Complete reference for CSS variables and color system used in MoonTrack frontend.

## Color System

MoonTrack uses HSL-based CSS variables for theming. All colors defined in `src/styles/globals.css`.

### Core Colors

#### Background Colors

| Token | CSS Variable | Light Value | Dark Value | Usage |
|-------|--------------|-------------|------------|-------|
| `background` | `--background` | white | slate-950 | Page background |
| `background-subtle` | `--background-subtle` | slate-50 | slate-900 | Subtle sections |
| `background-muted` | `--background-muted` | slate-100 | slate-800 | Muted areas, hovers |

```tsx
// Usage
<div className="bg-background">Main content</div>
<div className="bg-background-subtle">Subtle section</div>
<div className="hover:bg-background-muted">Hoverable item</div>
```

#### Foreground Colors

| Token | CSS Variable | Light Value | Dark Value | Usage |
|-------|--------------|-------------|------------|-------|
| `foreground` | `--foreground` | slate-900 | slate-50 | Primary text |
| `foreground-muted` | `--foreground-muted` | slate-500 | slate-400 | Secondary text |
| `muted-foreground` | `--muted-foreground` | slate-500 | slate-400 | Alias for above |

```tsx
// Usage
<p className="text-foreground">Primary text</p>
<p className="text-muted-foreground">Secondary text</p>
```

#### Border Colors

| Token | CSS Variable | Light Value | Dark Value | Usage |
|-------|--------------|-------------|------------|-------|
| `border` | `--border` | slate-200 | slate-700 | Default borders |
| `border-hover` | `--border-hover` | slate-300 | slate-600 | Hover state borders |

```tsx
// Usage
<div className="border border-border">Default border</div>
<div className="border border-border hover:border-border-hover">Hoverable border</div>
```

### Brand Colors

#### Primary (Cyan/Teal)

| Token | CSS Variable | Light Value | Dark Value | Usage |
|-------|--------------|-------------|------------|-------|
| `primary` | `--primary` | `174 72% 40%` | `174 72% 56%` | Primary actions |
| `primary-foreground` | `--primary-foreground` | white | slate-950 | Text on primary |
| `primary-muted` | `--primary-muted` | cyan-50 | cyan-900 | Primary background |

```tsx
// Usage
<Button>Primary action</Button>  {/* Uses bg-primary */}
<div className="bg-primary/10 text-primary">Highlighted section</div>
```

### Semantic Colors

#### Profit/Loss

| Token | CSS Variable | Light Value | Dark Value | Usage |
|-------|--------------|-------------|------------|-------|
| `profit` | `--profit` | green-600 | green-400 | Positive values |
| `profit-bg` | `--profit-bg` | green-50 | green-900/20 | Profit background |
| `loss` | `--loss` | red-500 | red-400 | Negative values |
| `loss-bg` | `--loss-bg` | red-50 | red-900/20 | Loss background |

```tsx
// Usage with PnLValue component
<PnLValue value={1234.56} />  {/* Auto-colors based on positive/negative */}

// Manual usage
<span className="text-profit">+$1,234.56</span>
<span className="text-loss">-$567.89</span>
<div className="bg-profit-bg text-profit">Profit highlight</div>
```

#### Transaction Types

| Token | CSS Variable | Light Value | Dark Value | Usage |
|-------|--------------|-------------|------------|-------|
| `tx-swap` | `--tx-swap` | green-600 | green-400 | Swap transactions |
| `tx-swap-bg` | `--tx-swap-bg` | green-50 | green-900/20 | Swap badge bg |
| `tx-transfer` | `--tx-transfer` | slate-500 | slate-400 | Transfer transactions |
| `tx-transfer-bg` | `--tx-transfer-bg` | slate-100 | slate-800 | Transfer badge bg |
| `tx-bridge` | `--tx-bridge` | cyan-600 | cyan-400 | Bridge transactions |
| `tx-bridge-bg` | `--tx-bridge-bg` | cyan-50 | cyan-900/20 | Bridge badge bg |
| `tx-liquidity` | `--tx-liquidity` | purple-500 | purple-400 | Liquidity transactions |
| `tx-liquidity-bg` | `--tx-liquidity-bg` | purple-50 | purple-900/20 | Liquidity badge bg |
| `tx-gm-pool` | `--tx-gm-pool` | orange-500 | orange-400 | GM Pool transactions |
| `tx-gm-pool-bg` | `--tx-gm-pool-bg` | orange-50 | orange-900/20 | GM Pool badge bg |

```tsx
// Usage with TransactionTypeBadge
<TransactionTypeBadge type="swap" />

// Manual badge styling
<Badge className="bg-tx-swap-bg text-tx-swap border-0">Swap</Badge>
<Badge className="bg-tx-transfer-bg text-tx-transfer border-0">Transfer</Badge>
```

### shadcn/ui Standard Colors

| Token | Usage |
|-------|-------|
| `card` / `card-foreground` | Card components |
| `popover` / `popover-foreground` | Popovers, dropdowns |
| `secondary` / `secondary-foreground` | Secondary buttons |
| `muted` / `muted-foreground` | Muted elements |
| `accent` / `accent-foreground` | Accented elements |
| `destructive` / `destructive-foreground` | Destructive actions |
| `input` | Input borders |
| `ring` | Focus rings |

## Typography

### Font Families

```tsx
// Sans-serif (default)
<p className="font-sans">Inter, system-ui, sans-serif</p>

// Monospace (for numbers, addresses, code)
<span className="font-mono">JetBrains Mono, Fira Code, monospace</span>
```

### Font Sizes

| Class | Size | Usage |
|-------|------|-------|
| `text-xs` | 12px | Badges, captions |
| `text-sm` | 14px | Secondary text, descriptions |
| `text-base` | 16px | Body text |
| `text-lg` | 18px | Emphasized text |
| `text-xl` | 20px | Section headings |
| `text-2xl` | 24px | Page titles |
| `text-3xl` | 30px | Hero numbers |

### Font Weights

| Class | Weight | Usage |
|-------|--------|-------|
| `font-normal` | 400 | Body text |
| `font-medium` | 500 | Labels, emphasized text |
| `font-semibold` | 600 | Card titles, stats |
| `font-bold` | 700 | Page titles |

### Text Styles

```tsx
// Page title
<h1 className="text-2xl font-bold tracking-tight">Page Title</h1>

// Page subtitle
<p className="text-muted-foreground">Description text here</p>

// Card title
<CardTitle className="text-base">Card Title</CardTitle>

// Stat value
<p className="text-2xl font-semibold">{formatUSD(value)}</p>

// Address/hash display
<span className="font-mono text-sm">0x1234...5678</span>
```

## Spacing Scale

Tailwind's default spacing scale with common patterns:

| Class | Value | Usage |
|-------|-------|-------|
| `gap-1` / `space-y-1` | 4px | Tight spacing |
| `gap-2` / `space-y-2` | 8px | Form field labels |
| `gap-3` / `space-y-3` | 12px | Inline items |
| `gap-4` / `space-y-4` | 16px | Form fields, card padding |
| `gap-6` / `space-y-6` | 24px | Page sections |
| `gap-8` / `space-y-8` | 32px | Major sections |

### Common Patterns

```tsx
// Page container
<div className="space-y-6">
  <header>...</header>
  <section>...</section>
</div>

// Card content
<CardContent className="p-4">...</CardContent>

// Form
<form className="space-y-4">
  <div className="space-y-2">
    <Label>Field</Label>
    <Input />
  </div>
</form>

// Inline items
<div className="flex items-center gap-3">
  <Icon />
  <span>Label</span>
</div>

// Grid
<div className="grid gap-4 md:grid-cols-2 lg:grid-cols-3">
  <Card>...</Card>
</div>
```

## Border Radius

| Token | Value | Usage |
|-------|-------|-------|
| `rounded-sm` | 4px | Small elements |
| `rounded-md` | 6px | Inputs, small buttons |
| `rounded-lg` | 8px | Cards, dialogs (default) |
| `rounded-full` | 9999px | Avatars, pills |

```tsx
// Default card radius
<Card className="rounded-lg">...</Card>

// Input radius
<Input className="rounded-md" />

// Avatar
<div className="rounded-full h-10 w-10">...</div>
```

## Shadows

Use sparingly - prefer borders for separation:

```tsx
// Elevated card
<Card className="shadow-sm">...</Card>

// Dropdown/popover
<div className="shadow-md">...</div>

// Modal
<DialogContent className="shadow-lg">...</DialogContent>
```

## Dark Mode

Theme is managed by ThemeProvider. Toggle between light/dark:

```tsx
import { useTheme } from '@/app/providers'

function Component() {
  const { resolvedTheme, setTheme } = useTheme()

  return (
    <button onClick={() => setTheme(resolvedTheme === 'dark' ? 'light' : 'dark')}>
      Toggle theme
    </button>
  )
}
```

### Theme-Aware Styling

All CSS variables automatically switch. For manual overrides:

```tsx
// Different colors per theme
<div className="bg-white dark:bg-slate-900">...</div>

// Different opacity per theme
<div className="bg-primary/10 dark:bg-primary/20">...</div>
```

## Chart Colors

For Recharts and data visualization:

| Token | Usage |
|-------|-------|
| `--chart-1` | Primary data series (cyan) |
| `--chart-2` | Secondary series (green) |
| `--chart-3` | Tertiary series (purple) |
| `--chart-4` | Fourth series (orange) |
| `--chart-5` | Fifth series (red) |

```tsx
// In chart config
const chartConfig = {
  btc: { color: 'hsl(var(--chart-1))' },
  eth: { color: 'hsl(var(--chart-2))' },
  sol: { color: 'hsl(var(--chart-3))' },
}
```

## CSS Variables Reference

Complete list in `src/styles/globals.css`:

```css
:root {
  /* Background */
  --background: 0 0% 100%;
  --background-subtle: 210 20% 98%;
  --background-muted: 210 20% 96%;

  /* Foreground */
  --foreground: 220 20% 10%;
  --foreground-muted: 215 16% 47%;

  /* Border */
  --border: 214 20% 88%;
  --border-hover: 214 20% 78%;

  /* Primary */
  --primary: 174 72% 40%;
  --primary-foreground: 0 0% 100%;
  --primary-muted: 174 50% 92%;

  /* Semantic */
  --profit: 142 76% 36%;
  --profit-bg: 142 76% 95%;
  --loss: 0 84% 50%;
  --loss-bg: 0 84% 96%;

  /* Transaction types */
  --tx-swap: 142 76% 36%;
  --tx-swap-bg: 142 76% 95%;
  --tx-transfer: 215 16% 47%;
  --tx-transfer-bg: 215 16% 95%;
  --tx-bridge: 174 72% 40%;
  --tx-bridge-bg: 174 50% 92%;
  --tx-liquidity: 270 60% 55%;
  --tx-liquidity-bg: 270 60% 95%;
  --tx-gm-pool: 38 92% 45%;
  --tx-gm-pool-bg: 38 92% 95%;

  /* shadcn/ui */
  --card: 0 0% 100%;
  --card-foreground: 220 20% 10%;
  --popover: 0 0% 100%;
  --popover-foreground: 220 20% 10%;
  --secondary: 210 20% 96%;
  --secondary-foreground: 220 20% 10%;
  --muted: 210 20% 96%;
  --muted-foreground: 215 16% 47%;
  --accent: 210 20% 96%;
  --accent-foreground: 220 20% 10%;
  --destructive: 0 84% 50%;
  --destructive-foreground: 0 0% 100%;
  --input: 214 20% 88%;
  --ring: 174 72% 40%;
  --radius: 0.5rem;

  /* Charts */
  --chart-1: 174 72% 40%;
  --chart-2: 142 76% 36%;
  --chart-3: 270 60% 55%;
  --chart-4: 38 92% 45%;
  --chart-5: 0 84% 50%;
}
```
