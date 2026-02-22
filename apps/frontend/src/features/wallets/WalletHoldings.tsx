import { useMemo, useState } from 'react'
import { ChevronDown, ChevronRight } from 'lucide-react'
import { usePositionWAC } from '@/hooks/useTaxLots'
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
import { ChainIcon } from '@/components/domain/ChainIcon'
import { LotDetailTable } from '@/components/domain/LotDetailTable'
import { formatUSD, formatCrypto } from '@/lib/format'
import { getChainName } from '@/types/wallet'
import type { AssetBalance } from '@/types/portfolio'

interface WalletHoldingsProps {
  walletId: string
  assets: AssetBalance[]
}

interface ChainBreakdown {
  chainId: string
  amount: number
  value: number
  wac: string | null
}

interface AssetGroup {
  assetId: string
  totalAmount: number
  totalValue: number
  price: number
  aggregatedWAC: string | null
  chains: ChainBreakdown[]
}

export function WalletHoldings({ walletId, assets }: WalletHoldingsProps) {
  const { data: positions } = usePositionWAC(walletId)
  const [expandedAssets, setExpandedAssets] = useState<Set<string>>(new Set())
  const [expandedChains, setExpandedChains] = useState<Set<string>>(new Set())

  const groups = useMemo<AssetGroup[]>(() => {
    // Group assets by asset_id
    const groupMap = new Map<string, { entries: AssetBalance[] }>()

    for (const asset of assets) {
      const existing = groupMap.get(asset.asset_id)
      if (existing) {
        existing.entries.push(asset)
      } else {
        groupMap.set(asset.asset_id, { entries: [asset] })
      }
    }

    return Array.from(groupMap.entries()).map(([assetId, { entries }]) => {
      const totalAmount = entries.reduce((sum, e) => sum + parseFloat(e.amount), 0)
      const totalValue = entries.reduce((sum, e) => sum + parseFloat(e.usd_value), 0)
      const price = parseFloat(entries[0].price)

      // Find aggregated WAC position for this asset
      const aggregatedPos = positions?.find(
        (p) => p.asset === assetId && p.is_aggregated === true
      )
      const aggregatedWAC = aggregatedPos?.weighted_avg_cost ?? null

      // Build per-chain breakdown
      const chains: ChainBreakdown[] = entries
        .filter((e) => e.chain_id)
        .map((e) => {
          const chainPos = positions?.find(
            (p) =>
              p.asset === assetId &&
              p.chain_id === e.chain_id &&
              !p.is_aggregated
          )

          return {
            chainId: e.chain_id!,
            amount: parseFloat(e.amount),
            value: parseFloat(e.usd_value),
            wac: chainPos?.weighted_avg_cost ?? null,
          }
        })

      return {
        assetId,
        totalAmount,
        totalValue,
        price,
        aggregatedWAC,
        chains,
      }
    })
  }, [assets, positions])

  const toggleAsset = (assetId: string) => {
    setExpandedAssets((prev) => {
      const next = new Set(prev)
      if (next.has(assetId)) {
        next.delete(assetId)
      } else {
        next.add(assetId)
      }
      return next
    })
  }

  const toggleChain = (key: string) => {
    setExpandedChains((prev) => {
      const next = new Set(prev)
      if (next.has(key)) {
        next.delete(key)
      } else {
        next.add(key)
      }
      return next
    })
  }

  // Unique asset count for the card title
  const uniqueAssetCount = groups.length

  if (!assets || assets.length === 0) {
    return (
      <Card>
        <CardHeader>
          <CardTitle className="text-base font-medium">Holdings</CardTitle>
        </CardHeader>
        <CardContent>
          <div className="flex flex-col items-center justify-center py-8 text-muted-foreground">
            <p>No holdings in this wallet</p>
          </div>
        </CardContent>
      </Card>
    )
  }

  return (
    <Card>
      <CardHeader>
        <CardTitle className="text-base font-medium">
          Holdings ({uniqueAssetCount})
        </CardTitle>
      </CardHeader>
      <CardContent>
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>Asset</TableHead>
              <TableHead className="text-right">Amount</TableHead>
              <TableHead className="text-right">Price</TableHead>
              <TableHead className="text-right">Value</TableHead>
              <TableHead className="text-right">WAC</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {groups.map((group) => {
              const isAssetExpanded = expandedAssets.has(group.assetId)

              return (
                <AssetGroupRows
                  key={group.assetId}
                  group={group}
                  walletId={walletId}
                  isExpanded={isAssetExpanded}
                  expandedChains={expandedChains}
                  onToggleAsset={() => toggleAsset(group.assetId)}
                  onToggleChain={toggleChain}
                />
              )
            })}
          </TableBody>
        </Table>
      </CardContent>
    </Card>
  )
}

