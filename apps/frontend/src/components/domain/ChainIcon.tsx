import type { ReactNode } from 'react'
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from '@/components/ui/tooltip'
import { CHAIN_CONFIG, getChainName } from '@/types/wallet'
import { cn } from '@/lib/utils'

interface ChainIconProps {
  chainId: string
  size?: 'xs' | 'sm' | 'default'
  showTooltip?: boolean
  className?: string
}

const sizeMap = {
  xs: 16,
  sm: 20,
  default: 24,
}

const CHAIN_SVG_PATHS: Record<string, ReactNode> = {
  ethereum: (
    <path
      d="M8 1L3 8.5L8 11.5L13 8.5L8 1ZM3 9.5L8 15L13 9.5L8 12.5L3 9.5Z"
      fill="currentColor"
    />
  ),
  polygon: (
    <path
      d="M11.2 6.1C10.9 5.9 10.5 5.9 10.2 6.1L8.5 7.1L7.3 7.8L5.7 8.8C5.4 9 5 9 4.7 8.8L3.4 8C3.1 7.8 2.9 7.5 2.9 7.1V5.6C2.9 5.2 3.1 4.9 3.4 4.7L4.7 3.9C5 3.7 5.4 3.7 5.7 3.9L7 4.7C7.3 4.9 7.5 5.2 7.5 5.6V6.6L8.7 5.8V4.8C8.7 4.4 8.5 4.1 8.2 3.9L5.8 2.5C5.5 2.3 5.1 2.3 4.8 2.5L2.3 3.9C2 4.1 1.8 4.4 1.8 4.8V7.7C1.8 8.1 2 8.4 2.3 8.6L4.7 10C5 10.2 5.4 10.2 5.7 10L7.3 9L8.5 8.3L10.1 7.3C10.4 7.1 10.8 7.1 11.1 7.3L12.4 8.1C12.7 8.3 12.9 8.6 12.9 9V10.5C12.9 10.9 12.7 11.2 12.4 11.4L11.2 12.2C10.9 12.4 10.5 12.4 10.2 12.2L8.9 11.4C8.6 11.2 8.4 10.9 8.4 10.5V9.5L7.2 10.3V11.3C7.2 11.7 7.4 12 7.7 12.2L10.1 13.6C10.4 13.8 10.8 13.8 11.1 13.6L13.5 12.2C13.8 12 14 11.7 14 11.3V8.4C14 8 13.8 7.7 13.5 7.5L11.2 6.1Z"
      fill="currentColor"
    />
  ),
  arbitrum: (
    <path
      d="M9.4 5.8L11.4 9.3L12.5 7.5L10 3.2H8.9L6.3 7.7L8 10.7L9.4 8.3L8.1 6L9.4 5.8ZM5.5 9L3.5 5.5L2.4 7.3L5 11.8H6.2L7.3 10L5.5 6.8V9ZM8 1L2 4V12L8 15L14 12V4L8 1Z"
      fill="currentColor"
    />
  ),
  optimism: (
    <path
      d="M5.6 10.5C5 10.5 4.5 10.3 4.1 9.9C3.8 9.5 3.6 9 3.6 8.3C3.6 7.5 3.8 6.8 4.2 6.2C4.6 5.6 5.2 5.3 5.9 5.3C6.3 5.3 6.6 5.4 6.9 5.5C7.1 5.7 7.3 5.9 7.4 6.2L7.4 5.4H8.8V10.4H7.4V9.6C7.3 9.9 7 10.1 6.8 10.3C6.4 10.4 6 10.5 5.6 10.5ZM6.1 9.2C6.5 9.2 6.8 9.1 7.1 8.7C7.3 8.4 7.4 8 7.4 7.4C7.4 6.6 7.1 6.2 6.4 6.2C6 6.2 5.7 6.4 5.5 6.7C5.3 7.1 5.1 7.5 5.1 8.1C5.1 8.8 5.4 9.2 6.1 9.2ZM10 11.8V5.4H11.4V6.1C11.5 5.8 11.7 5.6 12 5.4C12.3 5.3 12.6 5.2 13 5.2C13.7 5.2 14.2 5.5 14.5 5.9C14.8 6.4 14.9 6.9 14.9 7.6C14.9 8.4 14.7 9.1 14.3 9.7C13.9 10.2 13.3 10.5 12.7 10.5C12.4 10.5 12.1 10.4 11.9 10.3C11.7 10.2 11.5 10 11.4 9.7V11.8H10ZM12.3 9.2C12.7 9.2 13 9 13.2 8.7C13.4 8.3 13.5 7.9 13.5 7.3C13.5 6.6 13.2 6.2 12.6 6.2C12.2 6.2 11.9 6.4 11.7 6.7C11.5 7 11.4 7.5 11.4 8C11.4 8.8 11.7 9.2 12.3 9.2Z"
      fill="currentColor"
    />
  ),
  base: (
    <path
      d="M8 14C11.3137 14 14 11.3137 14 8C14 4.68629 11.3137 2 8 2C4.82057 2 2.22053 4.47006 2.01498 7.6H10.27V8.4H2.01498C2.22053 11.5299 4.82057 14 8 14Z"
      fill="currentColor"
    />
  ),
  avalanche: (
    <path
      d="M10.6 10.8H12.8L8.5 3.2C8.3 2.9 7.7 2.9 7.5 3.2L6.2 5.5L7.3 7.4L10.6 10.8ZM5.5 6.8L3.2 10.8H7.8L5.5 6.8Z"
      fill="currentColor"
    />
  ),
  'binance-smart-chain': (
    <path
      d="M8 3L5.5 5.5L6.5 6.5L8 5L9.5 6.5L10.5 5.5L8 3ZM3 8L4 7L5 8L4 9L3 8ZM5.5 10.5L8 13L10.5 10.5L9.5 9.5L8 11L6.5 9.5L5.5 10.5ZM11 8L12 7L13 8L12 9L11 8ZM9.2 8L8 6.8L6.8 8L8 9.2L9.2 8Z"
      fill="currentColor"
    />
  ),
}

