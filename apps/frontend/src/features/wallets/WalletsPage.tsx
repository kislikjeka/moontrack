import { useState } from 'react'
import { Plus, Wallet } from 'lucide-react'
import { useWallets } from '@/hooks/useWallets'
import { usePortfolio } from '@/hooks/usePortfolio'
import { WalletCard } from '@/components/domain/WalletCard'
import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
import { CreateWalletDialog } from './CreateWalletDialog'

export default function WalletsPage() {
  const [isCreateOpen, setIsCreateOpen] = useState(false)
  const { data: wallets, isLoading: walletsLoading } = useWallets()
  const { data: portfolio, isLoading: portfolioLoading } = usePortfolio()

  const isLoading = walletsLoading || portfolioLoading

  // Create a map of wallet ID to balance and asset count
  const walletBalanceMap = new Map(
    portfolio?.wallet_balances.map((wb) => [
      wb.wallet_id,
      {
        total: wb.total_usd,
        assetCount: wb.assets.length,
      },
    ])
  )

  if (isLoading) {
    return <WalletsSkeleton />
  }

  return (
    <div className="space-y-6">
      {/* Page header */}
      <div className="flex flex-col gap-4 sm:flex-row sm:items-center sm:justify-between">
        <div>
          <h1 className="text-2xl font-bold tracking-tight">Wallets</h1>
          <p className="text-muted-foreground">
            Manage your crypto wallets
          </p>
        </div>
        <Button onClick={() => setIsCreateOpen(true)}>
          <Plus className="mr-2 h-4 w-4" />
          Create Wallet
        </Button>
      </div>

      {/* Wallets grid */}
      {wallets && wallets.length > 0 ? (
        <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
          {wallets.map((wallet) => {
            const balanceInfo = walletBalanceMap.get(wallet.id)
            const totalValue = balanceInfo
              ? Number(BigInt(balanceInfo.total)) / 100000000
              : 0
            const assetCount = balanceInfo?.assetCount || 0

            return (
              <WalletCard
                key={wallet.id}
                wallet={wallet}
                totalValue={totalValue}
                assetCount={assetCount}
              />
            )
          })}
        </div>
      ) : (
        <div className="flex flex-col items-center justify-center py-16 text-center">
          <div className="rounded-full bg-muted p-4 mb-4">
            <Wallet className="h-8 w-8 text-muted-foreground" />
          </div>
          <h3 className="text-lg font-medium">No wallets yet</h3>
          <p className="text-muted-foreground mt-1 mb-4">
            Create your first wallet to start tracking your portfolio
          </p>
          <Button onClick={() => setIsCreateOpen(true)}>
            <Plus className="mr-2 h-4 w-4" />
            Create Wallet
          </Button>
        </div>
      )}

      {/* Create wallet dialog */}
      <CreateWalletDialog
        open={isCreateOpen}
        onOpenChange={setIsCreateOpen}
      />
    </div>
  )
}

function WalletsSkeleton() {
  return (
    <div className="space-y-6">
      <div className="flex flex-col gap-4 sm:flex-row sm:items-center sm:justify-between">
        <div className="space-y-2">
          <Skeleton className="h-8 w-32" />
          <Skeleton className="h-4 w-48" />
        </div>
        <Skeleton className="h-10 w-36" />
      </div>
      <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
        {[...Array(3)].map((_, i) => (
          <Skeleton key={i} className="h-40" />
        ))}
      </div>
    </div>
  )
}
