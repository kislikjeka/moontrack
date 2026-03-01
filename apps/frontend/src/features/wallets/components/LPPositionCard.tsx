import { useState } from 'react'
import { ChevronDown } from 'lucide-react'
import { Card, CardContent } from '@/components/ui/card'
import { Badge } from '@/components/ui/badge'
import { AssetIcon } from '@/components/domain/AssetIcon'
import { ChainIcon } from '@/components/domain/ChainIcon'
import { PnLValue } from '@/components/domain/PnLValue'
import { formatUSD, formatDate, formatTokenAmount } from '@/lib/format'
import { cn } from '@/lib/utils'
import type { LPPosition } from '@/types/lpPosition'

interface LPPositionCardProps {
  position: LPPosition
}

export function LPPositionCard({ position }: LPPositionCardProps) {
  const [isExpanded, setIsExpanded] = useState(false)

  const depositedUSD = parseFloat(position.total_deposited_usd) / 1e8
  const withdrawnUSD = parseFloat(position.total_withdrawn_usd) / 1e8
  const feesUSD = parseFloat(position.total_claimed_fees_usd) / 1e8
  const aprPercent = position.apr_bps != null ? position.apr_bps / 100 : null

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
            <div className="flex -space-x-2">
              <AssetIcon symbol={position.token0_symbol} size="sm" />
              <AssetIcon symbol={position.token1_symbol} size="sm" />
            </div>
            <div>
              <div className="flex items-center gap-2">
                <span className="font-medium">
                  {position.token0_symbol} / {position.token1_symbol}
                </span>
                <Badge variant={position.status === 'open' ? 'profit' : 'secondary'}>
                  {position.status === 'open' ? 'Open' : 'Closed'}
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

        {/* Metrics row */}
        <div className="mt-3 grid grid-cols-3 gap-4">
          <div>
            <p className="text-xs text-muted-foreground">Deposited</p>
            <p className="text-sm font-medium font-mono">{formatUSD(depositedUSD)}</p>
          </div>
          <div>
            <p className="text-xs text-muted-foreground">Fees Earned</p>
            <p className="text-sm font-medium font-mono">{formatUSD(feesUSD)}</p>
          </div>
          <div>
            <p className="text-xs text-muted-foreground">APR</p>
            <p className="text-sm font-medium font-mono">
              {aprPercent != null ? `${aprPercent.toFixed(2)}%` : '—'}
            </p>
          </div>
        </div>

        {/* Expanded: additional details */}
        {isExpanded && (
          <div className="mt-4 border-t pt-4 space-y-4">
            {/* USD breakdown */}
            <div className="grid grid-cols-3 gap-4">
              <div>
                <p className="text-xs text-muted-foreground">Deposited</p>
                <p className="text-sm font-mono">{formatUSD(depositedUSD)}</p>
              </div>
              <div>
                <p className="text-xs text-muted-foreground">Withdrawn</p>
                <p className="text-sm font-mono">{formatUSD(withdrawnUSD)}</p>
              </div>
              <div>
                <p className="text-xs text-muted-foreground">Fees Claimed</p>
                <p className="text-sm font-mono">{formatUSD(feesUSD)}</p>
              </div>
            </div>

            {/* Token breakdown */}
            <div>
              <p className="text-xs text-muted-foreground mb-2">Remaining Tokens</p>
              <div className="space-y-1">
                <div className="flex items-center justify-between text-sm">
                  <div className="flex items-center gap-2">
                    <AssetIcon symbol={position.token0_symbol} size="sm" />
                    <span>{position.token0_symbol}</span>
                  </div>
                  <span className="font-mono">
                    {formatTokenAmount(position.remaining_token0, position.token0_decimals)}
                  </span>
                </div>
                <div className="flex items-center justify-between text-sm">
                  <div className="flex items-center gap-2">
                    <AssetIcon symbol={position.token1_symbol} size="sm" />
                    <span>{position.token1_symbol}</span>
                  </div>
                  <span className="font-mono">
                    {formatTokenAmount(position.remaining_token1, position.token1_decimals)}
                  </span>
                </div>
              </div>
            </div>

            {/* Metadata */}
            <div className="flex flex-wrap gap-x-6 gap-y-1 text-sm text-muted-foreground">
              <span>Opened: {formatDate(position.opened_at)}</span>
              {position.closed_at && (
                <span>Closed: {formatDate(position.closed_at)}</span>
              )}
              {position.nft_token_id && (
                <span>NFT ID: #{position.nft_token_id}</span>
              )}
            </div>

            {/* PnL for closed positions */}
            {position.status === 'closed' && position.realized_pnl_usd != null && (
              <div className="flex items-center gap-2">
                <span className="text-sm text-muted-foreground">Realized PnL:</span>
                <PnLValue
                  value={parseFloat(position.realized_pnl_usd) / 1e8}
                  showIcon
                  size="sm"
                />
              </div>
            )}
          </div>
        )}
      </CardContent>
    </Card>
  )
}
