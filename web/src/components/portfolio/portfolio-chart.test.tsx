import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen, waitFor } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { PortfolioChart } from '@/components/portfolio/portfolio-chart'

function Wrapper({ children }: { children: React.ReactNode }) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return <QueryClientProvider client={client}>{children}</QueryClientProvider>
}

afterEach(() => {
  vi.unstubAllGlobals()
})

describe('PortfolioChart', () => {
  it('renders the chart when closed positions are returned', async () => {
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
            realized_pnl: 50,
            opened_at: '2025-01-15T10:00:00Z',
            closed_at: '2025-01-16T10:00:00Z',
          },
        ],
        total: 1,
        limit: 100,
        offset: 0,
      }),
    })
    vi.stubGlobal('fetch', fetchMock)

    const { container } = render(<PortfolioChart />, { wrapper: Wrapper })

    await waitFor(() => {
      expect(container.querySelector('.recharts-responsive-container')).toBeInTheDocument()
    })
    expect(screen.queryByTestId('portfolio-chart-empty')).not.toBeInTheDocument()
  })

  it('shows empty state when no closed positions', async () => {
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
            realized_pnl: 0,
            opened_at: '2025-01-15T10:00:00Z',
          },
        ],
        total: 1,
        limit: 100,
        offset: 0,
      }),
    })
    vi.stubGlobal('fetch', fetchMock)

    render(<PortfolioChart />, { wrapper: Wrapper })

    expect(await screen.findByTestId('portfolio-chart-empty')).toBeInTheDocument()
  })

  it('shows empty state when the API returns null data', async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      json: async () => ({ data: null, total: 0, limit: 100, offset: 0 }),
    })
    vi.stubGlobal('fetch', fetchMock)

    render(<PortfolioChart />, { wrapper: Wrapper })

    expect(await screen.findByTestId('portfolio-chart-empty')).toBeInTheDocument()
  })

  it('shows error state when fetch fails', async () => {
    const fetchMock = vi.fn().mockRejectedValue(new Error('Network error'))
    vi.stubGlobal('fetch', fetchMock)

    render(<PortfolioChart />, { wrapper: Wrapper })

    expect(await screen.findByTestId('portfolio-chart-error')).toBeInTheDocument()
  })
})
