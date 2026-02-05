import { useQuery } from '@tanstack/react-query'
import { getPortfolioSummary } from '@/services/portfolio'
import type { PortfolioSummary } from '@/types/portfolio'

export function usePortfolio() {
  return useQuery<PortfolioSummary>({
    queryKey: ['portfolio'],
    queryFn: getPortfolioSummary,
    staleTime: 1000 * 60 * 2, // 2 minutes
  })
}
