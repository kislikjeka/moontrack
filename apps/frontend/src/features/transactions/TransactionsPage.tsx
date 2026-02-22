import { useState } from 'react'
import { Link } from 'react-router-dom'
import { RefreshCw, ArrowLeftRight } from 'lucide-react'
import { useTransactions } from '@/hooks/useTransactions'
import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { TransactionFilters } from './TransactionFilters'
import { TransactionListTable } from './TransactionListTable'
import type { TransactionFilters as FiltersType } from '@/types/transaction'

export default function TransactionsPage() {
  const [filters, setFilters] = useState<FiltersType>({
    page: 1,
    page_size: 20,
  })

  const { data, isLoading } = useTransactions(filters)
  const transactions = data?.transactions || []
  const total = data?.total || 0

  if (isLoading) {
    return <TransactionsSkeleton />
  }

  return (
    <div className="space-y-6">
      {/* Page header */}
      <div className="flex flex-col gap-4 sm:flex-row sm:items-center sm:justify-between">
        <div>
          <h1 className="text-2xl font-bold tracking-tight">Transactions</h1>
          <p className="text-muted-foreground">
            View your transaction history. Transfers are synced automatically.
          </p>
        </div>
        <Button asChild>
          <Link to="/transactions/new">
            <RefreshCw className="mr-2 h-4 w-4" />
            Balance Adjustment
          </Link>
        </Button>
      </div>

      {/* Filters */}
      <TransactionFilters filters={filters} onFiltersChange={setFilters} />

      {/* Transactions table */}
      {transactions.length > 0 ? (
        <Card>
          <CardHeader className="pb-2">
            <CardTitle className="text-base font-medium">
              {total} {total === 1 ? 'Transaction' : 'Transactions'}
            </CardTitle>
          </CardHeader>
          <CardContent>
            <TransactionListTable
              transactions={transactions}
              total={total}
              page={filters.page || 1}
              pageSize={filters.page_size || 20}
              onPageChange={(page) => setFilters({ ...filters, page })}
            />
          </CardContent>
        </Card>
      ) : (
        <div className="flex flex-col items-center justify-center py-16 text-center">
          <div className="rounded-full bg-muted p-4 mb-4">
            <ArrowLeftRight className="h-8 w-8 text-muted-foreground" />
          </div>
          <h3 className="text-lg font-medium">No transactions found</h3>
          <p className="text-muted-foreground mt-1 mb-4">
            {filters.wallet_id || filters.type || filters.asset_id
              ? 'Try adjusting your filters'
              : 'Transactions will appear here once your wallets are synced'}
          </p>
          <Button asChild>
            <Link to="/transactions/new">
              <RefreshCw className="mr-2 h-4 w-4" />
              Balance Adjustment
            </Link>
          </Button>
        </div>
      )}
    </div>
  )
}

function TransactionsSkeleton() {
  return (
    <div className="space-y-6">
      <div className="flex flex-col gap-4 sm:flex-row sm:items-center sm:justify-between">
        <div className="space-y-2">
          <Skeleton className="h-8 w-32" />
          <Skeleton className="h-4 w-48" />
        </div>
        <Skeleton className="h-10 w-40" />
      </div>
      <div className="flex gap-4">
        <Skeleton className="h-10 w-40" />
        <Skeleton className="h-10 w-40" />
      </div>
      <Skeleton className="h-96" />
    </div>
  )
}
