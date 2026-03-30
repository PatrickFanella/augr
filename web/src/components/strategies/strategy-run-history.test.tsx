import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { StrategyRunHistory } from '@/components/strategies/strategy-run-history'

function Wrapper({ children }: { children: React.ReactNode }) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return <QueryClientProvider client={client}>{children}</QueryClientProvider>
}

afterEach(() => {
  vi.unstubAllGlobals()
})

const strategyId = '00000000-0000-0000-0000-000000000001'

describe('StrategyRunHistory', () => {
  it('renders run list on successful fetch', async () => {
    const runs = [
      {
        id: '00000000-0000-0000-0000-000000000010',
        strategy_id: strategyId,
        ticker: 'AAPL',
        trade_date: '2025-01-02',
        status: 'completed',
        signal: 'buy',
        started_at: '2025-01-02T09:00:00Z',
        completed_at: '2025-01-02T09:01:00Z',
      },
      {
        id: '00000000-0000-0000-0000-000000000011',
        strategy_id: strategyId,
        ticker: 'AAPL',
        trade_date: '2025-01-03',
        status: 'failed',
        started_at: '2025-01-03T09:00:00Z',
        error_message: 'Connection timeout',
      },
    ]

    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      json: async () => ({ data: runs, limit: 20, offset: 0 }),
    })
    vi.stubGlobal('fetch', fetchMock)

    render(<StrategyRunHistory strategyId={strategyId} />, { wrapper: Wrapper })

    expect(await screen.findByTestId('run-history-list')).toBeInTheDocument()
    expect(screen.getByText('Completed')).toBeInTheDocument()
    expect(screen.getByText('Failed')).toBeInTheDocument()
    expect(screen.getByText('buy')).toBeInTheDocument()
  })

  it('shows empty state when no runs', async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      json: async () => ({ data: [], limit: 20, offset: 0 }),
    })
    vi.stubGlobal('fetch', fetchMock)

    render(<StrategyRunHistory strategyId={strategyId} />, { wrapper: Wrapper })

    expect(await screen.findByTestId('run-history-empty')).toBeInTheDocument()
  })

  it('shows empty state when API returns null data array', async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      json: async () => ({ data: null, limit: 20, offset: 0 }),
    })
    vi.stubGlobal('fetch', fetchMock)

    render(<StrategyRunHistory strategyId={strategyId} />, { wrapper: Wrapper })

    expect(await screen.findByTestId('run-history-empty')).toBeInTheDocument()
  })

  it('shows error state when fetch fails', async () => {
    const fetchMock = vi.fn().mockRejectedValue(new Error('Network error'))
    vi.stubGlobal('fetch', fetchMock)

    render(<StrategyRunHistory strategyId={strategyId} />, { wrapper: Wrapper })

    expect(await screen.findByTestId('run-history-error')).toBeInTheDocument()
  })
})
