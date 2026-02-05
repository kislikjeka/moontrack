import { useParams, Link, useNavigate } from 'react-router-dom'
import { ArrowLeft, Plus, Trash2 } from 'lucide-react'
import { useWallet, useDeleteWallet } from '@/hooks/useWallets'
import { usePortfolio } from '@/hooks/usePortfolio'
import { useTransactions } from '@/hooks/useTransactions'
import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { AddressDisplay } from '@/components/domain/AddressDisplay'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from '@/components/ui/dialog'
import { WalletAssets } from './WalletAssets'
import { WalletTransactions } from './WalletTransactions'
import { toast } from 'sonner'
import { useState } from 'react'

// Chain display names
const chainNames: Record<string, string> = {
  ethereum: 'Ethereum',
  bitcoin: 'Bitcoin',
  solana: 'Solana',
  polygon: 'Polygon',
  'binance-smart-chain': 'BSC',
  arbitrum: 'Arbitrum',
  optimism: 'Optimism',
  avalanche: 'Avalanche',
}

export default function WalletDetailPage() {
  const { id } = useParams<{ id: string }>()
  const navigate = useNavigate()
  const [isDeleteOpen, setIsDeleteOpen] = useState(false)

  const { data: wallet, isLoading: walletLoading } = useWallet(id || '')
  const { data: portfolio, isLoading: portfolioLoading } = usePortfolio()
  const { data: transactionsData, isLoading: transactionsLoading } = useTransactions({
    wallet_id: id,
    page_size: 20,
  })
  const deleteWallet = useDeleteWallet()

  const isLoading = walletLoading || portfolioLoading

  // Find wallet balance from portfolio
  const walletBalance = portfolio?.wallet_balances.find((wb) => wb.wallet_id === id)
  const totalValue = walletBalance
    ? Number(BigInt(walletBalance.total_usd)) / 100000000
    : 0
  const assets = walletBalance?.assets || []

  const handleDelete = async () => {
    if (!id) return

    try {
      await deleteWallet.mutateAsync(id)
      toast.success('Wallet deleted successfully')
      navigate('/wallets')
    } catch (_error) {
      toast.error('Failed to delete wallet')
    }
  }

  if (isLoading) {
    return <WalletDetailSkeleton />
  }

  if (!wallet) {
    return (
      <div className="flex flex-col items-center justify-center py-16">
        <h2 className="text-lg font-medium">Wallet not found</h2>
        <Button asChild variant="link" className="mt-2">
          <Link to="/wallets">Back to wallets</Link>
        </Button>
      </div>
    )
  }

  const chainName = chainNames[wallet.chain_id] || wallet.chain_id

  return (
    <div className="space-y-6">
      {/* Back button */}
      <Button asChild variant="ghost" size="sm" className="-ml-2">
        <Link to="/wallets">
          <ArrowLeft className="mr-2 h-4 w-4" />
          Back to wallets
        </Link>
      </Button>

      {/* Header */}
      <div className="flex flex-col gap-4 sm:flex-row sm:items-start sm:justify-between">
        <div className="space-y-1">
          <div className="flex items-center gap-3">
            <div className="flex h-12 w-12 items-center justify-center rounded-lg bg-primary/10 text-primary font-mono text-sm font-medium">
              {chainName.slice(0, 3).toUpperCase()}
            </div>
            <div>
              <h1 className="text-2xl font-bold tracking-tight">{wallet.name}</h1>
              <p className="text-muted-foreground">{chainName}</p>
            </div>
          </div>
          {wallet.address && (
            <AddressDisplay
              address={wallet.address}
              truncate={false}
              className="text-sm"
            />
          )}
        </div>

        <div className="flex gap-2">
          <Button
            asChild
            onClick={() => navigate('/transactions/new', { state: { walletId: id } })}
          >
            <Link to="/transactions/new" state={{ walletId: id }}>
              <Plus className="mr-2 h-4 w-4" />
              Add Transaction
            </Link>
          </Button>

          <Dialog open={isDeleteOpen} onOpenChange={setIsDeleteOpen}>
            <DialogTrigger asChild>
              <Button variant="outline" className="text-destructive">
                <Trash2 className="mr-2 h-4 w-4" />
                Delete
              </Button>
            </DialogTrigger>
            <DialogContent>
              <DialogHeader>
                <DialogTitle>Delete Wallet</DialogTitle>
                <DialogDescription>
                  Are you sure you want to delete &quot;{wallet.name}&quot;? This action cannot be undone.
                </DialogDescription>
              </DialogHeader>
              <DialogFooter>
                <Button
                  variant="outline"
                  onClick={() => setIsDeleteOpen(false)}
                  disabled={deleteWallet.isPending}
                >
                  Cancel
                </Button>
                <Button
                  variant="destructive"
                  onClick={handleDelete}
                  disabled={deleteWallet.isPending}
                >
                  {deleteWallet.isPending ? 'Deleting...' : 'Delete'}
                </Button>
              </DialogFooter>
            </DialogContent>
          </Dialog>
        </div>
      </div>

      {/* Total value card */}
      <div className="rounded-lg border border-border bg-card p-6">
        <p className="text-sm text-muted-foreground">Total Value</p>
        <p className="text-3xl font-bold tracking-tight">
          ${totalValue.toLocaleString('en-US', { minimumFractionDigits: 2, maximumFractionDigits: 2 })}
        </p>
        <p className="text-sm text-muted-foreground mt-1">
          {assets.length} {assets.length === 1 ? 'asset' : 'assets'}
        </p>
      </div>

      {/* Tabs */}
      <Tabs defaultValue="assets" className="space-y-4">
        <TabsList>
          <TabsTrigger value="assets">Assets</TabsTrigger>
          <TabsTrigger value="transactions">Transactions</TabsTrigger>
        </TabsList>

        <TabsContent value="assets">
          <WalletAssets assets={assets} />
        </TabsContent>

        <TabsContent value="transactions">
          <WalletTransactions
            transactions={transactionsData?.transactions || []}
            isLoading={transactionsLoading}
          />
        </TabsContent>
      </Tabs>
    </div>
  )
}

function WalletDetailSkeleton() {
  return (
    <div className="space-y-6">
      <Skeleton className="h-8 w-32" />
      <div className="flex flex-col gap-4 sm:flex-row sm:items-start sm:justify-between">
        <div className="flex items-center gap-3">
          <Skeleton className="h-12 w-12" />
          <div className="space-y-2">
            <Skeleton className="h-8 w-48" />
            <Skeleton className="h-4 w-24" />
          </div>
        </div>
        <div className="flex gap-2">
          <Skeleton className="h-10 w-36" />
          <Skeleton className="h-10 w-24" />
        </div>
      </div>
      <Skeleton className="h-32" />
      <Skeleton className="h-10 w-48" />
      <Skeleton className="h-64" />
    </div>
  )
}
