import { cn } from '@/lib/utils'
import { ChainIcon } from './ChainIcon'

interface AssetIconProps {
  symbol: string
  imageUrl?: string
  chainId?: string
  size?: 'sm' | 'default' | 'lg'
  className?: string
}

const sizeClasses = {
  sm: 'h-6 w-6 text-[10px]',
  default: 'h-8 w-8 text-xs',
  lg: 'h-10 w-10 text-sm',
}

const chainOverlaySize = {
  sm: 'xs' as const,
  default: 'xs' as const,
  lg: 'sm' as const,
}

export function AssetIcon({
  symbol,
  imageUrl,
  chainId,
  size = 'default',
  className,
}: AssetIconProps) {
  const initials = symbol.slice(0, 3).toUpperCase()

  const iconElement = imageUrl ? (
    <img
      src={imageUrl}
      alt={symbol}
      className={cn('rounded-full object-cover', sizeClasses[size])}
      onError={(e) => {
        const target = e.target as HTMLImageElement
        target.style.display = 'none'
        const fallback = target.nextElementSibling as HTMLElement
        if (fallback) fallback.style.display = 'flex'
      }}
    />
  ) : (
    <div
      className={cn(
        'flex items-center justify-center rounded-full bg-primary/10 text-primary font-mono font-medium',
        sizeClasses[size]
      )}
    >
      {initials}
    </div>
  )

  if (!chainId) {
    return <div className={className}>{iconElement}</div>
  }

  return (
    <div className={cn('relative inline-flex', className)}>
      {iconElement}
      <div className="absolute -bottom-0.5 -right-0.5 ring-2 ring-background rounded-full">
        <ChainIcon chainId={chainId} size={chainOverlaySize[size]} showTooltip />
      </div>
    </div>
  )
}
