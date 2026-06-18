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

  it('serializes order market type filters', async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      json: async () => ({ data: [], limit: 25, offset: 0 }),
    })
    vi.stubGlobal('fetch', fetchMock)

    const client = new ApiClient({ baseUrl: 'http://localhost:8080' })
    await client.listOrders({ market_type: 'crypto', ticker: 'BTCUSD', limit: 25 })

    expect(fetchMock).toHaveBeenCalledTimes(1)
    const [requestUrl] = fetchMock.mock.calls[0] as [URL, RequestInit]
    expect(requestUrl.toString()).toBe(
      'http://localhost:8080/api/v1/orders?market_type=crypto&ticker=BTCUSD&limit=25',
    )
  })

  it('supports research scanner endpoints with query params', async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce({
        ok: true,
        status: 200,
        json: async () => ({ data: [], limit: 10, offset: 0 }),
      })
      .mockResolvedValueOnce({
        ok: true,
        status: 200,
        json: async () => ({ data: [], limit: 25, offset: 0 }),
      })
    vi.stubGlobal('fetch', fetchMock)

    const client = new ApiClient({ baseUrl: 'http://localhost:8080' })

    await expect(
      client.listOptionsOpportunities('AAPL', {
        limit: 10,
        strategy_id: 'strategy-1',
        expiry: '2026-07-17',
        type: 'call',
      }),
    ).resolves.toEqual({ data: [], limit: 10, offset: 0 })

    await expect(
      client.listPolymarketOpportunities({
        limit: 25,
        strategy_id: 'strategy-1',
        slug: 'election-2028',
        token_id: 'token-123',
        outcome: 'YES',
        best_bid: 0.41,
        best_ask: 0.43,
        probability: 0.38,
        ask_depth_usd: 1200,
        ask_size: 3,
      }),
    ).resolves.toEqual({ data: [], limit: 25, offset: 0 })

    expect(fetchMock).toHaveBeenCalledTimes(2)

    const [optionsUrl] = fetchMock.mock.calls[0] as [URL, RequestInit]
    expect(optionsUrl.toString()).toBe(
      'http://localhost:8080/api/v1/research/options/opportunities/AAPL?limit=10&strategy_id=strategy-1&expiry=2026-07-17&type=call',
    )

    const [polymarketUrl] = fetchMock.mock.calls[1] as [URL, RequestInit]
    expect(polymarketUrl.toString()).toBe(
      'http://localhost:8080/api/v1/research/polymarket/opportunities?limit=25&strategy_id=strategy-1&slug=election-2028&token_id=token-123&outcome=YES&best_bid=0.41&best_ask=0.43&probability=0.38&ask_depth_usd=1200&ask_size=3',
    )
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

  it('fetches the Kalshi summary endpoint', async () => {
    const payload = {
      watched_markets: [],
      latest_snapshots: [],
      discovery: { last_run: null, status: 'not_started' },
      strategies: { active_paper: 0 },
    }
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      json: async () => payload,
    })
    vi.stubGlobal('fetch', fetchMock)

    const client = new ApiClient({ baseUrl: 'http://localhost:8080', token: 'jwt-token' })

    await expect(client.getKalshiSummary()).resolves.toEqual(payload)
    expect(fetchMock).toHaveBeenCalledTimes(1)
    const [requestUrl, requestInit] = fetchMock.mock.calls[0] as [URL, RequestInit]
    expect(requestUrl.toString()).toBe('http://localhost:8080/api/v1/kalshi/summary')
    expect(new Headers(requestInit.headers).get('Authorization')).toBe('Bearer jwt-token')
  })

  it('fetches the risk cockpit summary from the authenticated endpoint', async () => {
    const payload = {
      generated_at: '2026-06-09T12:00:00Z',
      kill_switch_active: false,
      circuit_breaker: true,
      exposures: [],
      warnings: ['Cross-flow limits approaching'],
    }
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      json: async () => payload,
    })
    vi.stubGlobal('fetch', fetchMock)

    const client = new ApiClient({ baseUrl: 'http://localhost:8080', token: 'jwt-token' })

    await expect(client.getRiskCockpit()).resolves.toEqual(payload)

    expect(fetchMock).toHaveBeenCalledTimes(1)
    const [requestUrl, requestInit] = fetchMock.mock.calls[0] as [URL, RequestInit]
    expect(requestUrl.toString()).toBe('http://localhost:8080/api/v1/risk/cockpit')
    expect(new Headers(requestInit.headers).get('Authorization')).toBe('Bearer jwt-token')
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

  it('supports trade decision journal endpoints', async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce({
        ok: true,
        status: 200,
        json: async () => ({
          data: [
            {
              id: 'decision-1',
              market_type: 'polymarket',
              instrument_key: 'pm:market-123:YES',
              side: 'buy',
              fair_value: 0.52,
              executable_price: 0.51,
              spread: 0.01,
              depth: 100,
              gross_ev: 0.12,
              net_ev: 0.08,
              kelly_fraction: 0.25,
              proposed_size: 50,
              approved_size: 25,
              risk_status: 'approved',
              risk_reasons: [],
              regime_tags: ['momentum'],
              status: 'paper_ordered',
              created_at: '2026-06-08T12:00:00Z',
              updated_at: '2026-06-08T12:01:00Z',
            },
          ],
          limit: 100,
          offset: 0,
        }),
      })
      .mockResolvedValueOnce({
        ok: true,
        status: 200,
        json: async () => ({
          id: 'decision-1',
          market_type: 'polymarket',
          instrument_key: 'pm:market-123:YES',
          side: 'buy',
          fair_value: 0.52,
          executable_price: 0.51,
          spread: 0.01,
          depth: 100,
          gross_ev: 0.12,
          net_ev: 0.08,
          kelly_fraction: 0.25,
          proposed_size: 50,
          approved_size: 25,
          risk_status: 'approved',
          risk_reasons: [],
          regime_tags: ['momentum'],
          status: 'paper_ordered',
          created_at: '2026-06-08T12:00:00Z',
          updated_at: '2026-06-08T12:01:00Z',
        }),
      })
    vi.stubGlobal('fetch', fetchMock)

    const client = new ApiClient({ baseUrl: 'http://localhost:8080' })

    await expect(
      client.listTradeDecisions({ market_type: 'polymarket', status: 'paper_ordered', limit: 100 }),
    ).resolves.toEqual({
      data: [
        {
          id: 'decision-1',
          market_type: 'polymarket',
          instrument_key: 'pm:market-123:YES',
          side: 'buy',
          fair_value: 0.52,
          executable_price: 0.51,
          spread: 0.01,
          depth: 100,
          gross_ev: 0.12,
          net_ev: 0.08,
          kelly_fraction: 0.25,
          proposed_size: 50,
          approved_size: 25,
          risk_status: 'approved',
          risk_reasons: [],
          regime_tags: ['momentum'],
          status: 'paper_ordered',
          created_at: '2026-06-08T12:00:00Z',
          updated_at: '2026-06-08T12:01:00Z',
        },
      ],
      limit: 100,
      offset: 0,
    })

    await expect(client.getTradeDecision('decision-1')).resolves.toEqual({
      id: 'decision-1',
      market_type: 'polymarket',
      instrument_key: 'pm:market-123:YES',
      side: 'buy',
      fair_value: 0.52,
      executable_price: 0.51,
      spread: 0.01,
      depth: 100,
      gross_ev: 0.12,
      net_ev: 0.08,
      kelly_fraction: 0.25,
      proposed_size: 50,
      approved_size: 25,
      risk_status: 'approved',
      risk_reasons: [],
      regime_tags: ['momentum'],
      status: 'paper_ordered',
      created_at: '2026-06-08T12:00:00Z',
      updated_at: '2026-06-08T12:01:00Z',
    })

    expect(fetchMock).toHaveBeenCalledTimes(2)
    const [listUrl] = fetchMock.mock.calls[0] as [URL, RequestInit]
    expect(listUrl.toString()).toBe(
      'http://localhost:8080/api/v1/journal/decisions?market_type=polymarket&status=paper_ordered&limit=100',
    )

    const [detailUrl] = fetchMock.mock.calls[1] as [URL, RequestInit]
    expect(detailUrl.toString()).toBe('http://localhost:8080/api/v1/journal/decisions/decision-1')
  })

  it('supports replay workbench endpoint', async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      json: async () => ({
        source: {
          id: 'decision-1',
          market_type: 'polymarket',
          instrument_key: 'pm:market-123:YES',
          side: 'buy',
          fair_value: 0.52,
          executable_price: 0.51,
          spread: 0.01,
          depth: 100,
          gross_ev: 0.12,
          net_ev: 0.08,
          kelly_fraction: 0.25,
          proposed_size: 50,
          approved_size: 25,
          risk_status: 'approved',
          risk_reasons: [],
          regime_tags: ['momentum'],
          status: 'paper_ordered',
          created_at: '2026-06-08T12:00:00Z',
          updated_at: '2026-06-08T12:01:00Z',
        },
        events: [],
        summary: {
          event_count: 0,
          has_paper_order: false,
          has_live_order: false,
          has_fill: false,
          has_outcome: false,
          latest_status: 'candidate',
          total_approved_size: 0,
          total_net_ev: 0,
          rejection_count: 0,
        },
      }),
    })
    vi.stubGlobal('fetch', fetchMock)

    const client = new ApiClient({ baseUrl: 'http://localhost:8080' })

    await expect(client.getDecisionReplay('decision-1')).resolves.toEqual({
      source: {
        id: 'decision-1',
        market_type: 'polymarket',
        instrument_key: 'pm:market-123:YES',
        side: 'buy',
        fair_value: 0.52,
        executable_price: 0.51,
        spread: 0.01,
        depth: 100,
        gross_ev: 0.12,
        net_ev: 0.08,
        kelly_fraction: 0.25,
        proposed_size: 50,
        approved_size: 25,
        risk_status: 'approved',
        risk_reasons: [],
        regime_tags: ['momentum'],
        status: 'paper_ordered',
        created_at: '2026-06-08T12:00:00Z',
        updated_at: '2026-06-08T12:01:00Z',
      },
      events: [],
      summary: {
        event_count: 0,
        has_paper_order: false,
        has_live_order: false,
        has_fill: false,
        has_outcome: false,
        latest_status: 'candidate',
        total_approved_size: 0,
        total_net_ev: 0,
        rejection_count: 0,
      },
    })

    expect(fetchMock).toHaveBeenCalledTimes(1)
    const [detailUrl] = fetchMock.mock.calls[0] as [URL, RequestInit]
    expect(detailUrl.toString()).toBe('http://localhost:8080/api/v1/replay/decisions/decision-1')
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
