import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { StrategyDetailPage } from '@/pages/strategy-detail-page'

const strategyId = '00000000-0000-0000-0000-000000000001'

function Wrapper({ children }: { children: React.ReactNode }) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return (
    <QueryClientProvider client={client}>
      <MemoryRouter initialEntries={[`/strategies/${strategyId}`]}>
        <Routes>
          <Route path="strategies/:id" element={children} />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>
  )
}

type StrategyFixture = {
  id: string
  name: string
  description?: string
  ticker: string
  market_type: 'stock' | 'crypto' | 'polymarket'
  status: 'active' | 'paused' | 'inactive'
  skip_next_run: boolean
  is_paper: boolean
  schedule_cron?: string
  config: Record<string, unknown>
  created_at: string
  updated_at: string
}

type MutationStub = {
  status: number
  body: StrategyFixture | { error: string; code?: string }
}

function createStrategy(overrides: Partial<StrategyFixture> = {}): StrategyFixture {
  return {
    id: strategyId,
    name: 'AAPL Momentum',
    description: 'A momentum-based strategy',
    ticker: 'AAPL',
    market_type: 'stock',
    status: 'active',
    skip_next_run: false,
    is_paper: false,
    schedule_cron: '0 9 * * 1-5',
    config: { analysts: ['market'] },
    created_at: '2025-01-01T00:00:00Z',
    updated_at: '2025-01-01T00:00:00Z',
    ...overrides,
  }
}

function createResponse(body: unknown, status = 200) {
  return {
    ok: status >= 200 && status < 300,
    status,
    json: async () => body,
  }
}

function stubStrategyFetch({
  strategy = createStrategy(),
  pauseResult,
  resumeResult,
  skipResult,
}: {
  strategy?: StrategyFixture
  pauseResult?: MutationStub
  resumeResult?: MutationStub
  skipResult?: MutationStub
} = {}) {
  let currentStrategy = strategy
  const runs = { data: [], limit: 20, offset: 0 }

  const fetchMock = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
    const url = typeof input === 'string' ? input : input.toString()

    if (url.includes('/runs')) {
      return createResponse(runs)
    }

    if (url.includes(`/api/v1/strategies/${strategyId}/pause`)) {
      const result = pauseResult ?? { status: 200, body: { ...currentStrategy, status: 'paused' as const } }
      if (result.status >= 200 && result.status < 300 && 'status' in result.body) {
        currentStrategy = result.body
      }
      return createResponse(result.body, result.status)
    }

    if (url.includes(`/api/v1/strategies/${strategyId}/resume`)) {
      const result = resumeResult ?? { status: 200, body: { ...currentStrategy, status: 'active' as const } }
      if (result.status >= 200 && result.status < 300 && 'status' in result.body) {
        currentStrategy = result.body
      }
      return createResponse(result.body, result.status)
    }

    if (url.includes(`/api/v1/strategies/${strategyId}/skip-next`)) {
      const result = skipResult ?? { status: 200, body: { ...currentStrategy, skip_next_run: true } }
      if (result.status >= 200 && result.status < 300 && 'status' in result.body) {
        currentStrategy = result.body
      }
      return createResponse(result.body, result.status)
    }

    return createResponse(currentStrategy)
  })

  vi.stubGlobal('fetch', fetchMock)
  return fetchMock
}

function renderPage() {
  render(<StrategyDetailPage />, { wrapper: Wrapper })
}

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})

