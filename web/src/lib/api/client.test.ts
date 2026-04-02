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

  it('normalizes null list data to an empty array', async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      json: async () => ({ data: null, limit: 25, offset: 0 }),
    })
    vi.stubGlobal('fetch', fetchMock)

    const client = new ApiClient({ baseUrl: 'http://localhost:8080' })

    await expect(client.listStrategies()).resolves.toEqual({ data: [], limit: 25, offset: 0 })
  })

  it('preserves populated list data responses', async () => {
    const payload = {
      data: [
        {
          id: 'strategy-1',
          name: 'Momentum',
          ticker: 'AAPL',
          market_type: 'stock',
          config: {},
          is_active: true,
          is_paper: true,
          created_at: '2026-03-30T00:00:00Z',
          updated_at: '2026-03-30T00:00:00Z',
        },
      ],
      limit: 25,
      offset: 0,
    }
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      json: async () => payload,
    })
    vi.stubGlobal('fetch', fetchMock)

    const client = new ApiClient({ baseUrl: 'http://localhost:8080' })

    await expect(client.listStrategies()).resolves.toEqual(payload)
  })

  it('supports conversation list/create/message endpoints', async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce({
        ok: true,
        status: 200,
        json: async () => ({ data: [{
          id: 'conv-1',
          pipeline_run_id: 'run-1',
          agent_role: 'trader',
          title: 'Chat with Trader — AAPL',
          created_at: '2026-04-01T00:00:00Z',
          updated_at: '2026-04-01T00:00:00Z',
        }], limit: 1, offset: 0 }),
      })
      .mockResolvedValueOnce({
        ok: true,
        status: 201,
        json: async () => ({
          id: 'conv-2',
          pipeline_run_id: 'run-1',
          agent_role: 'trader',
          title: 'Chat with Trader — AAPL',
          created_at: '2026-04-01T00:00:00Z',
          updated_at: '2026-04-01T00:00:00Z',
        }),
      })
      .mockResolvedValueOnce({
        ok: true,
        status: 200,
        json: async () => ({
          data: [{
            id: 'msg-1',
            conversation_id: 'conv-2',
            role: 'assistant',
            content: 'Momentum is still strong.',
            created_at: '2026-04-01T00:01:00Z',
          }],
          limit: 100,
          offset: 0,
        }),
      })
      .mockResolvedValueOnce({
        ok: true,
        status: 201,
        json: async () => ({
          id: 'msg-2',
          conversation_id: 'conv-2',
          role: 'assistant',
          content: 'I still favor the long side.',
          created_at: '2026-04-01T00:02:00Z',
        }),
      })
    vi.stubGlobal('fetch', fetchMock)

    const client = new ApiClient({ baseUrl: 'http://localhost:8080' })

    await expect(
      client.listConversations({ pipeline_run_id: 'run-1', agent_role: 'trader', limit: 1 }),
    ).resolves.toEqual({
      data: [{
        id: 'conv-1',
        pipeline_run_id: 'run-1',
        agent_role: 'trader',
        title: 'Chat with Trader — AAPL',
        created_at: '2026-04-01T00:00:00Z',
        updated_at: '2026-04-01T00:00:00Z',
      }],
      limit: 1,
      offset: 0,
    })

    await expect(
      client.createConversation({ pipeline_run_id: 'run-1', agent_role: 'trader' }),
    ).resolves.toEqual({
      id: 'conv-2',
      pipeline_run_id: 'run-1',
      agent_role: 'trader',
      title: 'Chat with Trader — AAPL',
      created_at: '2026-04-01T00:00:00Z',
      updated_at: '2026-04-01T00:00:00Z',
    })

    await expect(client.getConversationMessages('conv-2', { limit: 100 })).resolves.toEqual({
      data: [{
        id: 'msg-1',
        conversation_id: 'conv-2',
        role: 'assistant',
        content: 'Momentum is still strong.',
        created_at: '2026-04-01T00:01:00Z',
      }],
      limit: 100,
      offset: 0,
    })

    await expect(
      client.createConversationMessage('conv-2', { content: 'Should we keep the position?' }),
    ).resolves.toEqual({
      id: 'msg-2',
      conversation_id: 'conv-2',
      role: 'assistant',
      content: 'I still favor the long side.',
      created_at: '2026-04-01T00:02:00Z',
    })

    expect(fetchMock).toHaveBeenCalledTimes(4)

    const [listUrl, listInit] = fetchMock.mock.calls[0] as [URL, RequestInit]
    expect(listUrl.toString()).toBe(
      'http://localhost:8080/api/v1/conversations?pipeline_run_id=run-1&agent_role=trader&limit=1',
    )
    expect(listInit.method).toBeUndefined()

    const [createUrl, createInit] = fetchMock.mock.calls[1] as [URL, RequestInit]
    expect(createUrl.toString()).toBe('http://localhost:8080/api/v1/conversations')
    expect(createInit.method).toBe('POST')
    expect(createInit.body).toBe(JSON.stringify({ pipeline_run_id: 'run-1', agent_role: 'trader' }))

    const [messagesUrl, messagesInit] = fetchMock.mock.calls[2] as [URL, RequestInit]
    expect(messagesUrl.toString()).toBe('http://localhost:8080/api/v1/conversations/conv-2/messages?limit=100')
    expect(messagesInit.method).toBeUndefined()

    const [postUrl, postInit] = fetchMock.mock.calls[3] as [URL, RequestInit]
    expect(postUrl.toString()).toBe('http://localhost:8080/api/v1/conversations/conv-2/messages')
    expect(postInit.method).toBe('POST')
    expect(postInit.body).toBe(JSON.stringify({ content: 'Should we keep the position?' }))
  })
})
