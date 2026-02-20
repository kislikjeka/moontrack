import { Link } from 'react-router-dom'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { Skeleton } from '@/components/ui/skeleton'
import { TransactionTypeBadge } from '@/components/domain/TransactionTypeBadge'
import { ChainIcon } from '@/components/domain/ChainIcon'
import { formatDateTime, formatUSD } from '@/lib/format'
import { getChainShortName } from '@/types/wallet'
import type { TransactionListItem } from '@/types/transaction'

interface WalletTransactionsProps {
  transactions: TransactionListItem[]
  isLoading?: boolean
}

export function WalletTransactions({ transactions, isLoading }: WalletTransactionsProps) {
  if (isLoading) {
    return (
      <Card>
        <CardHeader>
          <CardTitle className="text-base font-medium">Transactions</CardTitle>
        </CardHeader>
        <CardContent>
          <div className="space-y-3">
            {[...Array(3)].map((_, i) => (
              <Skeleton key={i} className="h-12" />
            ))}
          </div>
        </CardContent>
      </Card>
    )
  }

  if (!transactions || transactions.length === 0) {
    return (
      <Card>
        <CardHeader>
          <CardTitle className="text-base font-medium">Transactions</CardTitle>
        </CardHeader>
        <CardContent>
          <div className="flex flex-col items-center justify-center py-8 text-muted-foreground">
            <p>No transactions for this wallet</p>
            <p className="text-sm">Add a transaction to start tracking</p>
          </div>
        </CardContent>
      </Card>
    )
  }

  return (
    <Card>
      <CardHeader>
        <CardTitle className="text-base font-medium">Transactions</CardTitle>
      </CardHeader>
      <CardContent>
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>Type</TableHead>
              <TableHead>Network</TableHead>
              <TableHead>Asset</TableHead>
              <TableHead className="text-right">Amount</TableHead>
              <TableHead className="text-right">Value</TableHead>
              <TableHead className="text-right">Date</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {transactions.map((tx) => (
              <TableRow key={tx.id} className="cursor-pointer">
                <TableCell>
                  <Link to={`/transactions/${tx.id}`}>
                    <TransactionTypeBadge type={tx.type} />
                  </Link>
                </TableCell>
                <TableCell>
                  <Link to={`/transactions/${tx.id}`}>
                    {tx.chain_id ? (
                      <div className="flex items-center gap-1.5">
                        <ChainIcon chainId={tx.chain_id} size="xs" />
                        <span className="text-xs text-muted-foreground">
                          {getChainShortName(tx.chain_id)}
                        </span>
                      </div>
                    ) : (
                      <span className="text-xs text-muted-foreground">-</span>
                    )}
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
                    {tx.usd_value ? formatUSD(tx.usd_value) : '-'}
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
      </CardContent>
    </Card>
  )
}
