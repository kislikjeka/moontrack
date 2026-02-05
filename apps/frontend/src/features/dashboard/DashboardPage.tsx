import { Wallet, ArrowLeftRight, TrendingUp, Coins } from 'lucide-react'
import { Link } from 'react-router-dom'
import { usePortfolio } from '@/hooks/usePortfolio'
import { useWallets } from '@/hooks/useWallets'
import { useTransactions } from '@/hooks/useTransactions'
import { StatCard } from '@/components/domain/StatCard'
import { Skeleton } from '@/components/ui/skeleton'
import { Button } from '@/components/ui/button'
import { formatUSD } from '@/lib/format'
import { PortfolioSummary } from './PortfolioSummary'
import { AssetDistributionChart } from './AssetDistributionChart'
import { RecentTransactions } from './RecentTransactions'
import { WalletsList } from './WalletsList'

export default function DashboardPage() {
  const { data: portfolio, isLoading: portfolioLoading } = usePortfolio()
  const { data: wallets, isLoading: walletsLoading } = useWallets()
  const { data: transactionsData, isLoading: transactionsLoading } =
    useTransactions({ page_size: 5 })

  const isLoading = portfolioLoading || walletsLoading || transactionsLoading

  if (isLoading) {
    return <DashboardSkeleton />
  }

  const totalValue = portfolio?.total_usd_value || '0'
  const totalAssets = portfolio?.total_assets || 0
  const walletCount = wallets?.length || 0
  const transactionCount = transactionsData?.total || 0

  return (
    <div className="space-y-6">
      {/* Page header */}
      <div className="flex flex-col gap-4 sm:flex-row sm:items-center sm:justify-between">
        <div>
          <h1 className="text-2xl font-bold tracking-tight">Dashboard</h1>
          <p className="text-muted-foreground">
            Overview of your crypto portfolio
          </p>
        </div>
        <div className="flex gap-2">
          <Button asChild variant="outline">
            <Link to="/wallets">
              <Wallet className="mr-2 h-4 w-4" />
              Wallets
            </Link>
          </Button>
          <Button asChild>
            <Link to="/transactions/new">
              <ArrowLeftRight className="mr-2 h-4 w-4" />
              New Transaction
            </Link>
          </Button>
        </div>
      </div>

      {/* Stats cards */}
      <div className="grid gap-4 md:grid-cols-2 lg:grid-cols-4">
        <StatCard
          label="Total Portfolio Value"
          value={formatUSD(parseFloat(totalValue))}
          icon={TrendingUp}
          iconColor="primary"
        />
        <StatCard
          label="Total Assets"
          value={totalAssets}
          icon={Coins}
          iconColor="profit"
        />
        <StatCard
          label="Wallets"
          value={walletCount}
          icon={Wallet}
          iconColor="primary"
        />
        <StatCard
          label="Transactions"
          value={transactionCount}
          icon={ArrowLeftRight}
          iconColor="muted"
        />
      </div>

      {/* Main content grid */}
      <div className="grid gap-6 lg:grid-cols-2">
        {/* Left column */}
        <div className="space-y-6">
          <PortfolioSummary portfolio={portfolio} />
          <AssetDistributionChart holdings={portfolio?.asset_holdings || []} />
        </div>

        {/* Right column */}
        <div className="space-y-6">
          <WalletsList
            wallets={wallets || []}
            walletBalances={portfolio?.wallet_balances || []}
          />
          <RecentTransactions
            transactions={transactionsData?.transactions || []}
          />
        </div>
      </div>
    </div>
  )
}

function DashboardSkeleton() {
  return (
    <div className="space-y-6">
      <div className="flex flex-col gap-4 sm:flex-row sm:items-center sm:justify-between">
        <div className="space-y-2">
          <Skeleton className="h-8 w-32" />
          <Skeleton className="h-4 w-48" />
        </div>
        <div className="flex gap-2">
          <Skeleton className="h-10 w-24" />
          <Skeleton className="h-10 w-36" />
        </div>
      </div>

      <div className="grid gap-4 md:grid-cols-2 lg:grid-cols-4">
        {[...Array(4)].map((_, i) => (
          <Skeleton key={i} className="h-28" />
        ))}
      </div>

      <div className="grid gap-6 lg:grid-cols-2">
        <div className="space-y-6">
          <Skeleton className="h-48" />
          <Skeleton className="h-64" />
        </div>
        <div className="space-y-6">
          <Skeleton className="h-48" />
          <Skeleton className="h-64" />
        </div>
      </div>
    </div>
  )
}
