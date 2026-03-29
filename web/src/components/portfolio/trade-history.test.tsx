import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { TradeHistory } from '@/components/portfolio/trade-history'

function Wrapper({ children }: { children: React.ReactNode }) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return <QueryClientProvider client={client}>{children}</QueryClientProvider>
}

afterEach(() => {
  vi.unstubAllGlobals()
})

describe('TradeHistory', () => {
  it('renders trades on successful fetch', async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      json: async () => ({
        data: [
          {
            id: 'trade-1',
            ticker: 'MSFT',
            side: 'buy',
            quantity: 20,
            price: 400.0,
            fee: 1.5,
            executed_at: '2025-02-01T14:30:00Z',
            created_at: '2025-02-01T14:30:00Z',
          },
        ],
        total: 1,
        limit: 50,
        offset: 0,
      }),
    })
    vi.stubGlobal('fetch', fetchMock)

    render(<TradeHistory />, { wrapper: Wrapper })

    expect(await screen.findByText('MSFT')).toBeInTheDocument()
    expect(screen.getByText('buy')).toBeInTheDocument()
    expect(screen.getByText('20')).toBeInTheDocument()
    expect(screen.getByText('$400.00')).toBeInTheDocument()
    expect(screen.getByText('$1.50')).toBeInTheDocument()
    expect(screen.getByText('$7,998.50')).toBeInTheDocument()
  })

  it('shows empty state when no trades', async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      json: async () => ({ data: [], total: 0, limit: 50, offset: 0 }),
    })
    vi.stubGlobal('fetch', fetchMock)

    render(<TradeHistory />, { wrapper: Wrapper })

    expect(await screen.findByTestId('trade-history-empty')).toBeInTheDocument()
  })

  it('shows error state when fetch fails', async () => {
    const fetchMock = vi.fn().mockRejectedValue(new Error('Network error'))
    vi.stubGlobal('fetch', fetchMock)

    render(<TradeHistory />, { wrapper: Wrapper })

    expect(await screen.findByTestId('trade-history-error')).toBeInTheDocument()
  })
})
