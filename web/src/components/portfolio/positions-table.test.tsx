import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { PositionsTable } from '@/components/portfolio/positions-table'

function Wrapper({ children }: { children: React.ReactNode }) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return <QueryClientProvider client={client}>{children}</QueryClientProvider>
}

afterEach(() => {
  vi.unstubAllGlobals()
})

describe('PositionsTable', () => {
  it('renders open positions on successful fetch', async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      json: async () => ({
        data: [
          {
            id: 'pos-1',
            ticker: 'AAPL',
            side: 'long',
            quantity: 10,
            avg_entry: 150.0,
            current_price: 155.0,
            unrealized_pnl: 50.0,
            realized_pnl: 0,
            stop_loss: 145.0,
            take_profit: 165.0,
            opened_at: '2025-01-15T10:00:00Z',
          },
        ],
        total: 1,
        limit: 50,
        offset: 0,
      }),
    })
    vi.stubGlobal('fetch', fetchMock)

    render(<PositionsTable />, { wrapper: Wrapper })

    expect(await screen.findByText('AAPL')).toBeInTheDocument()
    expect(screen.getByText('long')).toBeInTheDocument()
    expect(screen.getByText('10')).toBeInTheDocument()
    expect(screen.getByText('$150.00')).toBeInTheDocument()
    expect(screen.getByText('$155.00')).toBeInTheDocument()
    expect(screen.getByText('$50.00')).toBeInTheDocument()
  })

  it('shows empty state when no positions', async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      json: async () => ({ data: [], total: 0, limit: 50, offset: 0 }),
    })
    vi.stubGlobal('fetch', fetchMock)

    render(<PositionsTable />, { wrapper: Wrapper })

    expect(await screen.findByTestId('positions-table-empty')).toBeInTheDocument()
  })

  it('shows empty state when the API returns null data', async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      json: async () => ({ data: null, total: 0, limit: 50, offset: 0 }),
    })
    vi.stubGlobal('fetch', fetchMock)

    render(<PositionsTable />, { wrapper: Wrapper })

    expect(await screen.findByTestId('positions-table-empty')).toBeInTheDocument()
  })

  it('shows error state when fetch fails', async () => {
    const fetchMock = vi.fn().mockRejectedValue(new Error('Network error'))
    vi.stubGlobal('fetch', fetchMock)

    render(<PositionsTable />, { wrapper: Wrapper })

    expect(await screen.findByTestId('positions-table-error')).toBeInTheDocument()
  })

  it('shows position detail when a row is clicked', async () => {
    const user = userEvent.setup()
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      json: async () => ({
        data: [
          {
            id: 'pos-1',
            ticker: 'TSLA',
            side: 'short',
            quantity: 5,
            avg_entry: 250.0,
            current_price: 240.0,
            unrealized_pnl: 50.0,
            realized_pnl: 0,
            opened_at: '2025-01-15T10:00:00Z',
          },
        ],
        total: 1,
        limit: 50,
        offset: 0,
      }),
    })
    vi.stubGlobal('fetch', fetchMock)

    render(<PositionsTable />, { wrapper: Wrapper })

    const row = await screen.findByText('TSLA')
    await user.click(row.closest('tr')!)

    expect(screen.getByTestId('position-detail')).toBeInTheDocument()
    expect(screen.getByText('TSLA')).toBeInTheDocument()
  })
})