describe('StrategyDetailPage', () => {
  it('renders active strategies with the correct lifecycle action matrix', async () => {
    stubStrategyFetch()

    renderPage()

    expect(await screen.findByText('AAPL Momentum')).toBeInTheDocument()
    expect(screen.getByText('A momentum-based strategy')).toBeInTheDocument()
    expect(screen.getByText('AAPL')).toBeInTheDocument()
    expect(screen.getByTestId('strategy-detail-page')).toBeInTheDocument()
    expect(screen.getByTestId('strategy-status-badge')).toHaveTextContent('active')
    expect(screen.getByTestId('run-strategy-button')).toBeInTheDocument()
    expect(screen.getByTestId('pause-strategy-button')).toBeEnabled()
    expect(screen.getByTestId('resume-strategy-button')).toBeDisabled()
    expect(screen.getByTestId('skip-next-button')).toBeEnabled()
    expect(screen.getByTestId('delete-strategy-button')).toBeInTheDocument()
  })

  it('renders paused strategies with the correct lifecycle action matrix', async () => {
    stubStrategyFetch({ strategy: createStrategy({ status: 'paused', name: 'Paused Strategy' }) })

    renderPage()

    expect(await screen.findByText('Paused Strategy')).toBeInTheDocument()
    expect(screen.getByTestId('pause-strategy-button')).toBeDisabled()
    expect(screen.getByTestId('resume-strategy-button')).toBeEnabled()
    expect(screen.getByTestId('skip-next-button')).toBeDisabled()
  })

  it('renders inactive strategies with all lifecycle actions disabled', async () => {
    stubStrategyFetch({ strategy: createStrategy({ status: 'inactive', name: 'Inactive Strategy' }) })

    renderPage()

    expect(await screen.findByText('Inactive Strategy')).toBeInTheDocument()
    expect(screen.getByTestId('pause-strategy-button')).toBeDisabled()
    expect(screen.getByTestId('resume-strategy-button')).toBeDisabled()
    expect(screen.getByTestId('skip-next-button')).toBeDisabled()
  })

  it('pauses an active strategy through the pause endpoint and refreshes the action state', async () => {
    const fetchMock = stubStrategyFetch()

    renderPage()

    fireEvent.click(await screen.findByTestId('pause-strategy-button'))

    await waitFor(() => {
      expect(fetchMock).toHaveBeenCalledWith(
        expect.objectContaining({ pathname: `/api/v1/strategies/${strategyId}/pause` }),
        expect.objectContaining({ method: 'POST' }),
      )
    })

    await waitFor(() => {
      expect(screen.getByTestId('strategy-status-badge')).toHaveTextContent('paused')
      expect(screen.getByTestId('pause-strategy-button')).toBeDisabled()
      expect(screen.getByTestId('resume-strategy-button')).toBeEnabled()
      expect(screen.getByTestId('skip-next-button')).toBeDisabled()
    })
  })

  it('resumes a paused strategy through the resume endpoint', async () => {
    const fetchMock = stubStrategyFetch({ strategy: createStrategy({ status: 'paused' }) })

    renderPage()

    fireEvent.click(await screen.findByTestId('resume-strategy-button'))

    await waitFor(() => {
      expect(fetchMock).toHaveBeenCalledWith(
        expect.objectContaining({ pathname: `/api/v1/strategies/${strategyId}/resume` }),
        expect.objectContaining({ method: 'POST' }),
      )
    })
  })

  it('queues skip next through the skip endpoint', async () => {
    const fetchMock = stubStrategyFetch()

    renderPage()

    fireEvent.click(await screen.findByTestId('skip-next-button'))

    await waitFor(() => {
      expect(fetchMock).toHaveBeenCalledWith(
        expect.objectContaining({ pathname: `/api/v1/strategies/${strategyId}/skip-next` }),
        expect.objectContaining({ method: 'POST' }),
      )
    })

    expect(await screen.findByText('skip next queued')).toBeInTheDocument()
  })

  it('shows conflict feedback when a lifecycle transition fails', async () => {
    stubStrategyFetch({
      pauseResult: {
        status: 409,
        body: { error: 'strategy already paused', code: 'strategy_conflict' },
      },
    })

    renderPage()

    fireEvent.click(await screen.findByTestId('pause-strategy-button'))

    expect(await screen.findByTestId('strategy-action-error')).toHaveTextContent('strategy already paused')
  })

  it('shows error state when strategy fetch fails', async () => {
    const fetchMock = vi.fn().mockRejectedValue(new Error('Network error'))
    vi.stubGlobal('fetch', fetchMock)

    renderPage()

    expect(await screen.findByTestId('strategy-detail-error')).toBeInTheDocument()
  })

  it('renders run history and config editor', async () => {
    const strategy = createStrategy({ name: 'Test Strategy', ticker: 'TEST', is_paper: true })
    const runs = {
      data: [
        {
          id: '00000000-0000-0000-0000-000000000010',
          strategy_id: strategyId,
          ticker: 'TEST',
          trade_date: '2025-01-02',
          status: 'completed',
          signal: 'buy',
          started_at: '2025-01-02T09:00:00Z',
          completed_at: '2025-01-02T09:01:00Z',
        },
      ],
      limit: 20,
      offset: 0,
    }

    const fetchMock = vi.fn((input: RequestInfo | URL) => {
      const url = typeof input === 'string' ? input : input.toString()

      if (url.includes('/runs')) {
        return Promise.resolve(createResponse(runs))
      }

      return Promise.resolve(createResponse(strategy))
    })
    vi.stubGlobal('fetch', fetchMock)

    renderPage()

    expect(await screen.findByTestId('strategy-run-history')).toBeInTheDocument()
    expect(screen.getByTestId('strategy-config-editor')).toBeInTheDocument()
    expect(await screen.findByTestId('run-history-list')).toBeInTheDocument()
  })
})
