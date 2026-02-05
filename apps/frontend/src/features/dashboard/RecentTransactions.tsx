import { Link } from 'react-router-dom'
import { ArrowRight } from 'lucide-react'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { TransactionTypeBadge } from '@/components/domain/TransactionTypeBadge'
import { formatDateTime } from '@/lib/format'
import type { TransactionListItem } from '@/types/transaction'

interface RecentTransactionsProps {
  transactions: TransactionListItem[]
}

export function RecentTransactions({ transactions }: RecentTransactionsProps) {
  if (!transactions || transactions.length === 0) {
    return (
      <Card>
        <CardHeader className="flex flex-row items-center justify-between">
          <CardTitle className="text-base font-medium">Recent Transactions</CardTitle>
        </CardHeader>
        <CardContent>
          <div className="flex flex-col items-center justify-center py-8 text-muted-foreground">
            <p>No transactions yet</p>
            <Button asChild variant="link" className="mt-2">
              <Link to="/transactions/new">Add your first transaction</Link>
            </Button>
          </div>
        </CardContent>
      </Card>
    )
  }

  return (
    <Card>
      <CardHeader className="flex flex-row items-center justify-between pb-2">
        <CardTitle className="text-base font-medium">Recent Transactions</CardTitle>
        <Button asChild variant="ghost" size="sm">
          <Link to="/transactions" className="flex items-center gap-1">
            View all
            <ArrowRight className="h-4 w-4" />
          </Link>
        </Button>
      </CardHeader>
      <CardContent>
        <div className="space-y-3">
          {transactions.map((tx) => (
            <Link
              key={tx.id}
              to={`/transactions/${tx.id}`}
              className="flex items-center justify-between p-3 rounded-lg border border-border hover:border-border-hover hover:bg-background-muted transition-colors"
            >
              <div className="flex items-center gap-3">
                <TransactionTypeBadge type={tx.type} showLabel={false} />
                <div>
                  <p className="text-sm font-medium">{tx.display_amount}</p>
                  <p className="text-xs text-muted-foreground">{tx.wallet_name}</p>
                </div>
              </div>
              <div className="text-right">
                {tx.usd_value && (
                  <p className="text-sm font-medium">
                    ${(Number(BigInt(tx.usd_value)) / 100000000).toFixed(2)}
                  </p>
                )}
                <p className="text-xs text-muted-foreground">
                  {formatDateTime(tx.occurred_at)}
                </p>
              </div>
            </Link>
          ))}
        </div>
      </CardContent>
    </Card>
  )
}
