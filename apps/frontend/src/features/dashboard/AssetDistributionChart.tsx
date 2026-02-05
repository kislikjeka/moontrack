import { PieChart, Pie, Cell, ResponsiveContainer, Tooltip } from 'recharts'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import type { AssetHolding } from '@/types/portfolio'

interface AssetDistributionChartProps {
  holdings: AssetHolding[]
}

// Chart colors for different assets
const COLORS = [
  'hsl(174, 72%, 40%)', // primary
  'hsl(142, 76%, 36%)', // profit
  'hsl(270, 60%, 55%)', // liquidity
  'hsl(38, 92%, 45%)', // gm-pool
  'hsl(0, 84%, 50%)', // loss
  'hsl(215, 16%, 47%)', // transfer
  'hsl(200, 60%, 50%)',
  'hsl(340, 70%, 50%)',
]

// Format big.Int string to USD value (scaled by 10^8)
function formatAssetUSD(value: string): string {
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

export function AssetDistributionChart({ holdings }: AssetDistributionChartProps) {
  if (!holdings || holdings.length === 0) {
    return (
      <Card>
        <CardHeader>
          <CardTitle className="text-base font-medium">Asset Distribution</CardTitle>
        </CardHeader>
        <CardContent>
          <div className="flex flex-col items-center justify-center py-8 text-muted-foreground">
            <p>No assets to display</p>
            <p className="text-sm">Add transactions to see your portfolio distribution</p>
          </div>
        </CardContent>
      </Card>
    )
  }

  // Transform holdings for the chart
  const chartData = holdings.map((holding, index) => ({
    name: holding.asset_id, // Could be improved with asset name lookup
    value: Number(BigInt(holding.usd_value)) / 100000000,
    color: COLORS[index % COLORS.length],
    originalValue: holding.usd_value,
    amount: holding.total_amount,
  }))

  // Calculate total for percentages
  const total = chartData.reduce((sum, item) => sum + item.value, 0)

  return (
    <Card>
      <CardHeader>
        <CardTitle className="text-base font-medium">Asset Distribution</CardTitle>
      </CardHeader>
      <CardContent>
        <div className="flex flex-col lg:flex-row items-center gap-4">
          {/* Chart */}
          <div className="h-48 w-48 flex-shrink-0">
            <ResponsiveContainer width="100%" height="100%">
              <PieChart>
                <Pie
                  data={chartData}
                  cx="50%"
                  cy="50%"
                  innerRadius={50}
                  outerRadius={70}
                  paddingAngle={2}
                  dataKey="value"
                >
                  {chartData.map((entry, index) => (
                    <Cell key={`cell-${index}`} fill={entry.color} />
                  ))}
                </Pie>
                <Tooltip
                  content={({ active, payload }) => {
                    if (active && payload && payload.length) {
                      const data = payload[0].payload
                      const percentage = total > 0 ? ((data.value / total) * 100).toFixed(1) : '0'
                      return (
                        <div className="rounded-lg border border-border bg-background p-2 text-sm shadow-sm">
                          <p className="font-medium">{data.name}</p>
                          <p className="text-muted-foreground">
                            {formatAssetUSD(data.originalValue)} ({percentage}%)
                          </p>
                        </div>
                      )
                    }
                    return null
                  }}
                />
              </PieChart>
            </ResponsiveContainer>
          </div>

          {/* Legend */}
          <div className="flex-1 space-y-2 w-full">
            {chartData.slice(0, 5).map((item, index) => {
              const percentage = total > 0 ? ((item.value / total) * 100).toFixed(1) : '0'
              return (
                <div key={index} className="flex items-center justify-between">
                  <div className="flex items-center gap-2">
                    <div
                      className="h-3 w-3 rounded-full"
                      style={{ backgroundColor: item.color }}
                    />
                    <span className="text-sm font-medium">{item.name}</span>
                  </div>
                  <div className="text-right">
                    <span className="text-sm text-muted-foreground">{percentage}%</span>
                  </div>
                </div>
              )
            })}
            {holdings.length > 5 && (
              <p className="text-sm text-muted-foreground pt-2">
                +{holdings.length - 5} more assets
              </p>
            )}
          </div>
        </div>
      </CardContent>
    </Card>
  )
}
