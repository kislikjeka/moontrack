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
import { SUPPORTED_CHAINS, isValidEVMAddress } from '@/types/wallet'
import { toast } from 'sonner'

interface CreateWalletDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
}

export function CreateWalletDialog({ open, onOpenChange }: CreateWalletDialogProps) {
  const [name, setName] = useState('')
  const [chainId, setChainId] = useState<string>('')
  const [address, setAddress] = useState('')
  const [addressError, setAddressError] = useState('')
  const createWallet = useCreateWallet()

  const validateAddress = (value: string) => {
    if (!value.trim()) {
      setAddressError('Wallet address is required')
      return false
    }
    if (!isValidEVMAddress(value.trim())) {
      setAddressError('Invalid EVM address (must be 0x + 40 hex characters)')
      return false
    }
    setAddressError('')
    return true
  }

  const handleAddressChange = (value: string) => {
    setAddress(value)
    if (value.trim()) {
      validateAddress(value)
    } else {
      setAddressError('')
    }
  }

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

    if (!validateAddress(address)) {
      toast.error('Please enter a valid EVM address')
      return
    }

    try {
      await createWallet.mutateAsync({
        name: name.trim(),
        chain_id: Number(chainId),
        address: address.trim(),
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
    setAddressError('')
    onOpenChange(false)
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>Create Wallet</DialogTitle>
          <DialogDescription>
            Add an EVM wallet to track. Transactions will be synced automatically.
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
                  <SelectItem key={chain.id} value={String(chain.id)}>
                    {chain.name} ({chain.symbol})
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>

          <div className="space-y-2">
            <Label htmlFor="address">Wallet Address</Label>
            <Input
              id="address"
              placeholder="0x..."
              value={address}
              onChange={(e) => handleAddressChange(e.target.value)}
              className={`font-mono ${addressError ? 'border-destructive' : ''}`}
              disabled={createWallet.isPending}
            />
            {addressError ? (
              <p className="text-sm text-destructive">{addressError}</p>
            ) : (
              <p className="text-sm text-muted-foreground">
                Enter an EVM wallet address (0x + 40 hex characters)
              </p>
            )}
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
