import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { AssetIcon } from '@/components/domain/AssetIcon'
import type { AssetBalance } from '@/types/portfolio'

interface WalletAssetsProps {
  assets: AssetBalance[]
}

// Format big.Int string to number (scaled by 10^8)
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

// Format amount with appropriate decimals
function formatAmount(value: string): string {
  try {
    const num = parseFloat(value)
    if (num < 0.001 && num > 0) {
      return num.toExponential(2)
    }
    return new Intl.NumberFormat('en-US', {
      minimumFractionDigits: 0,
      maximumFractionDigits: 6,
    }).format(num)
  } catch {
    return '0'
  }
}

export function WalletAssets({ assets }: WalletAssetsProps) {
  if (!assets || assets.length === 0) {
    return (
      <Card>
        <CardHeader>
          <CardTitle className="text-base font-medium">Assets</CardTitle>
        </CardHeader>
        <CardContent>
          <div className="flex flex-col items-center justify-center py-8 text-muted-foreground">
            <p>No assets in this wallet</p>
            <p className="text-sm">Add a transaction to start tracking assets</p>
          </div>
        </CardContent>
      </Card>
    )
  }

  return (
    <Card>
      <CardHeader>
        <CardTitle className="text-base font-medium">Assets</CardTitle>
      </CardHeader>
      <CardContent>
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>Asset</TableHead>
              <TableHead className="text-right">Amount</TableHead>
              <TableHead className="text-right">Price</TableHead>
              <TableHead className="text-right">Value</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {assets.map((asset) => (
              <TableRow key={asset.asset_id}>
                <TableCell>
                  <div className="flex items-center gap-2">
                    <AssetIcon symbol={asset.asset_id} size="sm" />
                    <span className="font-medium">{asset.asset_id}</span>
                  </div>
                </TableCell>
                <TableCell className="text-right font-mono">
                  {formatAmount(asset.amount)}
                </TableCell>
                <TableCell className="text-right">
                  {formatUSDValue(asset.price)}
                </TableCell>
                <TableCell className="text-right font-medium">
                  {formatUSDValue(asset.usd_value)}
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </CardContent>
    </Card>
  )
}
