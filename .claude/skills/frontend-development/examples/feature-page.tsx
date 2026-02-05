/**
 * Example: Complete Feature Page Template
 *
 * This template demonstrates the standard structure for a feature page
 * in MoonTrack with loading states, error handling, and common patterns.
 */

import { Link } from 'react-router-dom'
import { Plus, ArrowLeft } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Skeleton } from '@/components/ui/skeleton'
import { useMyFeatureData } from '@/hooks/useMyFeature'
import type { MyItem } from '@/types/myFeature'

export default function MyFeaturePage() {
  const { data: items, isLoading, error, refetch } = useMyFeatureData()

  // Loading state
  if (isLoading) {
    return <PageSkeleton />
  }

  // Error state
  if (error) {
    return (
      <div className="flex flex-col items-center justify-center py-12">
        <p className="text-destructive mb-4">Failed to load data</p>
        <Button onClick={() => refetch()}>Try Again</Button>
      </div>
    )
  }

  return (
    <div className="space-y-6">
      {/* Page header */}
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-bold tracking-tight">My Feature</h1>
          <p className="text-muted-foreground">
            Description of what this page shows
          </p>
        </div>
        <Button>
          <Plus className="mr-2 h-4 w-4" />
          Add New
        </Button>
      </div>

      {/* Content */}
      {items && items.length > 0 ? (
        <div className="grid gap-4 md:grid-cols-2 lg:grid-cols-3">
          {items.map((item) => (
            <ItemCard key={item.id} item={item} />
          ))}
        </div>
      ) : (
        <EmptyState />
      )}
    </div>
  )
}

// Item card component
interface ItemCardProps {
  item: MyItem
}

function ItemCard({ item }: ItemCardProps) {
  return (
    <Link to={`/my-feature/${item.id}`}>
      <Card className="transition-colors hover:border-border-hover cursor-pointer">
        <CardHeader className="pb-2">
          <CardTitle className="text-base">{item.name}</CardTitle>
        </CardHeader>
        <CardContent>
          <p className="text-sm text-muted-foreground line-clamp-2">
            {item.description}
          </p>
          <div className="mt-4 flex items-center justify-between">
            <span className="text-2xl font-semibold">{item.value}</span>
            <span className="text-sm text-muted-foreground">{item.status}</span>
          </div>
        </CardContent>
      </Card>
    </Link>
  )
}

// Empty state component
function EmptyState() {
  return (
    <Card>
      <CardContent className="flex flex-col items-center justify-center py-12">
        <p className="text-lg font-medium">No items yet</p>
        <p className="text-muted-foreground mt-1">
          Create your first item to get started
        </p>
        <Button className="mt-4">
          <Plus className="mr-2 h-4 w-4" />
          Create Item
        </Button>
      </CardContent>
    </Card>
  )
}

// Loading skeleton
function PageSkeleton() {
  return (
    <div className="space-y-6">
      {/* Header skeleton */}
      <div className="flex items-center justify-between">
        <div>
          <Skeleton className="h-8 w-48" />
          <Skeleton className="h-4 w-64 mt-2" />
        </div>
        <Skeleton className="h-10 w-28" />
      </div>

      {/* Cards skeleton */}
      <div className="grid gap-4 md:grid-cols-2 lg:grid-cols-3">
        {Array.from({ length: 6 }).map((_, i) => (
          <Card key={i}>
            <CardHeader className="pb-2">
              <Skeleton className="h-5 w-32" />
            </CardHeader>
            <CardContent>
              <Skeleton className="h-4 w-full" />
              <Skeleton className="h-4 w-3/4 mt-1" />
              <div className="mt-4 flex items-center justify-between">
                <Skeleton className="h-8 w-24" />
                <Skeleton className="h-4 w-16" />
              </div>
            </CardContent>
          </Card>
        ))}
      </div>
    </div>
  )
}

/**
 * Example: Detail Page with Back Button
 */
export function MyFeatureDetailPage() {
  // const { id } = useParams()
  // const { data: item, isLoading } = useMyFeatureItem(id)

  return (
    <div className="space-y-6">
      {/* Back button */}
      <Button asChild variant="ghost" size="sm" className="-ml-2">
        <Link to="/my-feature">
          <ArrowLeft className="mr-2 h-4 w-4" />
          Back to list
        </Link>
      </Button>

      {/* Page content */}
      <div>
        <h1 className="text-2xl font-bold tracking-tight">Item Detail</h1>
        <p className="text-muted-foreground">View and manage this item</p>
      </div>

      {/* Detail cards */}
      <div className="grid gap-6 md:grid-cols-2">
        <Card>
          <CardHeader>
            <CardTitle className="text-base">Overview</CardTitle>
          </CardHeader>
          <CardContent>
            {/* Detail content */}
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle className="text-base">Actions</CardTitle>
          </CardHeader>
          <CardContent className="space-y-2">
            <Button className="w-full">Primary Action</Button>
            <Button variant="outline" className="w-full">
              Secondary Action
            </Button>
            <Button variant="destructive" className="w-full">
              Delete
            </Button>
          </CardContent>
        </Card>
      </div>
    </div>
  )
}
