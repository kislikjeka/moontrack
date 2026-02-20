import { useState } from 'react'
import { cn } from '@/lib/utils'
import { ChainIcon } from './ChainIcon'

interface AssetIconProps {
  symbol: string
  imageUrl?: string
  chainId?: string
  size?: 'sm' | 'default' | 'lg'
  className?: string
}

const sizeClasses = {
  sm: 'h-6 w-6 text-[10px]',
  default: 'h-8 w-8 text-xs',
  lg: 'h-10 w-10 text-sm',
}

const chainOverlaySize = {
  sm: 'xs' as const,
  default: 'xs' as const,
  lg: 'sm' as const,
}

// Token icon URLs from CoinGecko CDN (stable, no API key needed)
const TOKEN_ICONS: Record<string, string> = {
  ETH: 'https://coin-images.coingecko.com/coins/images/279/small/ethereum.png',
  BTC: 'https://coin-images.coingecko.com/coins/images/1/small/bitcoin.png',
  WBTC: 'https://coin-images.coingecko.com/coins/images/7598/small/wrapped_bitcoin_wbtc.png',
  USDC: 'https://coin-images.coingecko.com/coins/images/6319/small/usdc.png',
  USDT: 'https://coin-images.coingecko.com/coins/images/325/small/Tether.png',
  DAI: 'https://coin-images.coingecko.com/coins/images/9956/small/Badge_Dai.png',
  WETH: 'https://coin-images.coingecko.com/coins/images/2518/small/weth.png',
  LINK: 'https://coin-images.coingecko.com/coins/images/877/small/chainlink-new-logo.png',
  UNI: 'https://coin-images.coingecko.com/coins/images/12504/small/uni.png',
  AAVE: 'https://coin-images.coingecko.com/coins/images/12645/small/aave-token.png',
  MATIC: 'https://coin-images.coingecko.com/coins/images/4713/small/polygon.png',
  ARB: 'https://coin-images.coingecko.com/coins/images/16547/small/arb.jpg',
  OP: 'https://coin-images.coingecko.com/coins/images/25244/small/Optimism.png',
  SOL: 'https://coin-images.coingecko.com/coins/images/4128/small/solana.png',
  AVAX: 'https://coin-images.coingecko.com/coins/images/12559/small/Avalanche_Circle_RedWhite_Trans.png',
  BNB: 'https://coin-images.coingecko.com/coins/images/825/small/bnb-icon2_2x.png',
  DOGE: 'https://coin-images.coingecko.com/coins/images/5/small/dogecoin.png',
  SHIB: 'https://coin-images.coingecko.com/coins/images/11939/small/shiba.png',
  CRV: 'https://coin-images.coingecko.com/coins/images/12124/small/Curve.png',
  MKR: 'https://coin-images.coingecko.com/coins/images/1348/small/dai-multi-collateral-mcd.png',
  LDO: 'https://coin-images.coingecko.com/coins/images/13573/small/Lido_DAO.png',
  COMP: 'https://coin-images.coingecko.com/coins/images/10613/small/compoundd.png',
  STETH: 'https://coin-images.coingecko.com/coins/images/13442/small/steth_logo.png',
  RETH: 'https://coin-images.coingecko.com/coins/images/20947/small/reth.png',
  CBETH: 'https://coin-images.coingecko.com/coins/images/27008/small/cbeth.png',
  PEPE: 'https://coin-images.coingecko.com/coins/images/29850/small/pepe-token.jpeg',
  GRT: 'https://coin-images.coingecko.com/coins/images/13397/small/Graph_Token.png',
  SUSHI: 'https://coin-images.coingecko.com/coins/images/12271/small/512x512_Logo_no_chop.png',
  SNX: 'https://coin-images.coingecko.com/coins/images/3887/small/snx.png',
  APE: 'https://coin-images.coingecko.com/coins/images/24383/small/apecoin.jpg',
}

function getTokenImageUrl(symbol: string): string | undefined {
  return TOKEN_ICONS[symbol.toUpperCase()]
}

export function AssetIcon({
  symbol,
  imageUrl,
  chainId,
  size = 'default',
  className,
}: AssetIconProps) {
  const resolvedUrl = imageUrl || getTokenImageUrl(symbol)
  const [imgError, setImgError] = useState(false)
  const initials = symbol.slice(0, 3).toUpperCase()

  const fallback = (
    <div
      className={cn(
        'flex items-center justify-center rounded-full bg-primary/10 text-primary font-mono font-medium',
        sizeClasses[size]
      )}
    >
      {initials}
    </div>
  )

  const iconElement = resolvedUrl && !imgError ? (
    <img
      src={resolvedUrl}
      alt={symbol}
      className={cn('rounded-full object-cover', sizeClasses[size])}
      onError={() => setImgError(true)}
    />
  ) : fallback

  if (!chainId) {
    return <div className={className}>{iconElement}</div>
  }

  return (
    <div className={cn('relative inline-flex', className)}>
      {iconElement}
      <div className="absolute -bottom-0.5 -right-0.5 ring-2 ring-background rounded-full">
        <ChainIcon chainId={chainId} size={chainOverlaySize[size]} showTooltip />
      </div>
    </div>
  )
}
