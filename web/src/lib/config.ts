export function getApiBaseUrl() {
  const configuredBaseUrl = (import.meta.env.VITE_API_BASE_URL || '').trim()
  if (configuredBaseUrl) {
    return configuredBaseUrl.replace(/\/$/, '')
  }

  // Use same-origin: in dev Vite proxies /api and /ws to the backend,
  // so this works over SSH port-forwarding, Tailscale, and direct localhost alike.
  return window.location.origin
}

export function getWebSocketUrl(path = '/ws') {
  const base = getApiBaseUrl()
  const wsPath = path.startsWith('/') ? path : `/${path}`

  // Same-origin mode (dev proxy): derive WS URL from current page location.
  if (!base) {
    const proto = window.location.protocol === 'https:' ? 'wss:' : 'ws:'
    return `${proto}//${window.location.host}${wsPath}`
  }

  const url = new URL(base)
  url.protocol = url.protocol === 'https:' ? 'wss:' : 'ws:'
  url.pathname = wsPath
  url.search = ''
  url.hash = ''
  return url.toString()
}
