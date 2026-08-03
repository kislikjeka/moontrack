import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { formatRelativeDate, formatUSD } from '@/lib/format'
import type { PortfolioSummary as PortfolioSummaryType } from '@/types/portfolio'

interface PortfolioSummaryProps {
  portfolio?: PortfolioSummaryType
}

/**
 * Says that the total above was computed over an incomplete set of prices (#79).
 *
 * The backend counts the unpriced lots and the wire carries the counts; without
 * this the user reads a confidently-rendered total that silently omits them —
 * the same "absence presented as fact" the dash fixes one level down.
 *
 * Rendered from the counts rather than from `pnl_is_partial`: the backend
 * derives that flag from pending lots alone, so it is false for a portfolio
 * whose lots are all permanently unpriceable — the case that most needs saying,
 * since it never resolves on its own.
 */
function PartialTotalNotice({ portfolio }: { portfolio: PortfolioSummaryType }) {
  const pending = portfolio.pending_lot_count ?? 0
  const unpriceable = portfolio.unpriceable_lot_count ?? 0
  const total = pending + unpriceable

  if (total === 0) return null

  // The two causes call for different user action, so they are named separately
  // rather than summed into one opaque number.
  const causes = [
    pending > 0 ? `${pending} awaiting pricing` : null,
    unpriceable > 0 ? `${unpriceable} with no price source` : null,
  ].filter(Boolean)

  return (
    <p className="text-xs text-muted-foreground pt-1">
      Partial: excludes {total} {total === 1 ? 'lot' : 'lots'} ({causes.join(', ')})
    </p>
  )
}

export function PortfolioSummary({ portfolio }: PortfolioSummaryProps) {
  if (!portfolio) {
    return (
      <Card>
        <CardHeader>
          <CardTitle>Portfolio Summary</CardTitle>
        </CardHeader>
        <CardContent>
          <p className="text-muted-foreground">No portfolio data available</p>
        </CardContent>
      </Card>
    )
  }

  return (
    <Card>
      <CardHeader className="pb-2">
        <CardTitle className="text-base font-medium">Portfolio Summary</CardTitle>
      </CardHeader>
      <CardContent>
        <div className="space-y-4">
          {/* Total value */}
          <div>
            <p className="text-sm text-muted-foreground">Total Value</p>
            <p className="text-3xl font-bold tracking-tight">
              {formatUSD(portfolio.total_usd_value)}
            </p>
            <PartialTotalNotice portfolio={portfolio} />
          </div>

          {/* Stats */}
          <div className="grid grid-cols-2 gap-4 pt-2">
            <div>
              <p className="text-sm text-muted-foreground">Assets</p>
              <p className="text-lg font-semibold">{portfolio.total_assets}</p>
            </div>
            <div>
              <p className="text-sm text-muted-foreground">Wallets</p>
              <p className="text-lg font-semibold">
                {portfolio.wallet_balances.length}
              </p>
            </div>
          </div>

          {/* Last updated */}
          {portfolio.last_updated && (
            <p className="text-xs text-muted-foreground pt-2">
              Last updated {formatRelativeDate(portfolio.last_updated)}
            </p>
          )}
        </div>
      </CardContent>
    </Card>
  )
}
