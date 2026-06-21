import type { AuthResponse } from '@/shared/types/auth'

type TokenSnapshot = AuthResponse | null

const refreshStorageKey = 'augr.refresh-token.session'
let snapshot: TokenSnapshot = null
let authEpoch = 0

export function getTokenSnapshot(): TokenSnapshot {
  if (!snapshot) return null
  return { ...snapshot, refresh_token: snapshot.refresh_token || sessionStorage.getItem(refreshStorageKey) || '' }
}

export function setTokenSnapshot(tokens: AuthResponse): void {
  snapshot = tokens
  sessionStorage.setItem(refreshStorageKey, tokens.refresh_token)
}

export function clearTokenSnapshot(): void {
  authEpoch += 1
  snapshot = null
  sessionStorage.removeItem(refreshStorageKey)
}

export function getAuthEpoch(): number {
  return authEpoch
}

export function getAccessToken(): string | null {
  return snapshot?.access_token ?? null
}

export function getRefreshToken(): string | null {
  return snapshot?.refresh_token || sessionStorage.getItem(refreshStorageKey)
}

export function isAccessTokenExpiringSoon(skewMs = 60_000): boolean {
  if (!snapshot?.expires_at) return true
  return Date.parse(snapshot.expires_at) - Date.now() <= skewMs
}
