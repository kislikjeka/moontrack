import { useWallets } from '@/hooks/useWallets'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import type { TransactionFilters as FiltersType, TransactionType } from '@/types/transaction'

interface TransactionFiltersProps {
  filters: FiltersType
  onFiltersChange: (filters: FiltersType) => void
}

const transactionTypes: { value: TransactionType; label: string }[] = [
  { value: 'manual_income', label: 'Income' },
  { value: 'manual_outcome', label: 'Outcome' },
  { value: 'asset_adjustment', label: 'Adjustment' },
]

export function TransactionFilters({ filters, onFiltersChange }: TransactionFiltersProps) {
  const { data: wallets } = useWallets()

  const handleWalletChange = (value: string) => {
    onFiltersChange({
      ...filters,
      wallet_id: value === 'all' ? undefined : value,
      page: 1,
    })
  }

  const handleTypeChange = (value: string) => {
    onFiltersChange({
      ...filters,
      type: value === 'all' ? undefined : (value as TransactionType),
      page: 1,
    })
  }

  return (
    <div className="flex flex-wrap gap-4">
      {/* Wallet filter */}
      <Select
        value={filters.wallet_id || 'all'}
        onValueChange={handleWalletChange}
      >
        <SelectTrigger className="w-[180px]">
          <SelectValue placeholder="All wallets" />
        </SelectTrigger>
        <SelectContent>
          <SelectItem value="all">All wallets</SelectItem>
          {wallets?.map((wallet) => (
            <SelectItem key={wallet.id} value={wallet.id}>
              {wallet.name}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>

      {/* Type filter */}
      <Select
        value={filters.type || 'all'}
        onValueChange={handleTypeChange}
      >
        <SelectTrigger className="w-[180px]">
          <SelectValue placeholder="All types" />
        </SelectTrigger>
        <SelectContent>
          <SelectItem value="all">All types</SelectItem>
          {transactionTypes.map((type) => (
            <SelectItem key={type.value} value={type.value}>
              {type.label}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
    </div>
  )
}
