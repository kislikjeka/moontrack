import { useState } from 'react'
import { Copy, Check } from 'lucide-react'
import { cn } from '@/lib/utils'
import { truncateAddress } from '@/lib/format'

interface AddressDisplayProps {
  address: string
  truncate?: boolean
  showCopy?: boolean
  className?: string
}

export function AddressDisplay({
  address,
  truncate = true,
  showCopy = true,
  className,
}: AddressDisplayProps) {
  const [copied, setCopied] = useState(false)

  const displayAddress = truncate ? truncateAddress(address) : address

  const handleCopy = async (e: React.MouseEvent) => {
    e.preventDefault()
    e.stopPropagation()

    try {
      await navigator.clipboard.writeText(address)
      setCopied(true)
      setTimeout(() => setCopied(false), 2000)
    } catch (err) {
      console.error('Failed to copy address:', err)
    }
  }

  return (
    <span
      className={cn(
        'inline-flex items-center gap-1.5 font-mono',
        showCopy && 'cursor-pointer hover:text-foreground',
        className
      )}
      onClick={showCopy ? handleCopy : undefined}
      title={address}
    >
      <span>{displayAddress}</span>
      {showCopy && (
        <span className="text-muted-foreground">
          {copied ? (
            <Check className="h-3 w-3 text-profit" />
          ) : (
            <Copy className="h-3 w-3" />
          )}
        </span>
      )}
    </span>
  )
}
