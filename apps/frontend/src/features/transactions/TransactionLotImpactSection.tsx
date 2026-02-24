import { useState } from 'react'
import { Pencil } from 'lucide-react'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { CostBasisOverrideDialog } from '@/components/domain/CostBasisOverrideDialog'
import { useTransactionLots } from '@/hooks/useTaxLots'
import { formatDate, formatUSD } from '@/lib/format'
import type { TaxLot, CostBasisSource } from '@/types/taxlot'

interface TransactionLotImpactSectionProps {
  transactionId: string
}

const sourceBadgeVariants: Record<CostBasisSource, { label: string; variant: 'default' | 'secondary' | 'outline' | 'destructive' }> = {
  swap_price: { label: 'Swap', variant: 'default' },
  fmv_at_transfer: { label: 'FMV', variant: 'secondary' },
  linked_transfer: { label: 'Linked', variant: 'outline' },
  genesis_approximation: { label: 'Genesis', variant: 'destructive' },
}

export function TransactionLotImpactSection({ transactionId }: TransactionLotImpactSectionProps) {
  const { data, isLoading } = useTransactionLots(transactionId)
  const [selectedLot, setSelectedLot] = useState<TaxLot | null>(null)
  const [dialogOpen, setDialogOpen] = useState(false)

  if (isLoading || !data?.has_lot_impact) {
    return null
  }

  const handleEditClick = (lot: TaxLot) => {
    setSelectedLot(lot)
    setDialogOpen(true)
  }

  return (
    <>
      {data.acquired_lots.length > 0 && (
        <Card>
          <CardHeader>
            <CardTitle className="text-base font-medium">Acquired Lots</CardTitle>
          </CardHeader>
          <CardContent>
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead className="w-12">#</TableHead>
                  <TableHead>Acquired</TableHead>
                  <TableHead className="text-right">Qty Acquired</TableHead>
                  <TableHead className="text-right">Remaining</TableHead>
                  <TableHead className="text-right">Cost/Unit</TableHead>
                  <TableHead>Source</TableHead>
                  <TableHead className="w-16" />
                </TableRow>
              </TableHeader>
              <TableBody>
                {data.acquired_lots.map((lot, index) => {
                  const source = sourceBadgeVariants[lot.auto_cost_basis_source] || {
                    label: lot.auto_cost_basis_source,
                    variant: 'outline' as const,
                  }

                  return (
                    <TableRow key={lot.id}>
                      <TableCell className="text-muted-foreground">{index + 1}</TableCell>
                      <TableCell>{formatDate(lot.acquired_at)}</TableCell>
                      <TableCell className="text-right font-mono">{lot.quantity_acquired}</TableCell>
                      <TableCell className="text-right font-mono">{lot.quantity_remaining}</TableCell>
                      <TableCell className="text-right font-mono">
                        {formatUSD(lot.effective_cost_basis_per_unit)}
                        {lot.override_cost_basis_per_unit && (
                          <span className="ml-1 text-xs text-muted-foreground">(override)</span>
                        )}
                      </TableCell>
                      <TableCell>
                        <Badge variant={source.variant}>{source.label}</Badge>
                      </TableCell>
                      <TableCell>
                        <Button
                          variant="ghost"
                          size="icon"
                          className="h-8 w-8"
                          aria-label={`Override cost basis for lot ${index + 1}`}
                          onClick={() => handleEditClick(lot)}
                        >
                          <Pencil className="h-3.5 w-3.5" />
                        </Button>
                      </TableCell>
                    </TableRow>
                  )
                })}
              </TableBody>
            </Table>
          </CardContent>
        </Card>
      )}

      {data.disposals.length > 0 && (
        <Card>
          <CardHeader>
            <CardTitle className="text-base font-medium">Consumed Lots (FIFO)</CardTitle>
          </CardHeader>
          <CardContent>
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Lot Acquired</TableHead>
                  <TableHead>Asset</TableHead>
                  <TableHead className="text-right">Qty Disposed</TableHead>
                  <TableHead className="text-right">Cost Basis</TableHead>
                  <TableHead className="text-right">Proceeds/Unit</TableHead>
                  <TableHead className="text-right">Gain/Loss</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {data.disposals.map((disposal) => {
                  const isPositive = !disposal.realized_gain_loss.startsWith('-')

                  return (
                    <TableRow key={disposal.id}>
                      <TableCell>{formatDate(disposal.lot_acquired_at)}</TableCell>
                      <TableCell>{disposal.lot_asset}</TableCell>
                      <TableCell className="text-right font-mono">{disposal.quantity_disposed}</TableCell>
                      <TableCell className="text-right font-mono">{formatUSD(disposal.lot_cost_basis_per_unit)}</TableCell>
                      <TableCell className="text-right font-mono">{formatUSD(disposal.proceeds_per_unit)}</TableCell>
                      <TableCell className={`text-right font-mono ${isPositive ? 'text-green-600' : 'text-red-600'}`}>
                        {isPositive ? '+' : ''}{formatUSD(disposal.realized_gain_loss)}
                      </TableCell>
                    </TableRow>
                  )
                })}
              </TableBody>
            </Table>
          </CardContent>
        </Card>
      )}

      <CostBasisOverrideDialog
        lot={selectedLot}
        open={dialogOpen}
        onOpenChange={setDialogOpen}
      />
    </>
  )
}
