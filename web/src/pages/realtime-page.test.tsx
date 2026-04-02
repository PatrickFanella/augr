import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { act, cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { RealtimePage } from '@/pages/realtime-page'

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

  emitMessage(payload: unknown) {
    this.onmessage?.({ data: JSON.stringify(payload) } as MessageEvent)
  }
}

function Wrapper({ children }: { children: React.ReactNode }) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return <QueryClientProvider client={client}>{children}</QueryClientProvider>
}

describe('RealtimePage', () => {
  beforeEach(() => {
    MockWebSocket.instances = []
    vi.stubGlobal('WebSocket', MockWebSocket)
  })

  afterEach(() => {
    cleanup()
    vi.unstubAllGlobals()
  })

  it('renders historical event cards from the API', async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      json: async () => ({
        data: [
          {
            id: 'evt-1',
            pipeline_run_id: 'run-1',
            strategy_id: 'strategy-1',
            agent_role: 'trader',
            event_kind: 'signal',
            title: 'Signal emitted',
            summary: 'Buy signal generated',
            created_at: '2025-01-01T00:00:00Z',
          },
        ],
        limit: 50,
        offset: 0,
      }),
    })
    vi.stubGlobal('fetch', fetchMock)

    render(<RealtimePage />, { wrapper: Wrapper })

    expect(await screen.findByTestId('event-card-evt-1')).toBeInTheDocument()
    expect(screen.getAllByText('Signal emitted').length).toBeGreaterThan(0)
    expect(screen.getAllByText('Buy signal generated').length).toBeGreaterThan(0)
    expect(screen.getAllByText('trader').length).toBeGreaterThan(0)
  })

  it('renders selected event details when a card is clicked', async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      json: async () => ({
        data: [
          {
            id: 'evt-1',
            pipeline_run_id: 'run-1',
            strategy_id: 'strategy-1',
            agent_role: 'trader',
            event_kind: 'signal',
            title: 'Signal emitted',
            summary: 'Buy signal generated',
            metadata: { confidence: 0.92 },
            created_at: '2025-01-01T00:00:00Z',
          },
          {
            id: 'evt-2',
            pipeline_run_id: 'run-2',
            strategy_id: 'strategy-2',
            event_kind: 'error',
            title: 'Pipeline failed',
            summary: 'Retry exhausted',
            created_at: '2025-01-01T00:01:00Z',
          },
        ],
        limit: 50,
        offset: 0,
      }),
    })
    vi.stubGlobal('fetch', fetchMock)

    render(<RealtimePage />, { wrapper: Wrapper })

    const card = await screen.findByTestId('event-card-evt-1')
    fireEvent.click(card)

    const panel = await screen.findByTestId('selected-event-panel')
    expect(within(panel).getByText('Signal emitted')).toBeInTheDocument()
    expect(within(panel).getByText('Buy signal generated')).toBeInTheDocument()
    expect(within(panel).getByText('run-1')).toBeInTheDocument()
    expect(within(panel).getByText('strategy-1')).toBeInTheDocument()
    expect(screen.getByTestId('selected-event-metadata')).toHaveTextContent('confidence')
  })

  it('appends live websocket events to the feed', async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      json: async () => ({ data: [], limit: 50, offset: 0 }),
    })
    vi.stubGlobal('fetch', fetchMock)

    render(<RealtimePage />, { wrapper: Wrapper })

    expect(MockWebSocket.instances).toHaveLength(1)

    act(() => {
      MockWebSocket.instances[0]?.open()
      MockWebSocket.instances[0]?.emitMessage({
        type: 'signal',
        strategy_id: 'strategy-live',
        run_id: 'run-live',
        timestamp: '2025-01-01T00:02:00Z',
        data: { signal: 'buy', agent_role: 'trader' },
      })
    })

    await waitFor(() => {
      expect(screen.getByText('run-live')).toBeInTheDocument()
    })
    expect(screen.getAllByText('trader').length).toBeGreaterThan(0)
  })

  it('renders empty state when there are no events yet', async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      json: async () => ({ data: [], limit: 50, offset: 0 }),
    })
    vi.stubGlobal('fetch', fetchMock)

    render(<RealtimePage />, { wrapper: Wrapper })

    expect(await screen.findByTestId('realtime-empty')).toBeInTheDocument()
  })
})
