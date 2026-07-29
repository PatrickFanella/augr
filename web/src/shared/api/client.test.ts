import { http, HttpResponse } from 'msw'
import { setupServer } from 'msw/node'
import { afterAll, afterEach, beforeAll, describe, expect, it } from 'vitest'

import { apiRequest, configureApiClient } from '@/shared/api/client'
import { getCurrentUser } from '@/shared/api/endpoints'
import { clearTokenSnapshot, getAccessToken, setTokenSnapshot } from '@/shared/auth/tokenStore'
import { buildAuthResponse, buildUser } from '@/test/fixtures'

const apiBaseUrl = '/api/v1'
let refreshCalls = 0

async function waitForRefreshStart() {
  for (let attempt = 0; attempt < 20 && refreshCalls === 0; attempt += 1) {
    await new Promise((resolve) => setTimeout(resolve, 0))
  }
}

const server = setupServer(
  http.post(`${apiBaseUrl}/auth/refresh`, () => {
    refreshCalls += 1
    return HttpResponse.json(buildAuthResponse({ access_token: 'new-access-token' }))
  }),
  http.get(`${apiBaseUrl}/me`, ({ request }) => {
    if (request.headers.get('authorization') === 'Bearer new-access-token') return HttpResponse.json(buildUser())
    return HttpResponse.json({ error: 'expired', code: 'ERR_UNAUTHORIZED' }, { status: 401 })
  }),
)

describe('api client refresh handling', () => {
  beforeAll(() => server.listen({ onUnhandledRequest: 'error' }))
  afterEach(() => {
    refreshCalls = 0
    clearTokenSnapshot()
    server.resetHandlers()
  })
  afterAll(() => server.close())

  it('coalesces concurrent 401 refresh attempts', async () => {
    configureApiClient({ baseUrl: apiBaseUrl })
    setTokenSnapshot(buildAuthResponse({ access_token: 'expired-access-token' }))

    const [a, b, c] = await Promise.all([getCurrentUser(), getCurrentUser(), getCurrentUser()])

    expect(a.username).toBe('dev-paper-operator')
    expect(b.username).toBe('dev-paper-operator')
    expect(c.username).toBe('dev-paper-operator')
    expect(refreshCalls).toBe(1)
  })

  it('does not install refreshed tokens after logout during pending refresh', async () => {
    configureApiClient({ baseUrl: apiBaseUrl })
    setTokenSnapshot(buildAuthResponse({ access_token: 'expired-access-token' }))
    server.use(
      http.post(`${apiBaseUrl}/auth/refresh`, async () => {
        refreshCalls += 1
        await new Promise((resolve) => setTimeout(resolve, 25))
        return HttpResponse.json(buildAuthResponse({ access_token: 'new-access-token' }))
      }),
    )

    const request = getCurrentUser()
    await waitForRefreshStart()
    clearTokenSnapshot()

    await expect(request).rejects.toThrow(/session changed|unauthorized/i)
    expect(refreshCalls).toBe(1)
    expect(getAccessToken()).toBeNull()
  })

  it('does not replay non-GET requests after 401', async () => {
    configureApiClient({ baseUrl: apiBaseUrl })
    setTokenSnapshot(buildAuthResponse({ access_token: 'expired-access-token' }))
    let putCalls = 0
    server.use(
      http.put(`${apiBaseUrl}/settings`, () => {
        putCalls += 1
        return HttpResponse.json({ error: 'expired', code: 'ERR_UNAUTHORIZED' }, { status: 401 })
      }),
    )

    await expect(apiRequest('/settings', { method: 'PUT', body: { theme: 'paper' } })).rejects.toThrow(/expired/i)

    expect(putCalls).toBe(1)
    expect(refreshCalls).toBe(0)
  })

  it('retains the session when refresh fails transiently', async () => {
    configureApiClient({ baseUrl: apiBaseUrl })
    setTokenSnapshot(buildAuthResponse({ access_token: 'expired-access-token' }))
    server.use(
      http.post(`${apiBaseUrl}/auth/refresh`, () => HttpResponse.json({ error: 'temporarily unavailable', code: 'ERR_INTERNAL' }, { status: 503 })),
    )

    await expect(getCurrentUser()).rejects.toThrow(/temporarily unavailable/i)
    expect(getAccessToken()).toBe('expired-access-token')
  })
})
