import { useQuery } from '@tanstack/react-query'
import { listLendingPositions } from '@/services/lendingPosition'
import type { LendingPosition } from '@/types/lendingPosition'

export function useLendingPositions(walletId: string, status?: string) {
  return useQuery<LendingPosition[]>({
    queryKey: ['lending-positions', walletId, status],
    queryFn: () => listLendingPositions(walletId, status),
    staleTime: 1000 * 60 * 2,
    enabled: !!walletId,
  })
}
