import { useState } from 'react'
import { Loader2 } from 'lucide-react'
import { useCreateWallet } from '@/hooks/useWallets'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { SUPPORTED_CHAINS } from '@/types/wallet'
import { toast } from 'sonner'

interface CreateWalletDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
}

export function CreateWalletDialog({ open, onOpenChange }: CreateWalletDialogProps) {
  const [name, setName] = useState('')
  const [chainId, setChainId] = useState('')
  const [address, setAddress] = useState('')
  const createWallet = useCreateWallet()

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault()

    if (!name.trim()) {
      toast.error('Please enter a wallet name')
      return
    }

    if (!chainId) {
      toast.error('Please select a chain')
      return
    }

    try {
      await createWallet.mutateAsync({
        name: name.trim(),
        chain_id: chainId,
        address: address.trim() || undefined,
      })
      toast.success('Wallet created successfully')
      handleClose()
    } catch (_error) {
      toast.error('Failed to create wallet')
    }
  }

  const handleClose = () => {
    setName('')
    setChainId('')
    setAddress('')
    onOpenChange(false)
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>Create Wallet</DialogTitle>
          <DialogDescription>
            Add a new wallet to track your crypto holdings
          </DialogDescription>
        </DialogHeader>

        <form onSubmit={handleSubmit} className="space-y-4">
          <div className="space-y-2">
            <Label htmlFor="name">Wallet Name</Label>
            <Input
              id="name"
              placeholder="My Ethereum Wallet"
              value={name}
              onChange={(e) => setName(e.target.value)}
              disabled={createWallet.isPending}
            />
          </div>

          <div className="space-y-2">
            <Label htmlFor="chain">Chain</Label>
            <Select
              value={chainId}
              onValueChange={setChainId}
              disabled={createWallet.isPending}
            >
              <SelectTrigger>
                <SelectValue placeholder="Select a chain" />
              </SelectTrigger>
              <SelectContent>
                {SUPPORTED_CHAINS.map((chain) => (
                  <SelectItem key={chain.id} value={chain.id}>
                    {chain.name} ({chain.symbol})
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>

          <div className="space-y-2">
            <Label htmlFor="address">
              Wallet Address{' '}
              <span className="text-muted-foreground">(optional)</span>
            </Label>
            <Input
              id="address"
              placeholder="0x..."
              value={address}
              onChange={(e) => setAddress(e.target.value)}
              className="font-mono"
              disabled={createWallet.isPending}
            />
          </div>

          <DialogFooter>
            <Button
              type="button"
              variant="outline"
              onClick={handleClose}
              disabled={createWallet.isPending}
            >
              Cancel
            </Button>
            <Button type="submit" disabled={createWallet.isPending}>
              {createWallet.isPending ? (
                <>
                  <Loader2 className="mr-2 h-4 w-4 animate-spin" />
                  Creating...
                </>
              ) : (
                'Create Wallet'
              )}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}
