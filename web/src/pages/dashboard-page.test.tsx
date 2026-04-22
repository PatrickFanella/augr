import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { cleanup, render, screen } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { DashboardPage } from '@/pages/dashboard-page'

class MockWebSocket {
  static instances: MockWebSocket[] = []
  static CONNECTING = 0
  static OPEN = 1
  static CLOSING = 2
  static CLOSED = 3

  readyState = MockWebSocket.CONNECTING
  url: string
  onopen: (() => void) | null = null
  onmessage: ((event: MessageEvent) => void) | null = null
  onerror: ((event: Event) => void) | null = null
  onclose: (() => void) | null = null
  send = vi.fn()

  constructor(url: string) {
    this.url = url
    MockWebSocket.instances.push(this)
  }

  close() {
    this.readyState = MockWebSocket.CLOSED
    this.onclose?.()
  }

  open() {
    this.readyState = MockWebSocket.OPEN
    this.onopen?.()
  }
}

function Wrapper({ children }: { children: React.ReactNode }) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return <QueryClientProvider client={client}>{children}</QueryClientProvider>
}

describe('DashboardPage', () => {
  beforeEach(() => {
    MockWebSocket.instances = []
    vi.stubGlobal('WebSocket', MockWebSocket)
  })

  afterEach(() => {
    cleanup()
    vi.unstubAllGlobals()
  })

  it('renders dashboard activity and recent runs from the API', async () => {
    const fetchMock = vi.fn((input: RequestInfo | URL) => {
      const url = typeof input === 'string' ? input : input.toString()

      if (url.includes('portfolio')) {
        return Promise.resolve({
          ok: true,
          status: 200,
          json: async () => ({ open_positions: 0, unrealized_pnl: 0, realized_pnl: 0 }),
        })
      }

      if (url.includes('/api/v1/runs')) {
        return Promise.resolve({
          ok: true,
          status: 200,
          json: async () => ({
            data: [
              {
                id: 'run-1',
                strategy_id: 'strategy-1',
                ticker: 'AAPL',
                trade_date: '2026-04-22T00:00:00Z',
                status: 'completed',
                signal: 'buy',
                started_at: '2026-04-22T00:00:00Z',
                completed_at: '2026-04-22T00:03:00Z',
              },
            ],
            limit: 10,
            offset: 0,
          }),
        })
      }

      if (url.includes('/api/v1/events')) {
        return Promise.resolve({
          ok: true,
          status: 200,
          json: async () => ({
            data: [
              {
                id: 'evt-1',
                pipeline_run_id: 'run-1',
                strategy_id: 'strategy-1',
                event_kind: 'pipeline_started',
                title: 'Pipeline started',
                summary: 'AAPL run kicked off',
                created_at: '2026-04-22T00:00:00Z',
              },
            ],
            limit: 20,
            offset: 0,
          }),
        })
      }

      if (url.includes('strateg')) {
        return Promise.resolve({
          ok: true,
          status: 200,
          json: async () => ({ data: [], limit: 20, offset: 0 }),
        })
      }

      if (url.includes('risk')) {
        return Promise.resolve({
          ok: true,
          status: 200,
          json: async () => ({
            risk_status: 'normal',
            circuit_breaker: { state: 'open', reason: '' },
            kill_switch: { active: false, reason: '', mechanisms: [], activated_at: null },
            position_limits: { max_per_position_pct: 10, max_total_pct: 80, max_concurrent: 5, max_per_market_pct: 40 },
            updated_at: '2025-01-01T00:00:00Z',
          }),
        })
      }

      return Promise.reject(new Error(`Unhandled fetch URL in test: ${url}`))
    })
    vi.stubGlobal('fetch', fetchMock)

    render(<DashboardPage />, { wrapper: Wrapper })

    expect(await screen.findByTestId('dashboard-page')).toBeInTheDocument()
    expect(await screen.findByText('AAPL run kicked off')).toBeInTheDocument()
    expect(screen.getAllByText('Pipeline started').length).toBeGreaterThan(0)
    expect(await screen.findByText('Recent runs')).toBeInTheDocument()
    expect(await screen.findByText('AAPL')).toBeInTheDocument()
  })

  it('renders when the active strategies data array is null', async () => {
    vi.useRealTimers()
    const fetchMock = vi.fn((input: RequestInfo | URL) => {
      const url = typeof input === 'string' ? input : input.toString()

      if (url.includes('portfolio')) {
        return Promise.resolve({
          ok: true,
          status: 200,
          json: async () => ({ open_positions: 0, unrealized_pnl: 0, realized_pnl: 0 }),
        })
      }

      if (url.includes('strateg')) {
        return Promise.resolve({
          ok: true,
          status: 200,
          json: async () => ({ data: null, limit: 20, offset: 0 }),
        })
      }

      if (url.includes('risk')) {
        return Promise.resolve({
          ok: true,
          status: 200,
          json: async () => ({
            risk_status: 'normal',
            circuit_breaker: { state: 'open', reason: '' },
            kill_switch: { active: false, reason: '', mechanisms: [], activated_at: null },
            position_limits: { max_per_position_pct: 10, max_total_pct: 80, max_concurrent: 5, max_per_market_pct: 40 },
            updated_at: '2025-01-01T00:00:00Z',
          }),
        })
      }

      return Promise.reject(new Error(`Unhandled fetch URL in test: ${url}`))
    })
    vi.stubGlobal('fetch', fetchMock)

    render(<DashboardPage />, { wrapper: Wrapper })

    expect(await screen.findByTestId('dashboard-page')).toBeInTheDocument()
    expect(await screen.findByText('No active strategies')).toBeInTheDocument()
  })
})
