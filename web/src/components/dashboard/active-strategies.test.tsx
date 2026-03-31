import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { ActiveStrategies } from '@/components/dashboard/active-strategies'

function Wrapper({ children }: { children: React.ReactNode }) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return <QueryClientProvider client={client}>{children}</QueryClientProvider>
}

afterEach(() => {
  vi.unstubAllGlobals()
})

describe('ActiveStrategies', () => {
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
        is_active: true,
        is_paper: true,
        config: {},
        created_at: '2025-01-01T00:00:00Z',
        updated_at: '2025-01-01T00:00:00Z',
      },
    ]

    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      json: async () => ({ data: strategies, limit: 20, offset: 0 }),
    })
    vi.stubGlobal('fetch', fetchMock)

    render(<ActiveStrategies />, { wrapper: Wrapper })

    expect(await screen.findByText('AAPL Momentum')).toBeInTheDocument()
    expect(screen.getByText('BTC Swing')).toBeInTheDocument()
    expect(screen.getByText('stock')).toBeInTheDocument()
    expect(screen.getByText('crypto')).toBeInTheDocument()
    expect(screen.getByText('paper')).toBeInTheDocument()
  })

  it('shows empty state when no strategies', async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      json: async () => ({ data: [], limit: 20, offset: 0 }),
    })
    vi.stubGlobal('fetch', fetchMock)

    render(<ActiveStrategies />, { wrapper: Wrapper })

    expect(await screen.findByText('No active strategies')).toBeInTheDocument()
  })

  it('shows empty state when API returns a null data array', async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      json: async () => ({ data: null, limit: 20, offset: 0 }),
    })
    vi.stubGlobal('fetch', fetchMock)

    render(<ActiveStrategies />, { wrapper: Wrapper })

    expect(await screen.findByText('No active strategies')).toBeInTheDocument()
  })

  it('shows error state when fetch fails', async () => {
    const fetchMock = vi.fn().mockRejectedValue(new Error('Network error'))
    vi.stubGlobal('fetch', fetchMock)

    render(<ActiveStrategies />, { wrapper: Wrapper })

    expect(await screen.findByTestId('active-strategies-error')).toBeInTheDocument()
  })
})
