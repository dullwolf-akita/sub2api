/**
 * Bridges browser wallet extensions from the parent page into cross-origin iframes
 * (e.g. pay.openllm.shop). Wallet extensions inject providers on the top window only;
 * embedded pay pages request wallet access via postMessage.
 */

import { computed, onMounted, onUnmounted, type ComputedRef, type Ref, toValue, type MaybeRefOrGetter } from 'vue'

type BridgeWalletId = 'binance' | 'okx' | 'metamask'

interface ParentInjectedProvider {
  request: (args: { method: string; params?: unknown[] }) => Promise<unknown>
  isBinance?: boolean
  isBinanceWallet?: boolean
  isBinanceChainWallet?: boolean
  isBnbChain?: boolean
  isBnbChainWallet?: boolean
  isBNBChain?: boolean
  isBNBChainWallet?: boolean
  isMetaMask?: boolean
  isOkxWallet?: boolean
  isOKExWallet?: boolean
  isOkxWeb3?: boolean
  providers?: ParentInjectedProvider[]
  name?: string
  label?: string
  rdns?: string
  providerName?: string
  walletName?: string
  providerInfo?: { name?: string; rdns?: string }
  walletInfo?: { name?: string; rdns?: string }
  _metamask?: { isUnlocked?: () => Promise<boolean> }
}

type ParentWalletWindow = Window & {
  ethereum?: ParentInjectedProvider
  okxwallet?: ParentInjectedProvider
  binancew3w?: { ethereum?: ParentInjectedProvider }
  BinanceChain?: ParentInjectedProvider
  binanceChain?: ParentInjectedProvider
  BNBChain?: ParentInjectedProvider
  bnbChain?: ParentInjectedProvider
}

function isBridgeWalletId(value: unknown): value is BridgeWalletId {
  return value === 'binance' || value === 'okx' || value === 'metamask'
}

function getProviderMetadataHints(provider: ParentInjectedProvider): string[] {
  const values = [
    provider.name,
    provider.label,
    provider.rdns,
    provider.providerName,
    provider.walletName,
    provider.providerInfo?.name,
    provider.providerInfo?.rdns,
    provider.walletInfo?.name,
    provider.walletInfo?.rdns,
  ]

  return values
    .filter((value): value is string => typeof value === 'string' && value.trim().length > 0)
    .map((value) => value.trim().toLowerCase())
}

function isBinanceProvider(provider: ParentInjectedProvider, win: ParentWalletWindow): boolean {
  const metadataHints = getProviderMetadataHints(provider)

  return Boolean(
    provider === win.binancew3w?.ethereum
      || provider === win.BinanceChain
      || provider === win.binanceChain
      || provider === win.BNBChain
      || provider === win.bnbChain
      || provider.request === win.binancew3w?.ethereum?.request
      || provider.request === win.BinanceChain?.request
      || provider.request === win.binanceChain?.request
      || provider.request === win.BNBChain?.request
      || provider.request === win.bnbChain?.request
      || provider.isBinance
      || provider.isBinanceWallet
      || provider.isBinanceChainWallet
      || provider.isBnbChain
      || provider.isBnbChainWallet
      || provider.isBNBChain
      || provider.isBNBChainWallet
      || metadataHints.some((hint) => hint.includes('binance') || hint.includes('bnb chain') || hint.includes('bnbchain')),
  )
}

function isOkxProvider(provider: ParentInjectedProvider, win: ParentWalletWindow): boolean {
  return Boolean(
    provider === win.okxwallet
      || provider.request === win.okxwallet?.request
      || provider.isOkxWallet
      || provider.isOKExWallet
      || provider.isOkxWeb3,
  )
}

function isMetaMaskProvider(provider: ParentInjectedProvider, win: ParentWalletWindow): boolean {
  return Boolean(
    provider.isMetaMask
      && provider._metamask
      && !isOkxProvider(provider, win)
      && !isBinanceProvider(provider, win),
  )
}

function detectTopWalletProviders(): Map<BridgeWalletId, ParentInjectedProvider> {
  if (typeof window === 'undefined') return new Map()

  const win = window as ParentWalletWindow
  const wallets = new Map<BridgeWalletId, ParentInjectedProvider>()
  const pushWallet = (walletId: BridgeWalletId, provider: ParentInjectedProvider | undefined) => {
    if (!provider || typeof provider.request !== 'function' || wallets.has(walletId)) {
      return
    }
    wallets.set(walletId, provider)
  }

  pushWallet('binance', win.binancew3w?.ethereum)
  pushWallet('binance', win.BinanceChain)
  pushWallet('binance', win.binanceChain)
  pushWallet('binance', win.BNBChain)
  pushWallet('binance', win.bnbChain)
  pushWallet('okx', win.okxwallet)

  const ethereumProviders = Array.isArray(win.ethereum?.providers)
    ? win.ethereum.providers
    : win.ethereum
      ? [win.ethereum]
      : []

  for (const provider of ethereumProviders) {
    if (!provider || typeof provider.request !== 'function') continue

    if (isBinanceProvider(provider, win)) {
      pushWallet('binance', provider)
      continue
    }

    if (isOkxProvider(provider, win)) {
      pushWallet('okx', provider)
      continue
    }

    if (isMetaMaskProvider(provider, win)) {
      pushWallet('metamask', provider)
    }
  }

  return wallets
}

