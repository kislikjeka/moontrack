import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import {
  getWallets,
  getWallet,
  createWallet,
  updateWallet,
  deleteWallet,
} from '@/services/wallet'
import type { Wallet, CreateWalletRequest, UpdateWalletRequest } from '@/types/wallet'

export function useWallets() {
  return useQuery<Wallet[]>({
    queryKey: ['wallets'],
    queryFn: getWallets,
  })
}

export function useWallet(id: string) {
  return useQuery<Wallet>({
    queryKey: ['wallets', id],
    queryFn: () => getWallet(id),
    enabled: !!id,
  })
}

export function useCreateWallet() {
  const queryClient = useQueryClient()

  return useMutation({
    mutationFn: (data: CreateWalletRequest) => createWallet(data),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['wallets'] })
      queryClient.invalidateQueries({ queryKey: ['portfolio'] })
    },
  })
}

export function useUpdateWallet() {
  const queryClient = useQueryClient()

  return useMutation({
    mutationFn: ({ id, data }: { id: string; data: UpdateWalletRequest }) =>
      updateWallet(id, data),
    onSuccess: (_data, variables) => {
      queryClient.invalidateQueries({ queryKey: ['wallets'] })
      queryClient.invalidateQueries({ queryKey: ['wallets', variables.id] })
    },
  })
}

export function useDeleteWallet() {
  const queryClient = useQueryClient()

  return useMutation({
    mutationFn: (id: string) => deleteWallet(id),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['wallets'] })
      queryClient.invalidateQueries({ queryKey: ['portfolio'] })
    },
  })
}
