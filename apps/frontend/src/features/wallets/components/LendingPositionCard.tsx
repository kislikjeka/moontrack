import { useState } from 'react'
import { ChevronDown } from 'lucide-react'
import { Card, CardContent } from '@/components/ui/card'
import { Badge } from '@/components/ui/badge'
import { AssetIcon } from '@/components/domain/AssetIcon'
import { ChainIcon } from '@/components/domain/ChainIcon'
import { formatDate, formatTokenAmount } from '@/lib/format'
import { cn } from '@/lib/utils'
import type { LendingPosition } from '@/types/lendingPosition'

interface LendingPositionCardProps {
  position: LendingPosition
}

export function LendingPositionCard({ position }: LendingPositionCardProps) {
  const [isExpanded, setIsExpanded] = useState(false)

  const hasBorrow = !!position.borrow_asset

  return (
    <Card
      className={cn(
        'cursor-pointer transition-colors hover:bg-accent/50',
        isExpanded && 'bg-accent/30'
      )}
      onClick={() => setIsExpanded(!isExpanded)}
    >
      <CardContent className="p-4">
        {/* Collapsed: always visible */}
        <div className="flex items-start justify-between">
          <div className="flex items-center gap-3">
            <AssetIcon symbol={position.supply_asset} size="sm" />
            <div>
              <div className="flex items-center gap-2">
                <span className="font-medium">{position.supply_asset}</span>
                {hasBorrow && (
                  <span className="text-muted-foreground">/ {position.borrow_asset}</span>
                )}
                <Badge variant={position.status === 'active' ? 'profit' : 'secondary'}>
                  {position.status === 'active' ? 'Active' : 'Closed'}
                </Badge>
                <ChainIcon chainId={position.chain_id} size="xs" showTooltip />
              </div>
              <p className="text-sm text-muted-foreground">{position.protocol}</p>
            </div>
          </div>
          <ChevronDown
            className={cn(
              'h-4 w-4 text-muted-foreground transition-transform',
              isExpanded && 'rotate-180'
            )}
          />
        </div>

        {/* Balances row */}
        <div className="mt-3 grid grid-cols-2 gap-4">
          <div>
            <p className="text-xs text-muted-foreground">Supplied</p>
            <p className="text-sm font-medium font-mono">
              {formatTokenAmount(position.supply_amount, position.supply_decimals)} {position.supply_asset}
            </p>
          </div>
          {hasBorrow && (
            <div>
              <p className="text-xs text-muted-foreground">Borrowed</p>
              <p className="text-sm font-medium font-mono">
                {formatTokenAmount(position.borrow_amount, position.borrow_decimals || 0)} {position.borrow_asset}
              </p>
            </div>
          )}
        </div>

        {/* Expanded: additional details */}
        {isExpanded && (
          <div className="mt-4 border-t pt-4">
            <div className="flex flex-wrap gap-x-6 gap-y-1 text-sm text-muted-foreground">
              <span>Opened: {formatDate(position.opened_at)}</span>
              {position.closed_at && (
                <span>Closed: {formatDate(position.closed_at)}</span>
              )}
            </div>
          </div>
        )}
      </CardContent>
    </Card>
  )
}
