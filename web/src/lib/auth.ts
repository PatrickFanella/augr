const ACCESS_TOKEN_KEY = 'access_token'
const REFRESH_TOKEN_KEY = 'refresh_token'
const EXPIRES_AT_KEY = 'expires_at'

function getStorage() {
  if (typeof window === 'undefined') {
    return null
  }

  try {
    return window.localStorage
  } catch {
    return null
  }
}

function getStorageValue(key: string) {
  const storage = getStorage()

  if (!storage) {
    return null
  }

  try {
    return storage.getItem(key)
  } catch {
    return null
  }
}

function setStorageValue(key: string, value: string) {
  const storage = getStorage()

  if (!storage) {
    return
  }

  try {
    storage.setItem(key, value)
  } catch {
    return
  }
}

function removeStorageValue(key: string) {
  const storage = getStorage()

  if (!storage) {
    return
  }

  try {
    storage.removeItem(key)
  } catch {
    return
  }
}

export function getAccessToken() {
  return getStorageValue(ACCESS_TOKEN_KEY)
}

export function getRefreshToken(): string | null {
  return getStorageValue(REFRESH_TOKEN_KEY)
}

export function getExpiresAt(): number | null {
  const raw = getStorageValue(EXPIRES_AT_KEY)
  if (!raw) return null
  const parsed = Number(raw)
  if (Number.isNaN(parsed)) return null
  // Normalize to milliseconds
  return parsed < 1_000_000_000_000 ? parsed * 1000 : parsed
}

export function setTokens(accessToken: string, refreshToken: string, expiresAt: string | number) {
  setStorageValue(ACCESS_TOKEN_KEY, accessToken)
  setStorageValue(REFRESH_TOKEN_KEY, refreshToken)
  setStorageValue(EXPIRES_AT_KEY, String(expiresAt))
}

export function clearTokens() {
  removeStorageValue(ACCESS_TOKEN_KEY)
  removeStorageValue(REFRESH_TOKEN_KEY)
  removeStorageValue(EXPIRES_AT_KEY)
}

export function isAuthenticated() {
  const accessToken = getAccessToken()

  if (!accessToken) {
    return false
  }

  const expiresAt = getStorageValue(EXPIRES_AT_KEY)

  if (!expiresAt) {
    return false
  }

  const parsedExpiresAt = Number(expiresAt)

  if (Number.isNaN(parsedExpiresAt)) {
    return false
  }

  const expiresAtMs = parsedExpiresAt < 1_000_000_000_000 ? parsedExpiresAt * 1000 : parsedExpiresAt

  return expiresAtMs > Date.now()
}
