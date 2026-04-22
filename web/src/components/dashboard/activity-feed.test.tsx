import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { act, cleanup, render, screen } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { ActivityFeed } from '@/components/dashboard/activity-feed'

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

function jsonResponse(body: unknown, status = 200) {
  return {
    ok: status >= 200 && status < 300,
    status,
    json: async () => body,
  }
}

describe('ActivityFeed', () => {
  beforeEach(() => {
    MockWebSocket.instances = []
    vi.stubGlobal('WebSocket', MockWebSocket)
    vi.stubGlobal(
      'fetch',
      vi.fn(() => Promise.resolve(jsonResponse({ data: [], limit: 20, offset: 0 }))),
    )
  })

  afterEach(() => {
    cleanup()
    vi.unstubAllGlobals()
  })

  it('renders historical events from the API before live websocket updates arrive', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(() =>
        Promise.resolve(
          jsonResponse({
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
        ),
      ),
    )

    render(<ActivityFeed />, { wrapper: Wrapper })

    expect(await screen.findByText('AAPL run kicked off')).toBeInTheDocument()
    expect(screen.getAllByText('Pipeline started').length).toBeGreaterThan(0)
    expect(screen.getByText(/Run: run-1/i)).toBeInTheDocument()
  })

  it('shows empty state when no events received', async () => {
    render(<ActivityFeed />, { wrapper: Wrapper })

    expect(screen.getByTestId('activity-feed')).toBeInTheDocument()
    expect(await screen.findByTestId('activity-feed-empty')).toBeInTheDocument()
  })

  it('shows connected badge after WebSocket opens', async () => {
    render(<ActivityFeed />, { wrapper: Wrapper })

    const ws = MockWebSocket.instances[0]
    expect(ws).toBeDefined()

    await vi.waitFor(() => {
      ws?.open()
      expect(screen.getByText('Connected')).toBeInTheDocument()
    })
  })

  it('renders pipeline health websocket events with a human-friendly label', async () => {
    render(<ActivityFeed />, { wrapper: Wrapper })

    const ws = MockWebSocket.instances[0]
    expect(ws).toBeDefined()

    await act(async () => {
      ws?.open()
      ws?.onmessage?.(
        new MessageEvent('message', {
          data: JSON.stringify({
            type: 'pipeline_health',
            strategy_id: '11111111-1111-1111-1111-111111111111',
            timestamp: '2026-04-21T13:45:00.000Z',
          }),
        }),
      )
    })

    expect(screen.getAllByText('Pipeline health').length).toBeGreaterThan(0)
  })
})
