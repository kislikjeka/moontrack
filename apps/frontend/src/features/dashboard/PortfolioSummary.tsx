import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { formatRelativeDate, formatUSD } from '@/lib/format'
import type { PortfolioSummary as PortfolioSummaryType } from '@/types/portfolio'

interface PortfolioSummaryProps {
  portfolio?: PortfolioSummaryType
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
