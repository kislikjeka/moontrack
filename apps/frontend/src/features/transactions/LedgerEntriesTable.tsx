import { CheckCircle } from 'lucide-react'
import { Card, CardContent, CardHeader, CardTitle, CardDescription } from '@/components/ui/card'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { Badge } from '@/components/ui/badge'
import type { LedgerEntry } from '@/types/transaction'

interface LedgerEntriesTableProps {
  entries: LedgerEntry[]
}

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

export function LedgerEntriesTable({ entries }: LedgerEntriesTableProps) {
  if (!entries || entries.length === 0) {
    return (
      <Card>
        <CardHeader>
          <CardTitle className="text-base font-medium">Ledger Entries</CardTitle>
        </CardHeader>
        <CardContent>
          <p className="text-muted-foreground">No ledger entries available</p>
        </CardContent>
      </Card>
    )
  }

  // Calculate totals for balance check
  const totals = entries.reduce(
    (acc, entry) => {
      const value = Number(BigInt(entry.usd_value || '0')) / 100000000
      if (entry.debit_credit === 'DEBIT') {
        acc.debit += value
      } else {
        acc.credit += value
      }
      return acc
    },
    { debit: 0, credit: 0 }
  )

  const isBalanced = Math.abs(totals.debit - totals.credit) < 0.01

  return (
    <Card>
      <CardHeader>
        <div className="flex items-center justify-between">
          <div>
            <CardTitle className="text-base font-medium">Ledger Entries</CardTitle>
            <CardDescription>
              Double-entry accounting records for this transaction
            </CardDescription>
          </div>
          {isBalanced && (
            <div className="flex items-center gap-1 text-sm text-profit">
              <CheckCircle className="h-4 w-4" />
              Balanced
            </div>
          )}
        </div>
      </CardHeader>
      <CardContent>
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>Account</TableHead>
              <TableHead>Type</TableHead>
              <TableHead>Asset</TableHead>
              <TableHead className="text-right">Amount</TableHead>
              <TableHead className="text-right">Debit</TableHead>
              <TableHead className="text-right">Credit</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {entries.map((entry) => (
              <TableRow key={entry.id}>
                <TableCell>
                  <div>
                    <p className="font-medium">{entry.account_label}</p>
                    <p className="text-xs text-muted-foreground font-mono">
                      {entry.account_code}
                    </p>
                  </div>
                </TableCell>
                <TableCell>
                  <Badge
                    variant={entry.debit_credit === 'DEBIT' ? 'secondary' : 'outline'}
                  >
                    {entry.debit_credit}
                  </Badge>
                </TableCell>
                <TableCell>{entry.asset_symbol}</TableCell>
                <TableCell className="text-right font-mono">
                  {entry.display_amount}
                </TableCell>
                <TableCell className="text-right">
                  {entry.debit_credit === 'DEBIT' && entry.usd_value
                    ? formatUSDValue(entry.usd_value)
                    : '-'}
                </TableCell>
                <TableCell className="text-right">
                  {entry.debit_credit === 'CREDIT' && entry.usd_value
                    ? formatUSDValue(entry.usd_value)
                    : '-'}
                </TableCell>
              </TableRow>
            ))}

            {/* Totals row */}
            <TableRow className="bg-muted/50 font-medium">
              <TableCell colSpan={4} className="text-right">
                Total
              </TableCell>
              <TableCell className="text-right">
                ${totals.debit.toFixed(2)}
              </TableCell>
              <TableCell className="text-right">
                ${totals.credit.toFixed(2)}
              </TableCell>
            </TableRow>
          </TableBody>
        </Table>
      </CardContent>
    </Card>
  )
}
