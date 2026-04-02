import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { cleanup, render, screen } from '@testing-library/react'
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

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})

describe('StrategyDetailPage', () => {
  it('renders strategy details and lifecycle actions on successful fetch', async () => {
    const strategy = {
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
    }

    const runs = { data: [], limit: 20, offset: 0 }

    const fetchMock = vi.fn((input: RequestInfo | URL) => {
      const url = typeof input === 'string' ? input : input.toString()

      if (url.includes('/runs')) {
        return Promise.resolve({
          ok: true,
          status: 200,
          json: async () => runs,
        })
      }

      return Promise.resolve({
        ok: true,
        status: 200,
        json: async () => strategy,
      })
    })
    vi.stubGlobal('fetch', fetchMock)

    render(<StrategyDetailPage />, { wrapper: Wrapper })

    expect(await screen.findByText('AAPL Momentum')).toBeInTheDocument()
    expect(screen.getByText('A momentum-based strategy')).toBeInTheDocument()
    expect(screen.getByText('AAPL')).toBeInTheDocument()
    expect(screen.getByTestId('strategy-detail-page')).toBeInTheDocument()
    expect(screen.getByTestId('strategy-status-badge')).toHaveTextContent('active')
    expect(screen.getByTestId('run-strategy-button')).toBeInTheDocument()
    expect(screen.getByTestId('pause-strategy-button')).toBeEnabled()
    expect(screen.getByTestId('skip-next-button')).toBeEnabled()
    expect(screen.getByTestId('delete-strategy-button')).toBeInTheDocument()
  })

  it('shows resume button for paused strategies', async () => {
    const strategy = {
      id: strategyId,
      name: 'Paused Strategy',
      ticker: 'AAPL',
      market_type: 'stock',
      status: 'paused',
      skip_next_run: false,
      is_paper: false,
      config: {},
      created_at: '2025-01-01T00:00:00Z',
      updated_at: '2025-01-01T00:00:00Z',
    }

    const fetchMock = vi.fn((input: RequestInfo | URL) => {
      const url = typeof input === 'string' ? input : input.toString()
      if (url.includes('/runs')) {
        return Promise.resolve({ ok: true, status: 200, json: async () => ({ data: [], limit: 20, offset: 0 }) })
      }
      return Promise.resolve({ ok: true, status: 200, json: async () => strategy })
    })
    vi.stubGlobal('fetch', fetchMock)

    render(<StrategyDetailPage />, { wrapper: Wrapper })

    expect(await screen.findByTestId('resume-strategy-button')).toBeEnabled()
    expect(screen.getByTestId('skip-next-button')).toBeDisabled()
  })

  it('shows error state when strategy fetch fails', async () => {
    const fetchMock = vi.fn().mockRejectedValue(new Error('Network error'))
    vi.stubGlobal('fetch', fetchMock)

    render(<StrategyDetailPage />, { wrapper: Wrapper })

    expect(await screen.findByTestId('strategy-detail-error')).toBeInTheDocument()
  })

  it('renders run history and config editor', async () => {
    const strategy = {
      id: strategyId,
      name: 'Test Strategy',
      ticker: 'TEST',
      market_type: 'stock',
      status: 'active',
      skip_next_run: false,
      is_paper: true,
      config: {},
      created_at: '2025-01-01T00:00:00Z',
      updated_at: '2025-01-01T00:00:00Z',
    }

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
        return Promise.resolve({ ok: true, status: 200, json: async () => runs })
      }

      return Promise.resolve({ ok: true, status: 200, json: async () => strategy })
    })
    vi.stubGlobal('fetch', fetchMock)

    render(<StrategyDetailPage />, { wrapper: Wrapper })

    expect(await screen.findByTestId('strategy-run-history')).toBeInTheDocument()
    expect(screen.getByTestId('strategy-config-editor')).toBeInTheDocument()
    expect(await screen.findByTestId('run-history-list')).toBeInTheDocument()
  })
})