export function useWalletBridge(
  frameRef: Ref<HTMLIFrameElement | null>,
  embeddedUrl: MaybeRefOrGetter<string>,
) {
  let walletBridgeInterval: number | null = null

  const frameOrigin = computed(() => {
    const url = toValue(embeddedUrl).trim()
    if (!url) return ''
    try {
      return new URL(url).origin
    } catch {
      return ''
    }
  })

  function postWalletBridgeStatus() {
    const frameWindow = frameRef.value?.contentWindow
    const origin = frameOrigin.value
    if (!frameWindow || !origin) return

    frameWindow.postMessage({
      type: 'sub2api:wallet-bridge-status',
      walletIds: Array.from(detectTopWalletProviders().keys()),
    }, origin)
  }

  function sendWalletBridgeResponse(payload: { requestId: string; result?: unknown; error?: string }) {
    const frameWindow = frameRef.value?.contentWindow
    const origin = frameOrigin.value
    if (!frameWindow || !origin) return

    frameWindow.postMessage({
      type: 'sub2api:wallet-bridge-response',
      ...payload,
    }, origin)
  }

  function scheduleWalletBridgeSync() {
    if (typeof window === 'undefined') return

    postWalletBridgeStatus()

    if (walletBridgeInterval !== null) {
      window.clearInterval(walletBridgeInterval)
    }

    let tickCount = 0
    walletBridgeInterval = window.setInterval(() => {
      tickCount += 1
      postWalletBridgeStatus()
      if (tickCount >= 20 && walletBridgeInterval !== null) {
        window.clearInterval(walletBridgeInterval)
        walletBridgeInterval = null
      }
    }, 500)
  }

  function handleFrameLoad() {
    scheduleWalletBridgeSync()
  }

  async function handleWalletBridgeMessage(event: MessageEvent) {
    const frameWindow = frameRef.value?.contentWindow
    const origin = frameOrigin.value
    if (!frameWindow || !origin) return
    if (event.source !== frameWindow || event.origin !== origin) return

    const data = event.data as {
      type?: string
      requestId?: string
      walletId?: unknown
      args?: { method: string; params?: unknown[] }
    } | undefined

    if (!data?.type) {
      return
    }

    if (data.type === 'sub2api:wallet-bridge-sync') {
      postWalletBridgeStatus()
      return
    }

    if (data.type !== 'sub2api:wallet-bridge-request' || !data.requestId || !isBridgeWalletId(data.walletId) || !data.args) {
      return
    }

    const provider = detectTopWalletProviders().get(data.walletId)
    if (!provider) {
      sendWalletBridgeResponse({
        requestId: data.requestId,
        error: 'The selected wallet extension was not detected in this browser.',
      })
      return
    }

    try {
      const result = await provider.request(data.args)
      sendWalletBridgeResponse({
        requestId: data.requestId,
        result,
      })
    } catch (error) {
      sendWalletBridgeResponse({
        requestId: data.requestId,
        error: error instanceof Error ? error.message : String(error),
      })
    }
  }

  function attachBridgeListeners() {
    if (typeof window === 'undefined') return
    window.addEventListener('message', handleWalletBridgeMessage)
    window.addEventListener('focus', scheduleWalletBridgeSync)
    window.addEventListener('pageshow', scheduleWalletBridgeSync)
    document.addEventListener('visibilitychange', scheduleWalletBridgeSync)
    scheduleWalletBridgeSync()
  }

  function detachBridgeListeners() {
    if (typeof window === 'undefined') return
    window.removeEventListener('message', handleWalletBridgeMessage)
    window.removeEventListener('focus', scheduleWalletBridgeSync)
    window.removeEventListener('pageshow', scheduleWalletBridgeSync)
    document.removeEventListener('visibilitychange', scheduleWalletBridgeSync)
    if (walletBridgeInterval !== null) {
      window.clearInterval(walletBridgeInterval)
      walletBridgeInterval = null
    }
  }

  onMounted(attachBridgeListeners)
  onUnmounted(detachBridgeListeners)

  return {
    frameOrigin: frameOrigin as ComputedRef<string>,
    handleFrameLoad,
    scheduleWalletBridgeSync,
  }
}
