import { useState } from 'react'
import { Calculator, ChevronDown, ChevronRight } from 'lucide-react'
import { useWallets } from '@/hooks/useWallets'
import { usePositionWAC } from '@/hooks/useTaxLots'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Skeleton } from '@/components/ui/skeleton'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { LotDetailTable } from '@/components/domain/LotDetailTable'
import { formatUSD, formatCrypto } from '@/lib/format'

export default function CostBasisPage() {
  const [selectedWalletId, setSelectedWalletId] = useState<string>('')
  const { data: wallets, isLoading: walletsLoading } = useWallets()
  const { data: positions, isLoading: positionsLoading } = usePositionWAC(
    selectedWalletId || undefined
  )
  const [expandedAssets, setExpandedAssets] = useState<Set<string>>(new Set())

  const isLoading = walletsLoading || positionsLoading

  const toggleAsset = (key: string) => {
    setExpandedAssets((prev) => {
      const next = new Set(prev)
      if (next.has(key)) {
        next.delete(key)
      } else {
        next.add(key)
      }
      return next
    })
  }

  if (isLoading) {
    return <CostBasisSkeleton />
  }

  return (
    <div className="space-y-6">
      {/* Page header */}
      <div className="flex flex-col gap-4 sm:flex-row sm:items-center sm:justify-between">
        <div>
          <h1 className="text-2xl font-bold tracking-tight">Cost Basis</h1>
          <p className="text-muted-foreground">
            View tax lots and weighted average cost for your holdings.
          </p>
        </div>

        {/* Wallet filter */}
        <Select
          value={selectedWalletId}
          onValueChange={(value) => {
            setSelectedWalletId(value === 'all' ? '' : value)
            setExpandedAssets(new Set())
          }}
        >
          <SelectTrigger className="w-[200px]">
            <SelectValue placeholder="All Wallets" />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="all">All Wallets</SelectItem>
            {wallets?.map((w) => (
              <SelectItem key={w.id} value={w.id}>
                {w.name}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      </div>

      {/* Positions table */}
      {positions && positions.length > 0 ? (
        <Card>
          <CardHeader className="pb-2">
            <CardTitle className="text-base font-medium">
              {positions.length} {positions.length === 1 ? 'Position' : 'Positions'}
            </CardTitle>
          </CardHeader>
          <CardContent>
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead className="w-8" />
                  <TableHead>Asset</TableHead>
                  <TableHead>Wallet</TableHead>
                  <TableHead className="text-right">Holdings</TableHead>
                  <TableHead className="text-right">WAC (USD)</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {positions.map((pos) => {
                  const key = `${pos.account_id}-${pos.asset}`
                  const isExpanded = expandedAssets.has(key)

                  return (
                    <PositionRow
                      key={key}
                      asset={pos.asset}
                      walletName={pos.wallet_name}
                      walletId={pos.wallet_id}
                      totalQuantity={pos.total_quantity}
                      weightedAvgCost={pos.weighted_avg_cost}
                      isExpanded={isExpanded}
                      onToggle={() => toggleAsset(key)}
                    />
                  )
                })}
              </TableBody>
            </Table>
          </CardContent>
        </Card>
      ) : (
        <EmptyState />
      )}
    </div>
  )
}

interface PositionRowProps {
  asset: string
  walletName: string
  walletId: string
  totalQuantity: string
  weightedAvgCost: string
  isExpanded: boolean
  onToggle: () => void
}

function PositionRow({
  asset,
  walletName,
  walletId,
  totalQuantity,
  weightedAvgCost,
  isExpanded,
  onToggle,
}: PositionRowProps) {
  return (
    <>
      <TableRow
        className="cursor-pointer hover:bg-muted/50"
        onClick={onToggle}
      >
        <TableCell>
          {isExpanded ? (
            <ChevronDown className="h-4 w-4 text-muted-foreground" />
          ) : (
            <ChevronRight className="h-4 w-4 text-muted-foreground" />
          )}
        </TableCell>
        <TableCell className="font-medium">{asset}</TableCell>
        <TableCell className="text-muted-foreground">{walletName}</TableCell>
        <TableCell className="text-right font-mono">
          {formatCrypto(totalQuantity)}
        </TableCell>
        <TableCell className="text-right font-mono">
          {formatUSD(weightedAvgCost)}
        </TableCell>
      </TableRow>
      {isExpanded && (
        <TableRow>
          <TableCell colSpan={5} className="p-0 border-b">
            <div className="bg-muted/30 px-4 py-2">
              <LotDetailTable walletId={walletId} asset={asset} />
            </div>
          </TableCell>
        </TableRow>
      )}
    </>
  )
}

function EmptyState() {
  return (
    <div className="flex flex-col items-center justify-center py-16 text-center">
      <div className="rounded-full bg-muted p-4 mb-4">
        <Calculator className="h-8 w-8 text-muted-foreground" />
      </div>
      <h3 className="text-lg font-medium">No cost basis data</h3>
      <p className="text-muted-foreground mt-1 max-w-sm">
        Cost basis tracking begins when transactions are recorded. Add a wallet and sync
        or create transactions to see tax lot data here.
      </p>
    </div>
  )
}

function CostBasisSkeleton() {
  return (
    <div className="space-y-6">
      <div className="flex flex-col gap-4 sm:flex-row sm:items-center sm:justify-between">
        <div className="space-y-2">
          <Skeleton className="h-8 w-32" />
          <Skeleton className="h-4 w-64" />
        </div>
        <Skeleton className="h-10 w-[200px]" />
      </div>
      <Skeleton className="h-96" />
    </div>
  )
}
