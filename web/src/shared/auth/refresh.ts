import { appConfig } from '@/app/config/env'
import { parseContract } from '@/shared/api/contract'
import { ApiClientError, toApiClientError } from '@/shared/api/errors'
import { authResponseSchema } from '@/shared/api/schemas'
import { clearTokenSnapshot, getAuthEpoch, getRefreshToken, setTokenSnapshot } from '@/shared/auth/tokenStore'
import { redactUrl } from '@/shared/logging/redact'
import type { AuthResponse } from '@/shared/types/auth'

let refreshPromise: Promise<AuthResponse> | null = null
let onRefreshFailure: (() => void) | null = null

export function setRefreshFailureHandler(handler: (() => void) | null): void {
  onRefreshFailure = handler
}

export async function refreshAccessToken(): Promise<AuthResponse> {
  if (refreshPromise) return refreshPromise
  const refreshToken = getRefreshToken()
  const startEpoch = getAuthEpoch()
  if (!refreshToken) {
    throw new ApiClientError('No refresh token is available', { kind: 'unauthorized' })
  }

  refreshPromise = performRefresh(refreshToken)
    .then((tokens) => {
      if (getAuthEpoch() !== startEpoch) {
        throw new ApiClientError('Session changed during token refresh', { kind: 'unauthorized' })
      }
      setTokenSnapshot(tokens)
      return tokens
    })
    .catch((error) => {
      if (getAuthEpoch() === startEpoch) {
        clearTokenSnapshot()
        onRefreshFailure?.()
      }
      throw error
    })
    .finally(() => {
      refreshPromise = null
    })

  return refreshPromise
}

async function performRefresh(refreshToken: string): Promise<AuthResponse> {
  const endpoint = `${appConfig.apiBaseUrl}/auth/refresh`
  const response = await fetch(endpoint, {
    method: 'POST',
    headers: { 'content-type': 'application/json', accept: 'application/json' },
    body: JSON.stringify({ refresh_token: refreshToken }),
  })
  const body = (await response.json().catch(() => undefined)) as unknown
  if (!response.ok) {
    throw toApiClientError(response.status, typeof body === 'object' && body ? body : undefined, redactUrl(endpoint))
  }
  return parseContract('POST /auth/refresh', authResponseSchema, body)
}
