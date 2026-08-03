import { TrendingUp, TrendingDown, Minus } from 'lucide-react'
import { cn } from '@/lib/utils'
import { EM_DASH, formatUSD, formatPercent } from '@/lib/format'

interface PnLValueProps {
  value: number | string | null | undefined
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
  /* An unknown PnL is neither a profit nor a loss nor a break-even: it gets no
     sign, no colour and no trend icon, only a dash (#79). Treating it as zero
     would paint "we could not compute this" as "you came out exactly even". */
  const parsed = typeof value === 'string' ? parseFloat(value) : value
  const numValue = parsed == null || isNaN(parsed) ? null : parsed
  const isKnown = numValue !== null
  const isPositive = isKnown && numValue > 0
  const isNegative = isKnown && numValue < 0
  const isZero = isKnown && numValue === 0

  const formattedValue = !isKnown
    ? EM_DASH
    : isPercent
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
