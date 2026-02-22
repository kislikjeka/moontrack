import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { taxlotService } from '@/services/taxlot'
import type { TaxLot, PositionWAC, OverrideCostBasisRequest, TransactionLotImpact } from '@/types/taxlot'

export function useTaxLots(walletId: string, asset: string) {
  return useQuery<TaxLot[]>({
    queryKey: ['tax-lots', walletId, asset],
    queryFn: () => taxlotService.getLots(walletId, asset),
    enabled: !!walletId && !!asset,
  })
}

export function usePositionWAC(walletId?: string) {
  return useQuery<PositionWAC[]>({
    queryKey: ['position-wac', walletId],
    queryFn: () => taxlotService.getWAC(walletId),
  })
}

export function useTransactionLots(transactionId: string) {
  return useQuery<TransactionLotImpact>({
    queryKey: ['transaction-lots', transactionId],
    queryFn: () => taxlotService.getTransactionLots(transactionId),
    enabled: !!transactionId,
  })
}

export function useOverrideCostBasis() {
  const queryClient = useQueryClient()

  return useMutation({
    mutationFn: ({ lotId, data }: { lotId: string; data: OverrideCostBasisRequest }) =>
      taxlotService.overrideCostBasis(lotId, data),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['tax-lots'] })
      queryClient.invalidateQueries({ queryKey: ['position-wac'] })
      queryClient.invalidateQueries({ queryKey: ['portfolio'] })
      queryClient.invalidateQueries({ queryKey: ['transaction-lots'] })
    },
  })
}
