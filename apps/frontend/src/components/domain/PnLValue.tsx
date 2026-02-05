import { TrendingUp, TrendingDown, Minus } from 'lucide-react'
import { cn } from '@/lib/utils'
import { formatUSD, formatPercent } from '@/lib/format'

interface PnLValueProps {
  value: number | string
  isPercent?: boolean
  showIcon?: boolean
  showSign?: boolean
  className?: string
  size?: 'sm' | 'default' | 'lg'
}

export function PnLValue({
  value,
  isPercent = false,
  showIcon = false,
  showSign = true,
  className,
  size = 'default',
}: PnLValueProps) {
  const numValue = typeof value === 'string' ? parseFloat(value) : value
  const isPositive = numValue > 0
  const isNegative = numValue < 0
  const isZero = numValue === 0

  const formattedValue = isPercent
    ? formatPercent(numValue)
    : showSign && isPositive
      ? `+${formatUSD(numValue)}`
      : formatUSD(numValue)

  const sizeClasses = {
    sm: 'text-sm',
    default: 'text-base',
    lg: 'text-lg font-medium',
  }

  const iconSizes = {
    sm: 'h-3 w-3',
    default: 'h-4 w-4',
    lg: 'h-5 w-5',
  }

  return (
    <span
      className={cn(
        'inline-flex items-center gap-1',
        sizeClasses[size],
        isPositive && 'text-profit',
        isNegative && 'text-loss',
        isZero && 'text-muted-foreground',
        className
      )}
    >
      {showIcon && (
        <>
          {isPositive && <TrendingUp className={iconSizes[size]} />}
          {isNegative && <TrendingDown className={iconSizes[size]} />}
          {isZero && <Minus className={iconSizes[size]} />}
        </>
      )}
      <span>{formattedValue}</span>
    </span>
  )
}
