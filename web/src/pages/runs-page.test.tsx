import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { cleanup, render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { RunsPage } from '@/pages/runs-page'

function Wrapper({ children }: { children: React.ReactNode }) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return (
    <QueryClientProvider client={client}>
      <MemoryRouter>{children}</MemoryRouter>
    </QueryClientProvider>
  )
}

function formatDate(dateStr: string) {
  return new Date(dateStr).toLocaleString(undefined, {
    month: 'short',
    day: 'numeric',
    year: 'numeric',
    hour: '2-digit',
    minute: '2-digit',
  })
}

function deferred<T>() {
  let resolve!: (value: T) => void
  let reject!: (reason?: unknown) => void
  const promise = new Promise<T>((res, rej) => {
    resolve = res
    reject = rej
  })

  return { promise, resolve, reject }
}

const strategyIdOne = '00000000-0000-0000-0000-000000000001'
const strategyIdTwo = '00000000-0000-0000-0000-000000000002'

const strategiesResponse = {
  data: [
    {
      id: strategyIdOne,
      name: 'AAPL Momentum',
      ticker: 'AAPL',
      market_type: 'stock',
      is_active: true,
      is_paper: false,
      config: {},
      created_at: '2025-01-01T00:00:00Z',
      updated_at: '2025-01-01T00:00:00Z',
    },
    {
      id: strategyIdTwo,
      name: 'BTC Mean Reversion',
      ticker: 'BTCUSD',
      market_type: 'crypto',
      is_active: true,
      is_paper: false,
      config: {},
      created_at: '2025-01-01T00:00:00Z',
      updated_at: '2025-01-01T00:00:00Z',
    },
  ],
  total: 2,
  limit: 1000,
  offset: 0,
}

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})

function getRequestedUrls(fetchMock: ReturnType<typeof vi.fn>) {
  return fetchMock.mock.calls.map(([input]) => String(input))
}

