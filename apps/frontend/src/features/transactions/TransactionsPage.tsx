import { useState } from 'react'
import { Link } from 'react-router-dom'
import { RefreshCw, ArrowLeftRight } from 'lucide-react'
import { useTransactions } from '@/hooks/useTransactions'
import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { TransactionTypeBadge } from '@/components/domain/TransactionTypeBadge'
import { TransactionFilters } from './TransactionFilters'
import { formatDateTime } from '@/lib/format'
import type { TransactionFilters as FiltersType } from '@/types/transaction'

// Format USD value from scaled integer
function formatUSDValue(value: string): string {
  try {
    const bigIntValue = BigInt(value)
    const dollars = Number(bigIntValue) / 100000000
    return new Intl.NumberFormat('en-US', {
      style: 'currency',
      currency: 'USD',
      minimumFractionDigits: 2,
      maximumFractionDigits: 2,
    }).format(dollars)
  } catch {
    return '$0.00'
  }
}

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
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Type</TableHead>
                  <TableHead>Wallet</TableHead>
                  <TableHead>Asset</TableHead>
                  <TableHead className="text-right">Amount</TableHead>
                  <TableHead className="text-right">Value</TableHead>
                  <TableHead className="text-right">Date</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {transactions.map((tx) => (
                  <TableRow key={tx.id}>
                    <TableCell>
                      <Link to={`/transactions/${tx.id}`}>
                        <TransactionTypeBadge type={tx.type} />
                      </Link>
                    </TableCell>
                    <TableCell>
                      <Link
                        to={`/transactions/${tx.id}`}
                        className="text-sm text-muted-foreground hover:text-foreground"
                      >
                        {tx.wallet_name}
                      </Link>
                    </TableCell>
                    <TableCell>
                      <Link to={`/transactions/${tx.id}`} className="font-medium">
                        {tx.asset_symbol}
                      </Link>
                    </TableCell>
                    <TableCell className="text-right font-mono">
                      <Link to={`/transactions/${tx.id}`}>
                        {tx.display_amount}
                      </Link>
                    </TableCell>
                    <TableCell className="text-right">
                      <Link to={`/transactions/${tx.id}`}>
                        {tx.usd_value ? formatUSDValue(tx.usd_value) : '-'}
                      </Link>
                    </TableCell>
                    <TableCell className="text-right text-muted-foreground">
                      <Link to={`/transactions/${tx.id}`}>
                        {formatDateTime(tx.occurred_at)}
                      </Link>
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>

            {/* Pagination */}
            {total > (filters.page_size || 20) && (
              <div className="flex items-center justify-between pt-4">
                <p className="text-sm text-muted-foreground">
                  Page {filters.page || 1} of {Math.ceil(total / (filters.page_size || 20))}
                </p>
                <div className="flex gap-2">
                  <Button
                    variant="outline"
                    size="sm"
                    disabled={(filters.page || 1) <= 1}
                    onClick={() =>
                      setFilters({ ...filters, page: (filters.page || 1) - 1 })
                    }
                  >
                    Previous
                  </Button>
                  <Button
                    variant="outline"
                    size="sm"
                    disabled={(filters.page || 1) >= Math.ceil(total / (filters.page_size || 20))}
                    onClick={() =>
                      setFilters({ ...filters, page: (filters.page || 1) + 1 })
                    }
                  >
                    Next
                  </Button>
                </div>
              </div>
            )}
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
