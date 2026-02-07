import { useState, useEffect } from 'react'
import { useNavigate, useLocation, Link } from 'react-router-dom'
import { ArrowLeft, Loader2, Search } from 'lucide-react'
import { useWallets } from '@/hooks/useWallets'
import { useCreateTransaction } from '@/hooks/useTransactions'
import { useAssetSearch } from '@/hooks/useAssetSearch'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Card, CardContent, CardHeader, CardTitle, CardDescription } from '@/components/ui/card'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { toast } from 'sonner'
import type { CreateTransactionRequest } from '@/types/transaction'
import type { Asset } from '@/types/asset'

export default function TransactionFormPage() {
  const navigate = useNavigate()
  const location = useLocation()
  const prefillWalletId = (location.state as { walletId?: string })?.walletId

  const [walletId, setWalletId] = useState(prefillWalletId || '')
  const [assetQuery, setAssetQuery] = useState('')
  const [selectedAsset, setSelectedAsset] = useState<Asset | null>(null)
  const [newBalance, setNewBalance] = useState('')
  const [usdRate, setUsdRate] = useState('')
  const [notes, setNotes] = useState('')

  const { data: wallets } = useWallets()
  const { data: assetResults, isLoading: assetsLoading } = useAssetSearch(assetQuery)
  const createTransaction = useCreateTransaction()

  // Auto-select wallet if prefilled
  useEffect(() => {
    if (prefillWalletId) {
      setWalletId(prefillWalletId)
    }
  }, [prefillWalletId])

  const handleAssetSelect = (asset: Asset) => {
    setSelectedAsset(asset)
    setAssetQuery(asset.symbol)
  }

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault()

    if (!walletId) {
      toast.error('Please select a wallet')
      return
    }

    if (!selectedAsset) {
      toast.error('Please select an asset')
      return
    }

    if (!newBalance) {
      toast.error('Please enter the new balance')
      return
    }

    const request: CreateTransactionRequest = {
      type: 'asset_adjustment',
      wallet_id: walletId,
      asset_id: selectedAsset.id,
      coingecko_id: selectedAsset.coingecko_id,
      amount: '0',
      new_balance: newBalance,
      usd_rate: usdRate || undefined,
      notes: notes || undefined,
    }

    try {
      const result = await createTransaction.mutateAsync(request)
      toast.success('Balance adjustment created successfully')
      navigate(`/transactions/${result.id}`)
    } catch (_error) {
      toast.error('Failed to create balance adjustment')
    }
  }

  return (
    <div className="max-w-2xl mx-auto space-y-6">
      {/* Back button */}
      <Button asChild variant="ghost" size="sm" className="-ml-2">
        <Link to="/transactions">
          <ArrowLeft className="mr-2 h-4 w-4" />
          Back to transactions
        </Link>
      </Button>

      <div>
        <h1 className="text-2xl font-bold tracking-tight">Balance Adjustment</h1>
        <p className="text-muted-foreground">
          Correct an asset balance to match your actual holdings. Regular transactions are synced automatically from the blockchain.
        </p>
      </div>

      <form onSubmit={handleSubmit} className="space-y-6">
        {/* Adjustment details */}
        <Card>
          <CardHeader className="pb-3">
            <CardTitle className="text-base">Adjustment Details</CardTitle>
            <CardDescription>
              Set the correct balance for an asset in your wallet
            </CardDescription>
          </CardHeader>
          <CardContent className="space-y-4">
            {/* Wallet */}
            <div className="space-y-2">
              <Label htmlFor="wallet">Wallet</Label>
              <Select value={walletId} onValueChange={setWalletId}>
                <SelectTrigger>
                  <SelectValue placeholder="Select a wallet" />
                </SelectTrigger>
                <SelectContent>
                  {wallets?.map((wallet) => (
                    <SelectItem key={wallet.id} value={wallet.id}>
                      {wallet.name}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>

            {/* Asset search */}
            <div className="space-y-2">
              <Label htmlFor="asset">Asset</Label>
              <div className="relative">
                <Search className="absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-muted-foreground" />
                <Input
                  id="asset"
                  placeholder="Search for an asset (e.g., BTC, ETH)"
                  value={assetQuery}
                  onChange={(e) => {
                    setAssetQuery(e.target.value)
                    if (selectedAsset && e.target.value !== selectedAsset.symbol) {
                      setSelectedAsset(null)
                    }
                  }}
                  className="pl-10"
                />
              </div>
              {/* Asset search results */}
              {assetQuery.length >= 2 && !selectedAsset && (
                <div className="rounded-md border border-border bg-popover">
                  {assetsLoading ? (
                    <div className="p-4 text-center text-sm text-muted-foreground">
                      Searching...
                    </div>
                  ) : assetResults && assetResults.length > 0 ? (
                    <div className="max-h-48 overflow-y-auto">
                      {assetResults.map((asset) => (
                        <button
                          key={asset.id}
                          type="button"
                          className="w-full px-4 py-2 text-left hover:bg-accent flex items-center gap-3"
                          onClick={() => handleAssetSelect(asset)}
                        >
                          <span className="font-medium">{asset.symbol}</span>
                          <span className="text-sm text-muted-foreground">
                            {asset.name}
                          </span>
                        </button>
                      ))}
                    </div>
                  ) : (
                    <div className="p-4 text-center text-sm text-muted-foreground">
                      No assets found
                    </div>
                  )}
                </div>
              )}
              {selectedAsset && (
                <p className="text-sm text-muted-foreground">
                  Selected: {selectedAsset.name} ({selectedAsset.symbol})
                </p>
              )}
            </div>

            {/* New Balance */}
            <div className="space-y-2">
              <Label htmlFor="newBalance">New Balance</Label>
              <Input
                id="newBalance"
                type="number"
                step="any"
                placeholder="Enter the correct balance"
                value={newBalance}
                onChange={(e) => setNewBalance(e.target.value)}
              />
              <p className="text-sm text-muted-foreground">
                The total amount you should have in this wallet
              </p>
            </div>

            {/* USD Rate (optional) */}
            <div className="space-y-2">
              <Label htmlFor="usdRate">
                USD Price{' '}
                <span className="text-muted-foreground">(optional)</span>
              </Label>
              <Input
                id="usdRate"
                type="number"
                step="any"
                placeholder="Auto-fetched from CoinGecko"
                value={usdRate}
                onChange={(e) => setUsdRate(e.target.value)}
              />
              <p className="text-sm text-muted-foreground">
                Leave empty to use the current market price
              </p>
            </div>

            {/* Notes (optional) */}
            <div className="space-y-2">
              <Label htmlFor="notes">
                Notes{' '}
                <span className="text-muted-foreground">(optional)</span>
              </Label>
              <Input
                id="notes"
                placeholder="Add any notes about this adjustment"
                value={notes}
                onChange={(e) => setNotes(e.target.value)}
              />
            </div>
          </CardContent>
        </Card>

        {/* Submit */}
        <div className="flex justify-end gap-2">
          <Button
            type="button"
            variant="outline"
            onClick={() => navigate(-1)}
            disabled={createTransaction.isPending}
          >
            Cancel
          </Button>
          <Button type="submit" disabled={createTransaction.isPending}>
            {createTransaction.isPending ? (
              <>
                <Loader2 className="mr-2 h-4 w-4 animate-spin" />
                Creating...
              </>
            ) : (
              'Create Adjustment'
            )}
          </Button>
        </div>
      </form>
    </div>
  )
}
