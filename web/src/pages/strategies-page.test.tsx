import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { cleanup, render, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { StrategiesPage } from '@/pages/strategies-page'

function Wrapper({ children }: { children: React.ReactNode }) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return (
    <QueryClientProvider client={client}>
      <MemoryRouter>{children}</MemoryRouter>
    </QueryClientProvider>
  )
}

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})

describe('StrategiesPage', () => {
  it('renders strategy list on successful fetch', async () => {
    const strategies = [
      {
        id: '00000000-0000-0000-0000-000000000001',
        name: 'AAPL Momentum',
        ticker: 'AAPL',
        market_type: 'stock',
        is_active: true,
        is_paper: false,
        schedule_cron: '0 9 * * *',
        config: {},
        created_at: '2025-01-01T00:00:00Z',
        updated_at: '2025-01-01T00:00:00Z',
      },
      {
        id: '00000000-0000-0000-0000-000000000002',
        name: 'BTC Swing',
        ticker: 'BTCUSD',
        market_type: 'crypto',
        is_active: false,
        is_paper: true,
        config: {},
        created_at: '2025-01-01T00:00:00Z',
        updated_at: '2025-01-01T00:00:00Z',
      },
    ]

    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      json: async () => ({ data: strategies, total: 2, limit: 100, offset: 0 }),
    })
    vi.stubGlobal('fetch', fetchMock)

    render(<StrategiesPage />, { wrapper: Wrapper })

    expect(await screen.findByText('AAPL Momentum')).toBeInTheDocument()
    expect(screen.getByText('BTC Swing')).toBeInTheDocument()
    expect(screen.getByText('active')).toBeInTheDocument()
    expect(screen.getByText('inactive')).toBeInTheDocument()
    expect(screen.getByText('paper')).toBeInTheDocument()
    expect(screen.getByTestId('strategies-list')).toBeInTheDocument()
  })

  it('shows empty state when no strategies exist', async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      json: async () => ({ data: [], total: 0, limit: 100, offset: 0 }),
    })
    vi.stubGlobal('fetch', fetchMock)

    render(<StrategiesPage />, { wrapper: Wrapper })

    expect(await screen.findByTestId('strategies-empty')).toBeInTheDocument()
  })

  it('shows empty state when API returns null data array', async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      json: async () => ({ data: null, total: 0, limit: 100, offset: 0 }),
    })
    vi.stubGlobal('fetch', fetchMock)

    render(<StrategiesPage />, { wrapper: Wrapper })

    expect(await screen.findByTestId('strategies-empty')).toBeInTheDocument()
  })

  it('shows error state when fetch fails', async () => {
    const fetchMock = vi.fn().mockRejectedValue(new Error('Network error'))
    vi.stubGlobal('fetch', fetchMock)

    render(<StrategiesPage />, { wrapper: Wrapper })

    expect(await screen.findByTestId('strategies-error')).toBeInTheDocument()
  })

  it('shows create strategy button', async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      json: async () => ({ data: [], total: 0, limit: 100, offset: 0 }),
    })
    vi.stubGlobal('fetch', fetchMock)

    render(<StrategiesPage />, { wrapper: Wrapper })

    const page = await screen.findByTestId('strategies-page')
    expect(page).toHaveTextContent('New strategy')
  })

  it('shows run buttons for each strategy', async () => {
    const strategies = [
      {
        id: '00000000-0000-0000-0000-000000000001',
        name: 'Test Strategy',
        ticker: 'TEST',
        market_type: 'stock',
        is_active: true,
        is_paper: false,
        config: {},
        created_at: '2025-01-01T00:00:00Z',
        updated_at: '2025-01-01T00:00:00Z',
      },
    ]

    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      json: async () => ({ data: strategies, total: 1, limit: 100, offset: 0 }),
    })
    vi.stubGlobal('fetch', fetchMock)

    render(<StrategiesPage />, { wrapper: Wrapper })

    expect(
      await screen.findByTestId('run-strategy-00000000-0000-0000-0000-000000000001'),
    ).toBeInTheDocument()
  })
})
