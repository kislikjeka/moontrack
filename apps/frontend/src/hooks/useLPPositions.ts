import { useQuery } from '@tanstack/react-query'
import { listLPPositions } from '@/services/lpPosition'
import type { LPPosition } from '@/types/lpPosition'

export function useLPPositions(walletId: string, status?: string) {
  return useQuery<LPPosition[]>({
    queryKey: ['lp-positions', walletId, status],
    queryFn: () => listLPPositions(walletId, status),
    staleTime: 1000 * 60 * 2, // 2 minutes
    enabled: !!walletId,
  })
}
