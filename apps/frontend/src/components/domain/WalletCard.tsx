import { Link } from 'react-router-dom'
import { ExternalLink, Wallet } from 'lucide-react'
import { Card, CardContent } from '@/components/ui/card'
import { AddressDisplay } from './AddressDisplay'
import { SyncStatusBadge } from './SyncStatusBadge'
import { ChainIconRow } from './ChainIcon'
import { cn } from '@/lib/utils'
import { formatUSD, formatRelativeDate } from '@/lib/format'
import type { Wallet as WalletType } from '@/types/wallet'

interface WalletCardProps {
  wallet: WalletType
  totalValue?: string | number
  assetCount?: number
  className?: string
}

export function WalletCard({
  wallet,
  totalValue = 0,
  assetCount = 0,
  className,
}: WalletCardProps) {
  const numValue = typeof totalValue === 'string' ? parseFloat(totalValue) : totalValue

  return (
    <Link to={`/wallets/${wallet.id}`}>
      <Card
        className={cn(
          'transition-colors hover:border-border-hover cursor-pointer',
          className
        )}
      >
        <CardContent className="p-4">
          {/* Header row: wallet icon + info | status + link */}
          <div className="flex items-start justify-between gap-3">
            {/* Left: wallet icon + wallet info */}
            <div className="flex items-center gap-3 min-w-0">
              <div className="flex h-10 w-10 items-center justify-center rounded-lg bg-primary/10 text-primary flex-shrink-0">
                <Wallet className="h-5 w-5" />
              </div>
              <div className="min-w-0">
                <h3 className="font-medium truncate">{wallet.name}</h3>
                {wallet.address && (
                  <AddressDisplay
                    address={wallet.address}
                    className="text-sm text-muted-foreground"
                  />
                )}
              </div>
            </div>
            {/* Right: status badge + external link */}
            <div className="flex items-center gap-2 flex-shrink-0">
              <SyncStatusBadge status={wallet.sync_status} />
              <ExternalLink className="h-4 w-4 text-muted-foreground" />
            </div>
          </div>

          {/* Chain icons row */}
          {wallet.supported_chains && wallet.supported_chains.length > 0 && (
            <div className="mt-2">
              <ChainIconRow chains={wallet.supported_chains} size="xs" maxVisible={7} />
            </div>
          )}

          <div className="mt-4 flex items-end justify-between">
            <div>
              <p className="text-2xl font-semibold">{formatUSD(numValue)}</p>
              <p className="text-sm text-muted-foreground">
                {assetCount} {assetCount === 1 ? 'asset' : 'assets'}
              </p>
            </div>
            {wallet.last_sync_at && (
              <p className="text-xs text-muted-foreground">
                Synced {formatRelativeDate(wallet.last_sync_at)}
              </p>
            )}
          </div>
        </CardContent>
      </Card>
    </Link>
  )
}

// Compact version for dashboard
export function WalletCardCompact({
  wallet,
  totalValue = 0,
  className,
}: Omit<WalletCardProps, 'assetCount'>) {
  const numValue = typeof totalValue === 'string' ? parseFloat(totalValue) : totalValue

  return (
    <Link to={`/wallets/${wallet.id}`}>
      <div
        className={cn(
          'flex items-center justify-between p-3 rounded-lg border border-border transition-colors hover:border-border-hover hover:bg-background-muted',
          className
        )}
      >
        <div className="flex items-center gap-3">
          <div className="flex h-8 w-8 items-center justify-center rounded-md bg-primary/10 text-primary">
            <Wallet className="h-4 w-4" />
          </div>
          <div>
            <div className="flex items-center gap-2">
              <p className="font-medium text-sm">{wallet.name}</p>
              <SyncStatusBadge status={wallet.sync_status} />
            </div>
            {wallet.address && (
              <AddressDisplay
                address={wallet.address}
                truncate
                className="text-xs text-muted-foreground"
              />
            )}
          </div>
        </div>
        <p className="font-medium">{formatUSD(numValue)}</p>
      </div>
    </Link>
  )
}
