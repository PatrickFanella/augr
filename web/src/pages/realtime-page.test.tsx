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

function jsonResponse(body: unknown, status = 200) {
  return {
    ok: status >= 200 && status < 300,
    status,
    json: async () => body,
  }
}

function listResponse<T>(data: T[], limit: number, offset = 0) {
  return { data, limit, offset }
}

function stubFetch(...responses: Array<ReturnType<typeof jsonResponse>>) {
  const fetchMock = vi.fn()
  responses.forEach((response) => {
    fetchMock.mockResolvedValueOnce(response)
  })
  vi.stubGlobal('fetch', fetchMock)
  return fetchMock
}

const baseEvent = {
  id: 'evt-1',
  pipeline_run_id: 'run-1',
  strategy_id: 'strategy-1',
  agent_role: 'trader' as const,
  event_kind: 'signal',
  title: 'Signal emitted',
  summary: 'Buy signal generated',
  created_at: '2025-01-01T00:00:00Z',
}

const secondaryEvent = {
  id: 'evt-2',
  pipeline_run_id: 'run-2',
  strategy_id: 'strategy-2',
  event_kind: 'error',
  title: 'Pipeline failed',
  summary: 'Retry exhausted',
  created_at: '2025-01-01T00:01:00Z',
}

const traderConversation = {
  id: 'conv-1',
  pipeline_run_id: 'run-1',
  agent_role: 'trader' as const,
  title: 'Chat with Trader — AAPL',
  created_at: '2025-01-01T00:00:00Z',
  updated_at: '2025-01-01T00:02:00Z',
}

const riskConversation = {
  id: 'conv-2',
  pipeline_run_id: 'run-2',
  agent_role: 'risk_manager' as const,
  title: 'Chat with Risk Manager — TSLA',
  created_at: '2025-01-01T00:03:00Z',
  updated_at: '2025-01-01T00:04:00Z',
}

const traderRun = {
  id: 'run-1',
  strategy_id: 'strategy-1',
  ticker: 'AAPL',
  trade_date: '2025-01-01T00:00:00Z',
  status: 'completed' as const,
  signal: 'buy' as const,
  started_at: '2025-01-01T00:00:00Z',
  completed_at: '2025-01-01T00:02:00Z',
}

