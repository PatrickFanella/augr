const DEFAULT_API_BASE_URL = 'http://localhost:8080'

export function getApiBaseUrl() {
  const configuredBaseUrl = (import.meta.env.VITE_API_BASE_URL || '').trim()
  const baseUrl = configuredBaseUrl || DEFAULT_API_BASE_URL

  return baseUrl.replace(/\/$/, '')
}

export function getWebSocketUrl(path = '/ws') {
  const url = new URL(getApiBaseUrl())
  url.protocol = url.protocol === 'https:' ? 'wss:' : 'ws:'
  url.pathname = path.startsWith('/') ? path : `/${path}`
  url.search = ''
  url.hash = ''
  return url.toString()
}
