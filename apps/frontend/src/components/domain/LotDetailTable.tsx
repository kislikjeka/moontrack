import { useState } from 'react'
import { Pencil } from 'lucide-react'
import { useTaxLots } from '@/hooks/useTaxLots'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { Skeleton } from '@/components/ui/skeleton'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { CostBasisOverrideDialog } from './CostBasisOverrideDialog'
import { formatDate, formatUSD } from '@/lib/format'
import type { TaxLot, CostBasisSource } from '@/types/taxlot'

interface LotDetailTableProps {
  walletId: string
  asset: string
  chainId?: string
}

const sourceBadgeVariants: Record<CostBasisSource, { label: string; variant: 'default' | 'secondary' | 'outline' | 'destructive' }> = {
  swap_price: { label: 'Swap', variant: 'default' },
  fmv_at_transfer: { label: 'FMV', variant: 'secondary' },
  linked_transfer: { label: 'Linked', variant: 'outline' },
  genesis_approximation: { label: 'Genesis', variant: 'destructive' },
}

export function LotDetailTable({ walletId, asset, chainId }: LotDetailTableProps) {
  const { data: lots, isLoading } = useTaxLots(walletId, asset, chainId)
  const [selectedLot, setSelectedLot] = useState<TaxLot | null>(null)
  const [dialogOpen, setDialogOpen] = useState(false)

  const handleEditClick = (lot: TaxLot) => {
    setSelectedLot(lot)
    setDialogOpen(true)
  }

  if (isLoading) {
    return (
      <div className="space-y-2 p-4">
        <Skeleton className="h-8 w-full" />
        <Skeleton className="h-8 w-full" />
        <Skeleton className="h-8 w-full" />
      </div>
    )
  }

  if (!lots || lots.length === 0) {
    return (
      <p className="text-sm text-muted-foreground p-4">No lots found for this asset.</p>
    )
  }

  return (
    <>
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead className="w-12">#</TableHead>
            <TableHead>Acquired</TableHead>
            {!chainId && <TableHead>Chain</TableHead>}
            <TableHead className="text-right">Qty Acquired</TableHead>
            <TableHead className="text-right">Remaining</TableHead>
            <TableHead className="text-right">Cost/Unit</TableHead>
            <TableHead>Source</TableHead>
            <TableHead className="w-16" />
          </TableRow>
        </TableHeader>
        <TableBody>
          {lots.map((lot, index) => {
            const source = sourceBadgeVariants[lot.auto_cost_basis_source] || {
              label: lot.auto_cost_basis_source,
              variant: 'outline' as const,
            }

            return (
              <TableRow key={lot.id}>
                <TableCell className="text-muted-foreground">{index + 1}</TableCell>
                <TableCell>{formatDate(lot.acquired_at)}</TableCell>
                {!chainId && <TableCell className="capitalize text-muted-foreground">{lot.chain_id || '—'}</TableCell>}
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

      <CostBasisOverrideDialog
        lot={selectedLot}
        open={dialogOpen}
        onOpenChange={setDialogOpen}
      />
    </>
  )
}
