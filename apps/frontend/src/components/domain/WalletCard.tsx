import { Link } from 'react-router-dom'
import { ExternalLink } from 'lucide-react'
import { Card, CardContent } from '@/components/ui/card'
import { AddressDisplay } from './AddressDisplay'
import { cn } from '@/lib/utils'
import { formatUSD } from '@/lib/format'
import type { Wallet as WalletType } from '@/types/wallet'

interface WalletCardProps {
  wallet: WalletType
  totalValue?: string | number
  assetCount?: number
  className?: string
}

// Chain icons mapping - using emoji for simplicity, could be replaced with proper icons
const chainIcons: Record<string, string> = {
  ethereum: 'ETH',
  bitcoin: 'BTC',
  solana: 'SOL',
  polygon: 'MATIC',
  'binance-smart-chain': 'BSC',
  arbitrum: 'ARB',
  optimism: 'OP',
  avalanche: 'AVAX',
}

export function WalletCard({
  wallet,
  totalValue = 0,
  assetCount = 0,
  className,
}: WalletCardProps) {
  const chainLabel = chainIcons[wallet.chain_id] || wallet.chain_id.toUpperCase()
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
          <div className="flex items-start justify-between gap-3">
            <div className="flex items-center gap-3">
              <div className="flex h-10 w-10 items-center justify-center rounded-lg bg-primary/10 text-primary font-mono text-xs font-medium">
                {chainLabel}
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
            <ExternalLink className="h-4 w-4 text-muted-foreground flex-shrink-0" />
          </div>

          <div className="mt-4 flex items-end justify-between">
            <div>
              <p className="text-2xl font-semibold">{formatUSD(numValue)}</p>
              <p className="text-sm text-muted-foreground">
                {assetCount} {assetCount === 1 ? 'asset' : 'assets'}
              </p>
            </div>
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
  const chainLabel = chainIcons[wallet.chain_id] || wallet.chain_id.toUpperCase()
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
          <div className="flex h-8 w-8 items-center justify-center rounded-md bg-primary/10 text-primary font-mono text-xs font-medium">
            {chainLabel}
          </div>
          <div>
            <p className="font-medium text-sm">{wallet.name}</p>
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
