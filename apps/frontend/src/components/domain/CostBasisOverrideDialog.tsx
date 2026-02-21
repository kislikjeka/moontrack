import { useState } from 'react'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Textarea } from '@/components/ui/textarea'
import { useOverrideCostBasis } from '@/hooks/useTaxLots'
import { toast } from 'sonner'
import type { TaxLot } from '@/types/taxlot'

interface CostBasisOverrideDialogProps {
  lot: TaxLot | null
  open: boolean
  onOpenChange: (open: boolean) => void
}

export function CostBasisOverrideDialog({
  lot,
  open,
  onOpenChange,
}: CostBasisOverrideDialogProps) {
  const [costBasis, setCostBasis] = useState('')
  const [reason, setReason] = useState('')
  const override = useOverrideCostBasis()

  const handleOpen = (isOpen: boolean) => {
    if (isOpen && lot) {
      setCostBasis(lot.effective_cost_basis_per_unit)
      setReason('')
    }
    onOpenChange(isOpen)
  }

  const isValidNumber = (v: string) => /^\d+(\.\d{0,8})?$/.test(v)

  const handleSubmit = () => {
    if (!lot || !isValidNumber(costBasis) || !reason.trim()) return

    override.mutate(
      { lotId: lot.id, data: { cost_basis_per_unit: costBasis, reason: reason.trim() } },
      {
        onSuccess: () => {
          toast.success('Cost basis override applied')
          onOpenChange(false)
        },
        onError: () => {
          toast.error('Failed to apply cost basis override')
        },
      }
    )
  }

  if (!lot) return null

  return (
    <Dialog open={open} onOpenChange={handleOpen}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>Override Cost Basis</DialogTitle>
          <DialogDescription>
            Set a manual cost basis for this {lot.asset} lot acquired on{' '}
            {new Date(lot.acquired_at).toLocaleDateString()}.
          </DialogDescription>
        </DialogHeader>

        <div className="space-y-4 py-4">
          <div className="grid grid-cols-2 gap-4 text-sm">
            <div>
              <p className="text-muted-foreground">Quantity Acquired</p>
              <p className="font-mono">{lot.quantity_acquired}</p>
            </div>
            <div>
              <p className="text-muted-foreground">Remaining</p>
              <p className="font-mono">{lot.quantity_remaining}</p>
            </div>
            <div>
              <p className="text-muted-foreground">Auto Cost Basis</p>
              <p className="font-mono">${lot.auto_cost_basis_per_unit}</p>
            </div>
            <div>
              <p className="text-muted-foreground">Source</p>
              <p>{formatSource(lot.auto_cost_basis_source)}</p>
            </div>
          </div>

          <div className="space-y-2">
            <Label htmlFor="cost-basis">New Cost Basis (USD per unit)</Label>
            <Input
              id="cost-basis"
              type="text"
              inputMode="decimal"
              placeholder="0.00"
              value={costBasis}
              onChange={(e) => setCostBasis(e.target.value)}
            />
          </div>

          <div className="space-y-2">
            <Label htmlFor="reason">Reason (required)</Label>
            <Textarea
              id="reason"
              placeholder="Explain why the cost basis is being overridden..."
              value={reason}
              onChange={(e) => setReason(e.target.value)}
              rows={3}
            />
          </div>
        </div>

        <DialogFooter>
          <Button variant="outline" onClick={() => handleOpen(false)}>
            Cancel
          </Button>
          <Button
            onClick={handleSubmit}
            disabled={!isValidNumber(costBasis) || !reason.trim() || override.isPending}
          >
            {override.isPending ? 'Saving...' : 'Apply Override'}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

function formatSource(source: string): string {
  const labels: Record<string, string> = {
    swap_price: 'Swap',
    fmv_at_transfer: 'FMV',
    linked_transfer: 'Linked',
    genesis_approximation: 'Genesis',
  }
  return labels[source] || source
}