describe('RunsPage', () => {
  it('renders the paginated runs table with badges, strategy names, and duration', async () => {
    const runsResponse = {
      data: [
        {
          id: '00000000-0000-0000-0000-000000000101',
          strategy_id: strategyIdOne,
          ticker: 'AAPL',
          trade_date: '2025-01-02',
          status: 'completed' as const,
          signal: 'buy' as const,
          started_at: '2025-01-02T09:00:00Z',
          completed_at: '2025-01-02T09:05:30Z',
        },
        {
          id: '00000000-0000-0000-0000-000000000102',
          strategy_id: strategyIdTwo,
          ticker: 'BTCUSD',
          trade_date: '2025-01-03',
          status: 'failed' as const,
          signal: 'sell' as const,
          started_at: '2025-01-03T10:00:00Z',
          completed_at: '2025-01-03T10:00:45Z',
        },
      ],
      limit: 10,
      offset: 0,
    }

    const fetchMock = vi.fn((input: RequestInfo | URL) => {
      const url = typeof input === 'string' ? input : input.toString()
      if (url.includes('/api/v1/strategies')) {
        return Promise.resolve({ ok: true, status: 200, json: async () => strategiesResponse })
      }
      return Promise.resolve({ ok: true, status: 200, json: async () => runsResponse })
    })
    vi.stubGlobal('fetch', fetchMock)

    render(<RunsPage />, { wrapper: Wrapper })

    expect(await screen.findByTestId('runs-table')).toBeInTheDocument()
    expect(screen.getByText('AAPL Momentum')).toBeInTheDocument()
    expect(screen.getByText('BTC Mean Reversion')).toBeInTheDocument()
    expect(screen.getByText('Completed')).toBeInTheDocument()
    expect(screen.getByText('Failed')).toBeInTheDocument()
    expect(screen.getByText('Buy')).toBeInTheDocument()
    expect(screen.getByText('Sell')).toBeInTheDocument()
    expect(screen.getByText(formatDate('2025-01-02T09:00:00Z'))).toBeInTheDocument()
    expect(screen.getByText('5m 30s')).toBeInTheDocument()
    expect(screen.getByText('45s')).toBeInTheDocument()
    expect(getRequestedUrls(fetchMock)).toContain('http://localhost:8080/api/v1/runs?limit=10&offset=0')
  })

  it('shows a loading skeleton while the runs request is in flight', () => {
    const runsDeferred = deferred<{ ok: boolean; status: number; json: () => Promise<unknown> }>()

    const fetchMock = vi.fn((input: RequestInfo | URL) => {
      const url = typeof input === 'string' ? input : input.toString()
      if (url.includes('/api/v1/strategies')) {
        return Promise.resolve({ ok: true, status: 200, json: async () => strategiesResponse })
      }
      return runsDeferred.promise
    })
    vi.stubGlobal('fetch', fetchMock)

    render(<RunsPage />, { wrapper: Wrapper })

    expect(screen.getByTestId('runs-loading')).toBeInTheDocument()
  })

  it('shows an error state and retries the runs request', async () => {
    const user = userEvent.setup()
    const runsResponse = {
      data: [
        {
          id: '00000000-0000-0000-0000-000000000103',
          strategy_id: strategyIdOne,
          ticker: 'AAPL',
          trade_date: '2025-01-04',
          status: 'running' as const,
          signal: 'hold' as const,
          started_at: '2025-01-04T11:00:00Z',
        },
      ],
      limit: 10,
      offset: 0,
    }

    let runRequests = 0
    const fetchMock = vi.fn((input: RequestInfo | URL) => {
      const url = typeof input === 'string' ? input : input.toString()
      if (url.includes('/api/v1/strategies')) {
        return Promise.resolve({ ok: true, status: 200, json: async () => strategiesResponse })
      }

      runRequests += 1
      if (runRequests === 1) {
        return Promise.reject(new Error('Network error'))
      }

      return Promise.resolve({ ok: true, status: 200, json: async () => runsResponse })
    })
    vi.stubGlobal('fetch', fetchMock)

    render(<RunsPage />, { wrapper: Wrapper })

    expect(await screen.findByTestId('runs-error')).toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: 'Retry' }))

    expect(await screen.findByTestId('runs-table')).toBeInTheDocument()
    expect(screen.getByText('Hold')).toBeInTheDocument()
    expect(screen.getByText('Running')).toBeInTheDocument()
  })

  it('supports previous and next pagination using limit and offset params', async () => {
    const user = userEvent.setup()
    const firstPageRuns = Array.from({ length: 10 }, (_, index) => ({
      id: `00000000-0000-0000-0000-0000000002${index}`,
      strategy_id: strategyIdOne,
      ticker: `AAPL${index}`,
      trade_date: '2025-01-05',
      status: 'completed' as const,
      signal: 'buy' as const,
      started_at: '2025-01-05T09:00:00Z',
      completed_at: '2025-01-05T09:01:00Z',
    }))

    const secondPageRuns = [
      {
        id: '00000000-0000-0000-0000-000000000300',
        strategy_id: strategyIdTwo,
        ticker: 'BTCUSD',
        trade_date: '2025-01-06',
        status: 'cancelled' as const,
        signal: 'sell' as const,
        started_at: '2025-01-06T09:00:00Z',
        completed_at: '2025-01-06T09:02:00Z',
      },
    ]

    const fetchMock = vi.fn((input: RequestInfo | URL) => {
      const url = typeof input === 'string' ? input : input.toString()
      if (url.includes('/api/v1/strategies')) {
        return Promise.resolve({ ok: true, status: 200, json: async () => strategiesResponse })
      }
      if (url.includes('offset=10')) {
        return Promise.resolve({
          ok: true,
          status: 200,
          json: async () => ({ data: secondPageRuns, limit: 10, offset: 10 }),
        })
      }
      return Promise.resolve({
        ok: true,
        status: 200,
        json: async () => ({ data: firstPageRuns, limit: 10, offset: 0 }),
      })
    })
    vi.stubGlobal('fetch', fetchMock)

    render(<RunsPage />, { wrapper: Wrapper })

    expect(await screen.findByText('AAPL0')).toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: 'Next' }))

    expect(await screen.findByText('BTCUSD')).toBeInTheDocument()
    expect(getRequestedUrls(fetchMock)).toContain(
      'http://localhost:8080/api/v1/runs?limit=10&offset=10',
    )

    await user.click(screen.getByRole('button', { name: 'Previous' }))
    expect(await screen.findByText('AAPL0')).toBeInTheDocument()
  })
})
