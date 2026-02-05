import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { transactionService } from '@/services/transaction'
import type {
  TransactionListResponse,
  TransactionDetail,
  CreateTransactionRequest,
  TransactionFilters,
} from '@/types/transaction'

export function useTransactions(filters?: TransactionFilters) {
  return useQuery<TransactionListResponse>({
    queryKey: ['transactions', filters],
    queryFn: () => transactionService.list(filters),
  })
}

export function useTransaction(id: string) {
  return useQuery<TransactionDetail>({
    queryKey: ['transactions', id],
    queryFn: () => transactionService.getById(id),
    enabled: !!id,
  })
}

export function useCreateTransaction() {
  const queryClient = useQueryClient()

  return useMutation({
    mutationFn: (data: CreateTransactionRequest) => transactionService.create(data),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['transactions'] })
      queryClient.invalidateQueries({ queryKey: ['portfolio'] })
      queryClient.invalidateQueries({ queryKey: ['wallets'] })
    },
  })
}
