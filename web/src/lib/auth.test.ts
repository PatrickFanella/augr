import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { clearTokens, getAccessToken, getExpiresAt, getRefreshToken, isAuthenticated, setTokens } from '@/lib/auth'

describe('auth', () => {
  beforeEach(() => {
    localStorage.clear()
  })

  afterEach(() => {
    vi.useRealTimers()
  })

  it('returns false when the token expiry is missing', () => {
    localStorage.setItem('access_token', 'access-token')

    expect(isAuthenticated()).toBe(false)
  })

  it('accepts millisecond expiry timestamps in the future', () => {
    vi.useFakeTimers()
    vi.setSystemTime(new Date('2026-03-30T08:00:00Z'))

    setTokens('access-token', 'refresh-token', Date.now() + 60_000)

    expect(isAuthenticated()).toBe(true)
  })

  it('accepts second-based expiry timestamps in the future', () => {
    vi.useFakeTimers()
    vi.setSystemTime(new Date('2026-03-30T08:00:00Z'))

    setTokens('access-token', 'refresh-token', Math.floor((Date.now() + 60_000) / 1000))

    expect(isAuthenticated()).toBe(true)
  })

  it('returns false when the token expiry is invalid', () => {
    localStorage.setItem('access_token', 'access-token')
    localStorage.setItem('expires_at', 'not-a-number')

    expect(isAuthenticated()).toBe(false)
  })

  it('returns false when the token expiry is in the past', () => {
    vi.useFakeTimers()
    vi.setSystemTime(new Date('2026-03-30T08:00:00Z'))

    setTokens('access-token', 'refresh-token', Date.now() - 1)

    expect(isAuthenticated()).toBe(false)
  })

  it('returns false after clearing tokens', () => {
    setTokens('access-token', 'refresh-token', Date.now() + 60_000)

    clearTokens()

    expect(isAuthenticated()).toBe(false)
  })

  it('fails closed when localStorage access throws', () => {
    const descriptor = Object.getOwnPropertyDescriptor(window, 'localStorage')

    Object.defineProperty(window, 'localStorage', {
      configurable: true,
      get() {
        throw new DOMException('Access denied', 'SecurityError')
      },
    })

    try {
      expect(isAuthenticated()).toBe(false)
      expect(() => setTokens('access-token', 'refresh-token', Date.now() + 60_000)).not.toThrow()
      expect(() => clearTokens()).not.toThrow()
    } finally {
      if (descriptor) {
        Object.defineProperty(window, 'localStorage', descriptor)
      }
    }
  })

  it('getRefreshToken returns stored refresh token', () => {
    setTokens('access', 'refresh-123', Date.now() + 3600_000)
    expect(getRefreshToken()).toBe('refresh-123')
  })

  it('getRefreshToken returns null when no token', () => {
    expect(getRefreshToken()).toBeNull()
  })

  it('getExpiresAt normalizes seconds to milliseconds', () => {
    const seconds = Math.floor(Date.now() / 1000) + 3600
    setTokens('access', 'refresh', seconds)
    const result = getExpiresAt()
    expect(result).toBeGreaterThan(Date.now())
    // Should be in milliseconds (13+ digits)
    expect(String(result).length).toBeGreaterThanOrEqual(13)
  })

  it('getExpiresAt returns milliseconds as-is', () => {
    const ms = Date.now() + 3600_000
    setTokens('access', 'refresh', ms)
    expect(getExpiresAt()).toBe(ms)
  })

  it('clearTokens removes all tokens', () => {
    setTokens('access', 'refresh', Date.now() + 3600_000)
    clearTokens()
    expect(getAccessToken()).toBeNull()
    expect(getRefreshToken()).toBeNull()
    expect(getExpiresAt()).toBeNull()
  })
})
