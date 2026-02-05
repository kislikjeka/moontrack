import { Link } from 'react-router-dom'
import { ArrowRight, Plus } from 'lucide-react'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { WalletCardCompact } from '@/components/domain/WalletCard'
import type { Wallet } from '@/types/wallet'
import type { WalletBalance } from '@/types/portfolio'

interface WalletsListProps {
  wallets: Wallet[]
  walletBalances: WalletBalance[]
}

export function WalletsList({ wallets, walletBalances }: WalletsListProps) {
  // Create a map of wallet ID to balance for quick lookup
  const balanceMap = new Map(
    walletBalances.map((wb) => [wb.wallet_id, wb.total_usd])
  )

  if (!wallets || wallets.length === 0) {
    return (
      <Card>
        <CardHeader className="flex flex-row items-center justify-between">
          <CardTitle className="text-base font-medium">Wallets</CardTitle>
        </CardHeader>
        <CardContent>
          <div className="flex flex-col items-center justify-center py-8 text-muted-foreground">
            <p>No wallets yet</p>
            <Button asChild variant="link" className="mt-2">
              <Link to="/wallets">
                <Plus className="mr-1 h-4 w-4" />
                Create your first wallet
              </Link>
            </Button>
          </div>
        </CardContent>
      </Card>
    )
  }

  return (
    <Card>
      <CardHeader className="flex flex-row items-center justify-between pb-2">
        <CardTitle className="text-base font-medium">Wallets</CardTitle>
        <Button asChild variant="ghost" size="sm">
          <Link to="/wallets" className="flex items-center gap-1">
            View all
            <ArrowRight className="h-4 w-4" />
          </Link>
        </Button>
      </CardHeader>
      <CardContent>
        <div className="space-y-2">
          {wallets.slice(0, 4).map((wallet) => {
            const balance = balanceMap.get(wallet.id) || '0'
            // Convert from scaled value
            const usdValue = Number(BigInt(balance)) / 100000000
            return (
              <WalletCardCompact
                key={wallet.id}
                wallet={wallet}
                totalValue={usdValue}
              />
            )
          })}
          {wallets.length > 4 && (
            <p className="text-sm text-muted-foreground text-center pt-2">
              +{wallets.length - 4} more wallets
            </p>
          )}
        </div>
      </CardContent>
    </Card>
  )
}
