import { useQuery } from '@tanstack/react-query'
import { assetService } from '@/services/asset'
import type { Asset } from '@/types/asset'

export function useAssetSearch(query: string) {
  return useQuery<Asset[]>({
    queryKey: ['assets', 'search', query],
    queryFn: async () => {
      if (query.length < 2) return []
      const result = await assetService.search(query)
      return result.assets
    },
    enabled: query.length >= 2,
    staleTime: 1000 * 60 * 5, // 5 minutes
  })
}

export function useAsset(id: string) {
  return useQuery<Asset>({
    queryKey: ['assets', id],
    queryFn: () => assetService.getAsset(id),
    enabled: !!id,
  })
}