const riskRun = {
  id: 'run-2',
  strategy_id: 'strategy-2',
  ticker: 'TSLA',
  trade_date: '2025-01-02T00:00:00Z',
  status: 'completed' as const,
  signal: 'sell' as const,
  started_at: '2025-01-02T00:00:00Z',
  completed_at: '2025-01-02T00:03:00Z',
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
    stubFetch(
      jsonResponse(listResponse([baseEvent], 50)),
      jsonResponse(listResponse([], 50)),
    )

    render(<RealtimePage />, { wrapper: Wrapper })

    expect(await screen.findByTestId('event-card-evt-1')).toBeInTheDocument()
    expect(screen.getAllByText('Signal emitted').length).toBeGreaterThan(0)
    expect(screen.getAllByText('Buy signal generated').length).toBeGreaterThan(0)
    expect(screen.getAllByText('trader').length).toBeGreaterThan(0)
  })

  it('renders selected event details when a card is clicked', async () => {
    stubFetch(
      jsonResponse(listResponse([{ ...baseEvent, metadata: { confidence: 0.92 } }, secondaryEvent], 50)),
      jsonResponse(listResponse([], 50)),
    )

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
    stubFetch(
      jsonResponse(listResponse([], 50)),
      jsonResponse(listResponse([], 50)),
    )

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
    stubFetch(
      jsonResponse(listResponse([], 50)),
      jsonResponse(listResponse([], 50)),
    )

    render(<RealtimePage />, { wrapper: Wrapper })

    expect(await screen.findByTestId('realtime-empty')).toBeInTheDocument()
  })

  it('loads an existing conversation for the selected event', async () => {
    const fetchMock = stubFetch(
      jsonResponse(listResponse([baseEvent], 50)),
      jsonResponse(listResponse([traderConversation, riskConversation], 50)),
      jsonResponse(listResponse([
        {
          id: 'msg-1',
          conversation_id: 'conv-1',
          role: 'assistant',
          content: 'Existing conversation answer.',
          created_at: '2025-01-01T00:01:00Z',
        },
      ], 100)),
    )

    render(<RealtimePage />, { wrapper: Wrapper })

    expect(await screen.findByText('Existing conversation answer.')).toBeInTheDocument()
    expect(fetchMock).toHaveBeenNthCalledWith(
      2,
      expect.objectContaining({ href: 'http://localhost:8080/api/v1/conversations?limit=50' }),
      expect.any(Object),
    )
    expect(fetchMock).toHaveBeenNthCalledWith(
      3,
      expect.objectContaining({ href: 'http://localhost:8080/api/v1/conversations/conv-1/messages?limit=100' }),
      expect.any(Object),
    )
  })

  it('switches conversation history from the selector without changing selected event details', async () => {
    const fetchMock = stubFetch(
      jsonResponse(listResponse([baseEvent], 50)),
      jsonResponse(listResponse([traderConversation, riskConversation], 50)),
      jsonResponse(listResponse([
        {
          id: 'msg-1',
          conversation_id: 'conv-1',
          role: 'assistant',
          content: 'Trader context answer.',
          created_at: '2025-01-01T00:01:00Z',
        },
      ], 100)),
      jsonResponse(listResponse([
        {
          id: 'msg-2',
          conversation_id: 'conv-2',
          role: 'assistant',
          content: 'Risk manager answer.',
          created_at: '2025-01-01T00:05:00Z',
        },
      ], 100)),
    )

    render(<RealtimePage />, { wrapper: Wrapper })

    expect(await screen.findByText('Trader context answer.')).toBeInTheDocument()
    fireEvent.change(screen.getByTestId('conversation-selector'), { target: { value: 'conv-2' } })

    expect(await screen.findByText('Risk manager answer.')).toBeInTheDocument()
    expect(screen.getByTestId('selected-event-panel')).toHaveTextContent('Signal emitted')
    expect(screen.getByTestId('conversation-context-note')).toHaveTextContent('Viewing conversation outside selected event context.')
    expect(fetchMock).toHaveBeenNthCalledWith(
      4,
      expect.objectContaining({ href: 'http://localhost:8080/api/v1/conversations/conv-2/messages?limit=100' }),
      expect.any(Object),
    )
  })

  it('creates a conversation from the new conversation form and opens it', async () => {
    const fetchMock = stubFetch(
      jsonResponse(listResponse([baseEvent], 50)),
      jsonResponse(listResponse([riskConversation], 50)),
      jsonResponse(listResponse([traderRun, riskRun], 50)),
      jsonResponse({
        id: 'conv-3',
        pipeline_run_id: 'run-2',
        agent_role: 'risk_manager',
        title: 'Chat with Risk Manager — TSLA',
        created_at: '2025-01-02T00:03:00Z',
        updated_at: '2025-01-02T00:03:00Z',
      }, 201),
    )

    render(<RealtimePage />, { wrapper: Wrapper })

    const selector = await screen.findByTestId('conversation-selector')
    await waitFor(() => expect(selector).toHaveValue(''))
    fireEvent.change(selector, { target: { value: '__new__' } })

    expect(await screen.findByTestId('new-conversation-form')).toBeInTheDocument()
    await waitFor(() => expect(screen.getByTestId('new-conversation-run')).toHaveTextContent('TSLA'))
    fireEvent.change(screen.getByTestId('new-conversation-run'), { target: { value: 'run-2' } })
    fireEvent.change(screen.getByTestId('new-conversation-agent-role'), { target: { value: 'risk_manager' } })
    fireEvent.click(screen.getByTestId('new-conversation-submit'))

    await waitFor(() => expect(screen.getByTestId('conversation-selector')).toHaveValue('conv-3'))
    expect(screen.getByText('No messages yet.')).toBeInTheDocument()
    expect(fetchMock).toHaveBeenNthCalledWith(
      3,
      expect.objectContaining({ href: 'http://localhost:8080/api/v1/runs?limit=50' }),
      expect.any(Object),
    )
    expect(fetchMock).toHaveBeenNthCalledWith(
      4,
      expect.objectContaining({ href: 'http://localhost:8080/api/v1/conversations' }),
      expect.objectContaining({
        method: 'POST',
        body: JSON.stringify({ pipeline_run_id: 'run-2', agent_role: 'risk_manager' }),
      }),
    )
  })

  it('creates a conversation on first send and renders refreshed messages', async () => {
    const fetchMock = stubFetch(
      jsonResponse(listResponse([baseEvent], 50)),
      jsonResponse(listResponse([], 50)),
      jsonResponse({
        id: 'conv-1',
        pipeline_run_id: 'run-1',
        agent_role: 'trader',
        title: 'Chat with Trader — AAPL',
        created_at: '2025-01-01T00:00:00Z',
        updated_at: '2025-01-01T00:00:00Z',
      }, 201),
      jsonResponse({
        id: 'msg-2',
        conversation_id: 'conv-1',
        role: 'assistant',
        content: 'Momentum still supports the long.',
        created_at: '2025-01-01T00:01:00Z',
      }, 201),
      jsonResponse(listResponse([
        {
          id: 'msg-1',
          conversation_id: 'conv-1',
          role: 'user',
          content: 'Do you still like the setup?',
          created_at: '2025-01-01T00:00:30Z',
        },
        {
          id: 'msg-2',
          conversation_id: 'conv-1',
          role: 'assistant',
          content: 'Momentum still supports the long.',
          created_at: '2025-01-01T00:01:00Z',
        },
      ], 100)),
    )

    render(<RealtimePage />, { wrapper: Wrapper })

    const input = await screen.findByTestId('chat-input')
    await waitFor(() => expect(input).toBeEnabled())
    fireEvent.change(input, { target: { value: 'Do you still like the setup?' } })
    fireEvent.click(screen.getByTestId('chat-send-button'))

    expect(await screen.findByText('Do you still like the setup?')).toBeInTheDocument()
    expect(await screen.findByText('Momentum still supports the long.')).toBeInTheDocument()
    expect(screen.getByTestId('selected-event-panel')).toHaveTextContent('Signal emitted')

    expect(fetchMock).toHaveBeenNthCalledWith(
      3,
      expect.objectContaining({ href: 'http://localhost:8080/api/v1/conversations' }),
      expect.objectContaining({ method: 'POST' }),
    )
    expect(fetchMock).toHaveBeenNthCalledWith(
      4,
      expect.objectContaining({ href: 'http://localhost:8080/api/v1/conversations/conv-1/messages' }),
      expect.objectContaining({ method: 'POST' }),
    )
    expect(fetchMock).toHaveBeenNthCalledWith(
      5,
      expect.objectContaining({ href: 'http://localhost:8080/api/v1/conversations/conv-1/messages?limit=100' }),
      expect.any(Object),
    )
  })

  it('shows an inline chat error when sending fails', async () => {
    stubFetch(
      jsonResponse(listResponse([baseEvent], 50)),
      jsonResponse(listResponse([], 50)),
      {
        ok: false,
        status: 500,
        json: async () => ({ error: 'LLM completion failed', code: 'ERR_INTERNAL' }),
      } as ReturnType<typeof jsonResponse>,
    )

    render(<RealtimePage />, { wrapper: Wrapper })

    const input = await screen.findByTestId('chat-input')
    await waitFor(() => expect(input).toBeEnabled())
    fireEvent.change(input, { target: { value: 'Why is the model hesitant?' } })
    fireEvent.click(screen.getByTestId('chat-send-button'))

    expect(await screen.findByText('LLM completion failed')).toBeInTheDocument()
    expect(screen.queryByText('Why is the model hesitant?')).not.toBeInTheDocument()
    expect(screen.getByTestId('selected-event-panel')).toHaveTextContent('Buy signal generated')
  })
})
