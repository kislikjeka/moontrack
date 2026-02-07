import { Clock, Loader2, CheckCircle, AlertCircle } from 'lucide-react'
import { Badge } from '@/components/ui/badge'
import { cn } from '@/lib/utils'
import type { WalletSyncStatus } from '@/types/wallet'

interface SyncStatusBadgeProps {
  status?: WalletSyncStatus
  className?: string
}

const statusConfig: Record<
  WalletSyncStatus,
  {
    label: string
    icon: React.ElementType
    variant: 'default' | 'secondary' | 'destructive' | 'outline'
    iconClassName?: string
  }
> = {
  pending: {
    label: 'Pending',
    icon: Clock,
    variant: 'outline',
  },
  syncing: {
    label: 'Syncing',
    icon: Loader2,
    variant: 'secondary',
    iconClassName: 'animate-spin',
  },
  synced: {
    label: 'Synced',
    icon: CheckCircle,
    variant: 'default',
  },
  error: {
    label: 'Error',
    icon: AlertCircle,
    variant: 'destructive',
  },
}

export function SyncStatusBadge({ status, className }: SyncStatusBadgeProps) {
  // Default to 'pending' if status is undefined (backend may not have sync fields yet)
  const effectiveStatus = status || 'pending'
  const config = statusConfig[effectiveStatus]
  const Icon = config.icon

  return (
    <Badge variant={config.variant} className={cn('gap-1', className)}>
      <Icon className={cn('h-3 w-3', config.iconClassName)} />
      <span>{config.label}</span>
    </Badge>
  )
}
