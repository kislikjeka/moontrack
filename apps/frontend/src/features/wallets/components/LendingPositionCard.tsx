import { useState } from 'react'
import { ChevronDown } from 'lucide-react'
import { Card, CardContent } from '@/components/ui/card'
import { Badge } from '@/components/ui/badge'
import { AssetIcon } from '@/components/domain/AssetIcon'
import { ChainIcon } from '@/components/domain/ChainIcon'
import { formatDate, formatTokenAmount, formatUSD } from '@/lib/format'
import { cn } from '@/lib/utils'
import { parseDeFiAsset } from '@/lib/defi-asset'
import type {
  LendingPosition,
  LendingPositionAsset,
} from '@/types/lendingPosition'

interface LendingPositionCardProps {
  position: LendingPosition
}

function getDisplaySymbol(asset: LendingPositionAsset): string {
  const parsed = parseDeFiAsset(asset.asset)
  return parsed ? parsed.underlyingSymbol : asset.asset
}

export function LendingPositionCard({ position }: LendingPositionCardProps) {
  const [isExpanded, setIsExpanded] = useState(false)

  const supplyAssets = position.assets.filter((a) => a.side === 'supply')
  const borrowAssets = position.assets.filter((a) => a.side === 'borrow')
  const hasBorrow = borrowAssets.length > 0

  return (
    <Card
      className={cn(
        'cursor-pointer transition-colors hover:bg-accent/50',
        isExpanded && 'bg-accent/30'
      )}
      onClick={() => setIsExpanded(!isExpanded)}
    >
      <CardContent className="p-4">
        {/* Header row */}
        <div className="flex items-center justify-between">
          <div className="flex items-center gap-2">
            <span className="font-medium">{position.protocol}</span>
            <Badge
              variant={position.status === 'active' ? 'profit' : 'secondary'}
            >
              {position.status === 'active' ? 'Active' : 'Closed'}
            </Badge>
            <ChainIcon
              chainId={position.chain_id}
              size="xs"
              showTooltip
            />
          </div>
          <ChevronDown
            className={cn(
              'h-4 w-4 text-muted-foreground transition-transform',
              isExpanded && 'rotate-180'
            )}
          />
        </div>

        {/* Two-column grid */}
        <div
          className={cn(
            'mt-3 grid gap-3',
            hasBorrow ? 'grid-cols-2' : 'grid-cols-1'
          )}
        >
          {/* Supplied column */}
          <div className="rounded-md bg-tx-liquidity-bg/30 p-3">
            <div className="mb-2 flex items-center gap-1.5">
              <div className="h-4 w-0.5 rounded-full bg-tx-liquidity" />
              <span className="text-xs font-semibold uppercase tracking-wider text-tx-liquidity">
                Supplied
              </span>
            </div>
            <div className="space-y-1.5">
              {supplyAssets.map((asset, idx) => {
                const symbol = getDisplaySymbol(asset)
                return (
                  <div key={idx} className="flex items-center gap-2">
                    <AssetIcon symbol={symbol} size="sm" />
                    <span className="text-sm font-medium">{symbol}</span>
                    <span className="ml-auto text-sm font-mono text-muted-foreground">
                      {formatTokenAmount(asset.amount, asset.decimals)}
                    </span>
                  </div>
                )
              })}
              {supplyAssets.length === 0 && (
                <p className="text-xs text-muted-foreground">No supply assets</p>
              )}
            </div>
          </div>

          {/* Borrowed column */}
          {hasBorrow && (
            <div className="rounded-md bg-loss-bg/30 p-3">
              <div className="mb-2 flex items-center gap-1.5">
                <div className="h-4 w-0.5 rounded-full bg-loss" />
                <span className="text-xs font-semibold uppercase tracking-wider text-loss">
                  Borrowed
                </span>
              </div>
              <div className="space-y-1.5">
                {borrowAssets.map((asset, idx) => {
                  const symbol = getDisplaySymbol(asset)
                  return (
                    <div key={idx} className="flex items-center gap-2">
                      <AssetIcon symbol={symbol} size="sm" />
                      <span className="text-sm font-medium">{symbol}</span>
                      <span className="ml-auto text-sm font-mono text-muted-foreground">
                        {formatTokenAmount(asset.amount, asset.decimals)}
                      </span>
                    </div>
                  )
                })}
              </div>
            </div>
          )}
        </div>

        {/* Expanded section */}
        {isExpanded && (
          <div className="mt-4 border-t pt-4">
            <div className="flex flex-wrap gap-x-6 gap-y-1 text-sm text-muted-foreground">
              <span>Opened: {formatDate(position.opened_at)}</span>
              {position.closed_at && (
                <span>Closed: {formatDate(position.closed_at)}</span>
              )}
              {position.interest_earned_usd &&
                position.interest_earned_usd !== '0' && (
                  <span>
                    Interest earned:{' '}
                    <span className="font-medium text-profit">
                      {formatUSD(position.interest_earned_usd)}
                    </span>
                  </span>
                )}
            </div>
          </div>
        )}
      </CardContent>
    </Card>
  )
}
