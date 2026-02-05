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
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { toast } from 'sonner'
import type { TransactionType, CreateTransactionRequest } from '@/types/transaction'
import type { Asset } from '@/types/asset'

export default function TransactionFormPage() {
  const navigate = useNavigate()
  const location = useLocation()
  const prefillWalletId = (location.state as { walletId?: string })?.walletId

  const [type, setType] = useState<TransactionType>('manual_income')
  const [walletId, setWalletId] = useState(prefillWalletId || '')
  const [assetQuery, setAssetQuery] = useState('')
  const [selectedAsset, setSelectedAsset] = useState<Asset | null>(null)
  const [amount, setAmount] = useState('')
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

    if (type === 'asset_adjustment') {
      if (!newBalance) {
        toast.error('Please enter the new balance')
        return
      }
    } else {
      if (!amount) {
        toast.error('Please enter an amount')
        return
      }
    }

    const request: CreateTransactionRequest = {
      type,
      wallet_id: walletId,
      asset_id: selectedAsset.id,
      coingecko_id: selectedAsset.coingecko_id,
      amount: type === 'asset_adjustment' ? '0' : amount,
      new_balance: type === 'asset_adjustment' ? newBalance : undefined,
      usd_rate: usdRate || undefined,
      notes: notes || undefined,
    }

    try {
      const result = await createTransaction.mutateAsync(request)
      toast.success('Transaction created successfully')
      navigate(`/transactions/${result.id}`)
    } catch (_error) {
      toast.error('Failed to create transaction')
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
        <h1 className="text-2xl font-bold tracking-tight">New Transaction</h1>
        <p className="text-muted-foreground">
          Record a new transaction in your portfolio
        </p>
      </div>

      <form onSubmit={handleSubmit} className="space-y-6">
        {/* Transaction type */}
        <Card>
          <CardHeader className="pb-3">
            <CardTitle className="text-base">Transaction Type</CardTitle>
            <CardDescription>
              Select the type of transaction you want to record
            </CardDescription>
          </CardHeader>
          <CardContent>
            <Tabs value={type} onValueChange={(v) => setType(v as TransactionType)}>
              <TabsList className="grid w-full grid-cols-3">
                <TabsTrigger value="manual_income">Income</TabsTrigger>
                <TabsTrigger value="manual_outcome">Outcome</TabsTrigger>
                <TabsTrigger value="asset_adjustment">Adjustment</TabsTrigger>
              </TabsList>
              <TabsContent value="manual_income" className="pt-4 text-sm text-muted-foreground">
                Record incoming assets to your wallet (purchases, airdrops, rewards)
              </TabsContent>
              <TabsContent value="manual_outcome" className="pt-4 text-sm text-muted-foreground">
                Record outgoing assets from your wallet (sales, transfers, payments)
              </TabsContent>
              <TabsContent value="asset_adjustment" className="pt-4 text-sm text-muted-foreground">
                Correct the balance of an asset to match your actual holdings
              </TabsContent>
            </Tabs>
          </CardContent>
        </Card>

        {/* Transaction details */}
        <Card>
          <CardHeader className="pb-3">
            <CardTitle className="text-base">Transaction Details</CardTitle>
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

            {/* Amount or New Balance */}
            {type === 'asset_adjustment' ? (
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
            ) : (
              <div className="space-y-2">
                <Label htmlFor="amount">Amount</Label>
                <Input
                  id="amount"
                  type="number"
                  step="any"
                  placeholder="0.00"
                  value={amount}
                  onChange={(e) => setAmount(e.target.value)}
                />
              </div>
            )}

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
                placeholder="Add any notes about this transaction"
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
              'Create Transaction'
            )}
          </Button>
        </div>
      </form>
    </div>
  )
}
