import { useState } from 'react'
import { Droplets } from 'lucide-react'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Tabs, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { Skeleton } from '@/components/ui/skeleton'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { useLPPositions } from '@/hooks/useLPPositions'
import { LPPositionCard } from './LPPositionCard'

interface LPPositionsSectionProps {
  walletId: string
}

type StatusFilter = 'all' | 'open' | 'closed'

export function LPPositionsSection({ walletId }: LPPositionsSectionProps) {
  const [statusFilter, setStatusFilter] = useState<StatusFilter>('all')

  const apiStatus = statusFilter === 'all' ? undefined : statusFilter
  const { data: positions, isLoading, error } = useLPPositions(walletId, apiStatus)

  // Sort: open first, then closed; within each group by opened_at desc
  const sortedPositions = positions
    ? [...positions].sort((a, b) => {
        if (a.status !== b.status) {
          return a.status === 'open' ? -1 : 1
        }
        return new Date(b.opened_at).getTime() - new Date(a.opened_at).getTime()
      })
    : []

  return (
    <Card>
      <CardHeader className="pb-3">
        <div className="flex items-center justify-between">
          <CardTitle className="text-base font-medium flex items-center gap-2">
            LP Positions
            {positions && positions.length > 0 && (
              <span className="text-sm font-normal text-muted-foreground">
                ({positions.length})
              </span>
            )}
          </CardTitle>
          <Tabs
            value={statusFilter}
            onValueChange={(v) => setStatusFilter(v as StatusFilter)}
          >
            <TabsList className="h-8">
              <TabsTrigger value="all" className="text-xs px-2.5 h-6">All</TabsTrigger>
              <TabsTrigger value="open" className="text-xs px-2.5 h-6">Open</TabsTrigger>
              <TabsTrigger value="closed" className="text-xs px-2.5 h-6">Closed</TabsTrigger>
            </TabsList>
          </Tabs>
        </div>
      </CardHeader>
      <CardContent>
        {isLoading ? (
          <div className="space-y-3">
            {[...Array(2)].map((_, i) => (
              <Skeleton key={i} className="h-28" />
            ))}
          </div>
        ) : error ? (
          <Alert variant="destructive">
            <AlertDescription>Failed to load LP positions</AlertDescription>
          </Alert>
        ) : sortedPositions.length === 0 ? (
          <div className="flex flex-col items-center justify-center py-8 text-muted-foreground">
            <div className="rounded-full bg-muted p-3 mb-3">
              <Droplets className="h-6 w-6" />
            </div>
            <p>No LP positions found</p>
            <p className="text-sm">
              {statusFilter !== 'all'
                ? 'Try adjusting your filter'
                : 'LP positions will appear here once detected during sync'}
            </p>
          </div>
        ) : (
          <div className="space-y-3">
            {sortedPositions.map((position) => (
              <LPPositionCard key={position.id} position={position} />
            ))}
          </div>
        )}
      </CardContent>
    </Card>
  )
}
