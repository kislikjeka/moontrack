import type { LucideIcon } from 'lucide-react'
import { Card, CardContent } from '@/components/ui/card'
import { cn } from '@/lib/utils'

interface StatCardProps {
  label: string
  value: string | number
  icon: LucideIcon
  iconColor?: 'primary' | 'profit' | 'loss' | 'muted'
  change?: {
    value: number
    isPercent?: boolean
  }
  className?: string
}

const iconColorClasses = {
  primary: 'text-primary',
  profit: 'text-profit',
  loss: 'text-loss',
  muted: 'text-muted-foreground',
}

export function StatCard({
  label,
  value,
  icon: Icon,
  iconColor = 'primary',
  change,
  className,
}: StatCardProps) {
  return (
    <Card className={cn('', className)}>
      <CardContent className="p-6">
        <div className="flex items-start justify-between">
          <div className="space-y-1">
            <p className="text-sm text-muted-foreground">{label}</p>
            <p className="text-2xl font-semibold tracking-tight">{value}</p>
            {change && (
              <p
                className={cn(
                  'text-sm',
                  change.value >= 0 ? 'text-profit' : 'text-loss'
                )}
              >
                {change.value >= 0 ? '+' : ''}
                {change.isPercent
                  ? `${change.value.toFixed(2)}%`
                  : change.value.toLocaleString()}
              </p>
            )}
          </div>
          <div
            className={cn(
              'rounded-lg bg-background-subtle p-3',
              iconColorClasses[iconColor]
            )}
          >
            <Icon className="h-5 w-5" />
          </div>
        </div>
      </CardContent>
    </Card>
  )
}
