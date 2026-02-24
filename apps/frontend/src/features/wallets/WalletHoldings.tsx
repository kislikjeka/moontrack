import { useState } from 'react'
import { ChevronDown, ChevronRight } from 'lucide-react'
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
import type { HoldingGroup, ChainHolding } from '@/types/portfolio'

interface WalletHoldingsProps {
  walletId: string
  holdings: HoldingGroup[]
}

export function WalletHoldings({ walletId, holdings }: WalletHoldingsProps) {
  const [expandedAssets, setExpandedAssets] = useState<Set<string>>(new Set())
  const [expandedChains, setExpandedChains] = useState<Set<string>>(new Set())

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

  if (!holdings || holdings.length === 0) {
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
          Holdings ({holdings.length})
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
            {holdings.map((group) => {
              const isAssetExpanded = expandedAssets.has(group.asset_id)

              return (
                <AssetGroupRows
                  key={group.asset_id}
                  group={group}
                  walletId={walletId}
                  isExpanded={isAssetExpanded}
                  expandedChains={expandedChains}
                  onToggleAsset={() => toggleAsset(group.asset_id)}
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
  group: HoldingGroup
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
            <AssetIcon symbol={group.asset_id} size="sm" />
            <span className="font-medium">{group.asset_id}</span>
          </div>
        </TableCell>
        <TableCell className="text-right font-mono">
          {formatCrypto(group.total_amount)}
        </TableCell>
        <TableCell className="text-right">
          {formatUSD(group.price)}
        </TableCell>
        <TableCell className="text-right font-medium">
          {formatUSD(group.total_usd_value)}
        </TableCell>
        <TableCell className="text-right">
          {group.aggregated_wac ? (
            <span className="font-mono">{formatUSD(group.aggregated_wac)}</span>
          ) : (
            <span className="text-muted-foreground">&mdash;</span>
          )}
        </TableCell>
      </TableRow>

      {/* Level 2: Chain breakdowns */}
      {isExpanded &&
        group.chains.map((chain) => {
          const chainKey = `${group.asset_id}:${chain.chain_id}`
          const isChainExpanded = expandedChains.has(chainKey)
          const ChainChevron = isChainExpanded ? ChevronDown : ChevronRight

          return (
            <ChainRows
              key={chainKey}
              chain={chain}
              assetId={group.asset_id}
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
  chain: ChainHolding
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
            <ChainIcon chainId={chain.chain_id} size="sm" showTooltip />
            <span className="text-sm">{getChainName(chain.chain_id)}</span>
          </div>
        </TableCell>
        <TableCell className="text-right font-mono">
          {formatCrypto(chain.amount)}
        </TableCell>
        {/* No Price column at chain level */}
        <TableCell />
        <TableCell className="text-right font-medium">
          {formatUSD(chain.usd_value)}
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
              <LotDetailTable walletId={walletId} asset={assetId} chainId={chain.chain_id} />
            </div>
          </TableCell>
        </TableRow>
      )}
    </>
  )
}
