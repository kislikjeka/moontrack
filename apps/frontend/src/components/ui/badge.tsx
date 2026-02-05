import * as React from 'react'
import { cva, type VariantProps } from 'class-variance-authority'
import { cn } from '@/lib/utils'

const badgeVariants = cva(
  'inline-flex items-center rounded-full border px-2.5 py-0.5 text-xs font-semibold transition-colors focus:outline-none focus:ring-2 focus:ring-ring focus:ring-offset-2',
  {
    variants: {
      variant: {
        default:
          'border-transparent bg-primary text-primary-foreground',
        secondary:
          'border-transparent bg-secondary text-secondary-foreground',
        destructive:
          'border-transparent bg-destructive text-destructive-foreground',
        outline: 'text-foreground',
        profit: 'border-transparent bg-profit-bg text-profit',
        loss: 'border-transparent bg-loss-bg text-loss',
        liquidity: 'border-transparent bg-tx-liquidity-bg text-tx-liquidity',
        gmPool: 'border-transparent bg-tx-gm-pool-bg text-tx-gm-pool',
        transfer: 'border-transparent bg-tx-transfer-bg text-tx-transfer',
        swap: 'border-transparent bg-tx-swap-bg text-tx-swap',
        bridge: 'border-transparent bg-tx-bridge-bg text-tx-bridge',
      },
    },
    defaultVariants: {
      variant: 'default',
    },
  }
)

export interface BadgeProps
  extends React.HTMLAttributes<HTMLDivElement>,
    VariantProps<typeof badgeVariants> {}

function Badge({ className, variant, ...props }: BadgeProps) {
  return (
    <div className={cn(badgeVariants({ variant }), className)} {...props} />
  )
}

export { Badge, badgeVariants }
