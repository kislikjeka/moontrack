import { useParams, Link } from 'react-router-dom'
import { ArrowLeft } from 'lucide-react'
import { useTransaction } from '@/hooks/useTransactions'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Skeleton } from '@/components/ui/skeleton'
import { Badge } from '@/components/ui/badge'
import { TransactionTypeBadge } from '@/components/domain/TransactionTypeBadge'
import { LedgerEntriesTable } from './LedgerEntriesTable'
import { formatDateTime } from '@/lib/format'

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

export default function TransactionDetailPage() {
  const { id } = useParams<{ id: string }>()
  const { data: transaction, isLoading } = useTransaction(id || '')

  if (isLoading) {
    return <TransactionDetailSkeleton />
  }

  if (!transaction) {
    return (
      <div className="flex flex-col items-center justify-center py-16">
        <h2 className="text-lg font-medium">Transaction not found</h2>
        <Button asChild variant="link" className="mt-2">
          <Link to="/transactions">Back to transactions</Link>
        </Button>
      </div>
    )
  }

  return (
    <div className="max-w-4xl mx-auto space-y-6">
      {/* Back button */}
      <Button asChild variant="ghost" size="sm" className="-ml-2">
        <Link to="/transactions">
          <ArrowLeft className="mr-2 h-4 w-4" />
          Back to transactions
        </Link>
      </Button>

      {/* Header */}
      <div className="flex flex-col gap-4 sm:flex-row sm:items-start sm:justify-between">
        <div className="space-y-2">
          <div className="flex items-center gap-3">
            <TransactionTypeBadge type={transaction.type} size="lg" />
            <Badge
              variant={transaction.status === 'COMPLETED' ? 'profit' : 'loss'}
            >
              {transaction.status}
            </Badge>
          </div>
          <h1 className="text-2xl font-bold tracking-tight">
            {transaction.display_amount}
          </h1>
          <p className="text-muted-foreground">
            {formatDateTime(transaction.occurred_at)}
          </p>
        </div>

        {transaction.usd_value && (
          <div className="text-right">
            <p className="text-sm text-muted-foreground">Value</p>
            <p className="text-2xl font-bold">{formatUSDValue(transaction.usd_value)}</p>
          </div>
        )}
      </div>

      {/* Details card */}
      <Card>
        <CardHeader>
          <CardTitle className="text-base font-medium">Transaction Details</CardTitle>
        </CardHeader>
        <CardContent>
          <dl className="grid gap-4 sm:grid-cols-2">
            <div>
              <dt className="text-sm text-muted-foreground">Type</dt>
              <dd className="font-medium">{transaction.type_label}</dd>
            </div>
            <div>
              <dt className="text-sm text-muted-foreground">Wallet</dt>
              <dd className="font-medium">
                <Link
                  to={`/wallets/${transaction.wallet_id}`}
                  className="text-primary hover:underline"
                >
                  {transaction.wallet_name}
                </Link>
              </dd>
            </div>
            <div>
              <dt className="text-sm text-muted-foreground">Asset</dt>
              <dd className="font-medium">{transaction.asset_symbol}</dd>
            </div>
            <div>
              <dt className="text-sm text-muted-foreground">Amount</dt>
              <dd className="font-mono font-medium">{transaction.display_amount}</dd>
            </div>
            <div>
              <dt className="text-sm text-muted-foreground">Occurred At</dt>
              <dd className="font-medium">{formatDateTime(transaction.occurred_at)}</dd>
            </div>
            <div>
              <dt className="text-sm text-muted-foreground">Recorded At</dt>
              <dd className="font-medium">{formatDateTime(transaction.recorded_at)}</dd>
            </div>
            {transaction.notes && (
              <div className="sm:col-span-2">
                <dt className="text-sm text-muted-foreground">Notes</dt>
                <dd className="font-medium">{transaction.notes}</dd>
              </div>
            )}
          </dl>
        </CardContent>
      </Card>

      {/* Ledger entries */}
      <LedgerEntriesTable entries={transaction.entries} />
    </div>
  )
}

function TransactionDetailSkeleton() {
  return (
    <div className="max-w-4xl mx-auto space-y-6">
      <Skeleton className="h-8 w-40" />
      <div className="flex flex-col gap-4 sm:flex-row sm:items-start sm:justify-between">
        <div className="space-y-2">
          <Skeleton className="h-6 w-24" />
          <Skeleton className="h-8 w-48" />
          <Skeleton className="h-4 w-32" />
        </div>
        <Skeleton className="h-16 w-32" />
      </div>
      <Skeleton className="h-48" />
      <Skeleton className="h-64" />
    </div>
  )
}
