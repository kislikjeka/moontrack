import { ArrowDownLeft, ArrowUpRight, ArrowLeftRight, RefreshCw, ArrowDownToLine, ArrowUpFromLine, Coins } from 'lucide-react'
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
    variant: 'profit' | 'loss' | 'transfer' | 'liquidity'
  }
> = {
  transfer_in: {
    label: 'Transfer In',
    icon: ArrowDownLeft,
    variant: 'profit',
  },
  transfer_out: {
    label: 'Transfer Out',
    icon: ArrowUpRight,
    variant: 'loss',
  },
  internal_transfer: {
    label: 'Internal',
    icon: ArrowLeftRight,
    variant: 'transfer',
  },
  asset_adjustment: {
    label: 'Adjustment',
    icon: RefreshCw,
    variant: 'transfer',
  },
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
