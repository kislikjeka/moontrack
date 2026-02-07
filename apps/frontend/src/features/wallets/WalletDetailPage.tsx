import { useParams, Link, useNavigate } from 'react-router-dom'
import { ArrowLeft, Trash2, RefreshCw, AlertCircle } from 'lucide-react'
import { useWallet, useDeleteWallet, useTriggerSync } from '@/hooks/useWallets'
import { usePortfolio } from '@/hooks/usePortfolio'
import { useTransactions } from '@/hooks/useTransactions'
import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { AddressDisplay } from '@/components/domain/AddressDisplay'
import { SyncStatusBadge } from '@/components/domain/SyncStatusBadge'
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
import { formatRelativeDate } from '@/lib/format'
import { getChainName, getChainSymbol } from '@/types/wallet'

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
  const triggerSync = useTriggerSync()

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

  const handleSync = async () => {
    if (!id) return

    try {
      await triggerSync.mutateAsync(id)
      toast.success('Sync started')
    } catch (_error) {
      toast.error('Failed to start sync')
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

  const chainName = getChainName(wallet.chain_id)
  const chainLabel = getChainSymbol(wallet.chain_id)

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
              {chainLabel}
            </div>
            <div>
              <div className="flex items-center gap-2">
                <h1 className="text-2xl font-bold tracking-tight">{wallet.name}</h1>
                <SyncStatusBadge status={wallet.sync_status} />
              </div>
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
            variant="outline"
            onClick={handleSync}
            disabled={triggerSync.isPending || wallet.sync_status === 'syncing'}
          >
            <RefreshCw className={`mr-2 h-4 w-4 ${triggerSync.isPending || wallet.sync_status === 'syncing' ? 'animate-spin' : ''}`} />
            {wallet.sync_status === 'syncing' ? 'Syncing...' : 'Sync Now'}
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

      {/* Sync error alert */}
      {wallet.sync_status === 'error' && wallet.sync_error && (
        <Alert variant="destructive">
          <AlertCircle className="h-4 w-4" />
          <AlertTitle>Sync Error</AlertTitle>
          <AlertDescription>{wallet.sync_error}</AlertDescription>
        </Alert>
      )}

      {/* Total value card */}
      <div className="rounded-lg border border-border bg-card p-6">
        <p className="text-sm text-muted-foreground">Total Value</p>
        <p className="text-3xl font-bold tracking-tight">
          ${totalValue.toLocaleString('en-US', { minimumFractionDigits: 2, maximumFractionDigits: 2 })}
        </p>
        <div className="flex items-center gap-4 mt-1">
          <p className="text-sm text-muted-foreground">
            {assets.length} {assets.length === 1 ? 'asset' : 'assets'}
          </p>
          {wallet.last_sync_at && (
            <p className="text-sm text-muted-foreground">
              Last synced {formatRelativeDate(wallet.last_sync_at)}
              {wallet.last_sync_block && ` (block ${wallet.last_sync_block.toLocaleString()})`}
            </p>
          )}
        </div>
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
          <Skeleton className="h-10 w-28" />
          <Skeleton className="h-10 w-24" />
        </div>
      </div>
      <Skeleton className="h-32" />
      <Skeleton className="h-10 w-48" />
      <Skeleton className="h-64" />
    </div>
  )
}