export function ChainIcon({
  chainId,
  size = 'default',
  showTooltip = false,
  className,
}: ChainIconProps) {
  const px = sizeMap[size]
  const config = CHAIN_CONFIG[chainId]
  const color = config?.color ?? '#6B7280'
  const svgPath = CHAIN_SVG_PATHS[chainId]

  const icon = svgPath ? (
    <div
      className={cn('flex items-center justify-center rounded-full flex-shrink-0', className)}
      style={{
        width: px,
        height: px,
        backgroundColor: color,
        color: '#fff',
      }}
    >
      <svg
        width={px * 0.65}
        height={px * 0.65}
        viewBox="0 0 16 16"
        fill="none"
      >
        {svgPath}
      </svg>
    </div>
  ) : (
    <div
      className={cn(
        'flex items-center justify-center rounded-full font-mono font-bold flex-shrink-0',
        className
      )}
      style={{
        width: px,
        height: px,
        backgroundColor: color,
        color: '#fff',
        fontSize: px * 0.45,
      }}
    >
      {(config?.shortName ?? chainId)[0]}
    </div>
  )

  if (!showTooltip) return icon

  return (
    <TooltipProvider delayDuration={200}>
      <Tooltip>
        <TooltipTrigger asChild>{icon}</TooltipTrigger>
        <TooltipContent>
          <p>{getChainName(chainId)}</p>
        </TooltipContent>
      </Tooltip>
    </TooltipProvider>
  )
}

interface ChainIconRowProps {
  chains: string[]
  size?: 'xs' | 'sm'
  maxVisible?: number
  className?: string
}

export function ChainIconRow({
  chains,
  size = 'xs',
  maxVisible = 5,
  className,
}: ChainIconRowProps) {
  const visible = chains.slice(0, maxVisible)
  const remaining = chains.length - maxVisible

  return (
    <div className={cn('flex items-center gap-0.5', className)}>
      {visible.map((chain) => (
        <ChainIcon key={chain} chainId={chain} size={size} showTooltip />
      ))}
      {remaining > 0 && (
        <span className="text-xs text-muted-foreground ml-0.5">
          +{remaining}
        </span>
      )}
    </div>
  )
}
