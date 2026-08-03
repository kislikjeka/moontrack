import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from '@/components/ui/tooltip'
import { EM_DASH, formatUSD } from '@/lib/format'
import type { PriceStatus } from '@/types/taxlot'

interface CostBasisCellProps {
  /** The money value, or null when the backend could not price it. */
  value: string | null
  /** Why the value is missing, when it is. */
  status?: PriceStatus
  /** Whether the value came from a user override rather than from pricing. */
  isOverridden?: boolean
}

const STATUS_EXPLANATION: Record<PriceStatus, string> = {
  resolved: '',
  pending: 'Price not fetched yet — this lot will be priced on the next backfill.',
  unpriceable: 'No price source for this asset — the cost basis cannot be determined.',
}

/**
 * The per-unit cost basis of a lot, rendered so that "unknown" cannot be misread
 * as "zero" (#79).
 *
 * A `null` cost basis used to reach `formatUSD` and come back as `$0.00`, so a
 * lot the backend had failed to price sat in the table next to genuinely priced
 * lots claiming it had been acquired for nothing. It now renders as a muted em
 * dash — the same treatment the WAC column in WalletHoldings already gave an
 * absent weighted average — with a tooltip naming which kind of "unknown" it is,
 * since "not yet" and "never" call for different user action.
 *
 * Both the lot table and the transaction lot-impact section render this column,
 * and the duplicated cell is what let the two drift apart.
 */
export function CostBasisCell({ value, status, isOverridden }: CostBasisCellProps) {
  const known = value !== null && value !== undefined && value !== ''

  if (known) {
    return (
      <>
        <span className="font-mono">{formatUSD(value)}</span>
        {isOverridden && (
          <span className="ml-1 text-xs text-muted-foreground">(override)</span>
        )}
      </>
    )
  }

  const explanation = status ? STATUS_EXPLANATION[status] : ''
  const dash = <span className="text-muted-foreground">{EM_DASH}</span>

  if (!explanation) return dash

  return (
    <TooltipProvider delayDuration={200}>
      <Tooltip>
        <TooltipTrigger asChild>
          <span className="text-muted-foreground cursor-help border-b border-dotted border-muted-foreground/50">
            {EM_DASH}
          </span>
        </TooltipTrigger>
        <TooltipContent>
          <p className="max-w-56">{explanation}</p>
        </TooltipContent>
      </Tooltip>
    </TooltipProvider>
  )
}
