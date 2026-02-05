import { cn } from '@/lib/utils'

interface AssetIconProps {
  symbol: string
  imageUrl?: string
  size?: 'sm' | 'default' | 'lg'
  className?: string
}

const sizeClasses = {
  sm: 'h-6 w-6 text-[10px]',
  default: 'h-8 w-8 text-xs',
  lg: 'h-10 w-10 text-sm',
}

export function AssetIcon({
  symbol,
  imageUrl,
  size = 'default',
  className,
}: AssetIconProps) {
  // Get initials from symbol (first 2-3 chars)
  const initials = symbol.slice(0, 3).toUpperCase()

  if (imageUrl) {
    return (
      <img
        src={imageUrl}
        alt={symbol}
        className={cn('rounded-full object-cover', sizeClasses[size], className)}
        onError={(e) => {
          // Fallback to initials on image error
          const target = e.target as HTMLImageElement
          target.style.display = 'none'
          const fallback = target.nextElementSibling as HTMLElement
          if (fallback) fallback.style.display = 'flex'
        }}
      />
    )
  }

  return (
    <div
      className={cn(
        'flex items-center justify-center rounded-full bg-primary/10 text-primary font-mono font-medium',
        sizeClasses[size],
        className
      )}
    >
      {initials}
    </div>
  )
}
