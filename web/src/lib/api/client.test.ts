import { afterEach, describe, expect, it, vi } from 'vitest'

import { ApiClient, ApiClientError } from '@/lib/api/client'

afterEach(() => {
  vi.unstubAllGlobals()
})

describe('ApiClient', () => {
  it('builds list requests with backend-compatible query params', async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      json: async () => ({ data: [], limit: 25, offset: 50 }),
    })
    vi.stubGlobal('fetch', fetchMock)

    const client = new ApiClient({ baseUrl: 'http://localhost:8080', token: 'jwt-token' })
    await client.listStrategies({ limit: 25, offset: 50, ticker: 'AAPL', is_active: true })

    expect(fetchMock).toHaveBeenCalledTimes(1)
    const [requestUrl, requestInit] = fetchMock.mock.calls[0] as [URL, RequestInit]
    expect(requestUrl.toString()).toBe(
      'http://localhost:8080/api/v1/strategies?limit=25&offset=50&ticker=AAPL&is_active=true',
    )
    expect(new Headers(requestInit.headers).get('Authorization')).toBe('Bearer jwt-token')
  })

  it('surfaces backend error envelopes', async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      ok: false,
      status: 401,
      json: async () => ({ error: 'unauthorized', code: 'ERR_UNAUTHORIZED' }),
    })
    vi.stubGlobal('fetch', fetchMock)

    const client = new ApiClient({ baseUrl: 'http://localhost:8080' })

    await expect(client.getRiskStatus()).rejects.toEqual(
      new ApiClientError('unauthorized', 401, 'ERR_UNAUTHORIZED'),
    )
  })
})