interface AssetGroupRowsProps {
  group: AssetGroup
  walletId: string
  isExpanded: boolean
  expandedChains: Set<string>
  onToggleAsset: () => void
  onToggleChain: (key: string) => void
}

function AssetGroupRows({
  group,
  walletId,
  isExpanded,
  expandedChains,
  onToggleAsset,
  onToggleChain,
}: AssetGroupRowsProps) {
  const Chevron = isExpanded ? ChevronDown : ChevronRight

  return (
    <>
      {/* Level 1: Asset Group */}
      <TableRow
        className="cursor-pointer hover:bg-muted/50"
        onClick={onToggleAsset}
      >
        <TableCell>
          <div className="flex items-center gap-2">
            <Chevron className="h-4 w-4 text-muted-foreground flex-shrink-0" />
            <AssetIcon symbol={group.assetId} size="sm" />
            <span className="font-medium">{group.assetId}</span>
          </div>
        </TableCell>
        <TableCell className="text-right font-mono">
          {formatCrypto(group.totalAmount)}
        </TableCell>
        <TableCell className="text-right">
          {formatUSD(group.price)}
        </TableCell>
        <TableCell className="text-right font-medium">
          {formatUSD(group.totalValue)}
        </TableCell>
        <TableCell className="text-right">
          {group.aggregatedWAC ? (
            <span className="font-mono">{formatUSD(group.aggregatedWAC)}</span>
          ) : (
            <span className="text-muted-foreground">&mdash;</span>
          )}
        </TableCell>
      </TableRow>

      {/* Level 2: Chain breakdowns */}
      {isExpanded &&
        group.chains.map((chain) => {
          const chainKey = `${group.assetId}:${chain.chainId}`
          const isChainExpanded = expandedChains.has(chainKey)
          const ChainChevron = isChainExpanded ? ChevronDown : ChevronRight

          return (
            <ChainRows
              key={chainKey}
              chain={chain}
              chainKey={chainKey}
              assetId={group.assetId}
              walletId={walletId}
              isExpanded={isChainExpanded}
              ChevronIcon={ChainChevron}
              onToggle={() => onToggleChain(chainKey)}
            />
          )
        })}
    </>
  )
}

interface ChainRowsProps {
  chain: ChainBreakdown
  chainKey: string
  assetId: string
  walletId: string
  isExpanded: boolean
  ChevronIcon: typeof ChevronDown
  onToggle: () => void
}

function ChainRows({
  chain,
  assetId,
  walletId,
  isExpanded,
  ChevronIcon,
  onToggle,
}: ChainRowsProps) {
  return (
    <>
      {/* Level 2: Chain row */}
      <TableRow
        className="cursor-pointer hover:bg-muted/50"
        onClick={onToggle}
      >
        <TableCell className="pl-8">
          <div className="flex items-center gap-2">
            <ChevronIcon className="h-4 w-4 text-muted-foreground flex-shrink-0" />
            <ChainIcon chainId={chain.chainId} size="sm" showTooltip />
            <span className="text-sm">{getChainName(chain.chainId)}</span>
          </div>
        </TableCell>
        <TableCell className="text-right font-mono">
          {formatCrypto(chain.amount)}
        </TableCell>
        {/* No Price column at chain level */}
        <TableCell />
        <TableCell className="text-right font-medium">
          {formatUSD(chain.value)}
        </TableCell>
        <TableCell className="text-right">
          {chain.wac ? (
            <span className="font-mono">{formatUSD(chain.wac)}</span>
          ) : (
            <span className="text-muted-foreground">&mdash;</span>
          )}
        </TableCell>
      </TableRow>

      {/* Level 3: Tax Lots */}
      {isExpanded && (
        <TableRow>
          <TableCell colSpan={5} className="p-0">
            <div className="bg-muted/30 px-4 py-2">
              <LotDetailTable walletId={walletId} asset={assetId} />
            </div>
          </TableCell>
        </TableRow>
      )}
    </>
  )
}
