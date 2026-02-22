import { Fragment, useState } from 'react'
import { Link } from 'react-router-dom'
import { ChevronDown, ChevronRight } from 'lucide-react'
import { Button } from '@/components/ui/button'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { TransactionTypeBadge } from '@/components/domain/TransactionTypeBadge'
import { ChainIcon } from '@/components/domain/ChainIcon'
import { TransactionLotImpactSection } from './TransactionLotImpactSection'
import { formatDateTime, formatUSD } from '@/lib/format'
import { getChainShortName } from '@/types/wallet'
import type { TransactionListItem } from '@/types/transaction'

interface TransactionListTableProps {
  transactions: TransactionListItem[]
  total?: number
  page?: number
  pageSize?: number
  onPageChange?: (page: number) => void
  showWalletColumn?: boolean
}

export function TransactionListTable({
  transactions,
  total = 0,
  page = 1,
  pageSize = 20,
  onPageChange,
  showWalletColumn = true,
}: TransactionListTableProps) {
  const [expandedRows, setExpandedRows] = useState<Set<string>>(new Set())

  const toggleRow = (id: string) => {
    setExpandedRows((prev) => {
      const next = new Set(prev)
      if (next.has(id)) {
        next.delete(id)
      } else {
        next.add(id)
      }
      return next
    })
  }

  const totalPages = Math.ceil(total / pageSize)
  const colSpan = showWalletColumn ? 8 : 7

  return (
    <>
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead className="w-8" />
            <TableHead>Type</TableHead>
            <TableHead>Network</TableHead>
            {showWalletColumn && <TableHead>Wallet</TableHead>}
            <TableHead>Asset</TableHead>
            <TableHead className="text-right">Amount</TableHead>
            <TableHead className="text-right">Value</TableHead>
            <TableHead className="text-right">Date</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {transactions.map((tx) => (
            <Fragment key={tx.id}>
              <TableRow>
                <TableCell>
                  <button
                    onClick={(e) => {
                      e.stopPropagation()
                      e.preventDefault()
                      toggleRow(tx.id)
                    }}
                    className="p-1 rounded hover:bg-muted"
                  >
                    {expandedRows.has(tx.id) ? (
                      <ChevronDown className="h-4 w-4 text-muted-foreground" />
                    ) : (
                      <ChevronRight className="h-4 w-4 text-muted-foreground" />
                    )}
                  </button>
                </TableCell>
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
                {showWalletColumn && (
                  <TableCell>
                    <Link
                      to={`/transactions/${tx.id}`}
                      className="text-sm text-muted-foreground hover:text-foreground"
                    >
                      {tx.wallet_name}
                    </Link>
                  </TableCell>
                )}
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
              {expandedRows.has(tx.id) && (
                <TableRow>
                  <TableCell colSpan={colSpan} className="p-0 border-b">
                    <div className="bg-muted/30 px-4 py-2">
                      <TransactionLotImpactSection transactionId={tx.id} />
                    </div>
                  </TableCell>
                </TableRow>
              )}
            </Fragment>
          ))}
        </TableBody>
      </Table>

      {onPageChange && total > pageSize && (
        <div className="flex items-center justify-between pt-4">
          <p className="text-sm text-muted-foreground">
            Page {page} of {totalPages}
          </p>
          <div className="flex gap-2">
            <Button
              variant="outline"
              size="sm"
              disabled={page <= 1}
              onClick={() => onPageChange(page - 1)}
            >
              Previous
            </Button>
            <Button
              variant="outline"
              size="sm"
              disabled={page >= totalPages}
              onClick={() => onPageChange(page + 1)}
            >
              Next
            </Button>
          </div>
        </div>
      )}
    </>
  )
}
