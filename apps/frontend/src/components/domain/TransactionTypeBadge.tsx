import { ArrowDownLeft, ArrowUpRight, RefreshCw } from 'lucide-react'
import { Badge } from '@/components/ui/badge'
import { cn } from '@/lib/utils'
import type { TransactionType } from '@/types/transaction'

interface TransactionTypeBadgeProps {
  type: TransactionType
  size?: 'default' | 'sm' | 'lg'
  showLabel?: boolean
  className?: string
}

const typeConfig: Record<
  TransactionType,
  {
    label: string
    icon: React.ElementType
    variant: 'profit' | 'loss' | 'transfer'
  }
> = {
  manual_income: {
    label: 'Income',
    icon: ArrowDownLeft,
    variant: 'profit',
  },
  manual_outcome: {
    label: 'Outcome',
    icon: ArrowUpRight,
    variant: 'loss',
  },
  asset_adjustment: {
    label: 'Adjustment',
    icon: RefreshCw,
    variant: 'transfer',
  },
}

const sizeConfig = {
  sm: {
    iconSize: 'h-3 w-3',
    text: 'text-xs',
  },
  default: {
    iconSize: 'h-3.5 w-3.5',
    text: 'text-xs',
  },
  lg: {
    iconSize: 'h-4 w-4',
    text: 'text-sm',
  },
}

export function TransactionTypeBadge({
  type,
  size = 'default',
  showLabel = true,
  className,
}: TransactionTypeBadgeProps) {
  const config = typeConfig[type]
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
