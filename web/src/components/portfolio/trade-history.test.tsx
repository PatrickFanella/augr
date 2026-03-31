import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { cleanup, render, screen, waitFor, within } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { TradeHistory } from '@/components/portfolio/trade-history'

function Wrapper({ children }: { children: React.ReactNode }) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return <QueryClientProvider client={client}>{children}</QueryClientProvider>
}

afterEach(() => {
  cleanup()
  vi.restoreAllMocks()
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

  it('shows empty state when the API returns null data', async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      json: async () => ({ data: null, total: 0, limit: 50, offset: 0 }),
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

  it('renders fallback values when trade fields are null', async () => {
    const consoleErrorSpy = vi.spyOn(console, 'error').mockImplementation(() => {})
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      json: async () => ({
        data: [
          {
            id: null,
            ticker: null,
            side: null,
            quantity: null,
            price: null,
            fee: null,
            executed_at: null,
            created_at: '2025-02-01T14:30:00Z',
          },
          {
            id: null,
            ticker: null,
            side: null,
            quantity: null,
            price: null,
            fee: null,
            executed_at: null,
            created_at: '2025-02-01T14:30:00Z',
          },
        ],
        total: 2,
        limit: 50,
        offset: 0,
      }),
    })
    vi.stubGlobal('fetch', fetchMock)

    render(<TradeHistory />, { wrapper: Wrapper })

    await waitFor(() => {
      expect(screen.queryByTestId('trade-history-loading')).not.toBeInTheDocument()
    })

    expect(screen.queryByTestId('trade-history-error')).not.toBeInTheDocument()
    expect(screen.queryByTestId('trade-history-empty')).not.toBeInTheDocument()

    const rows = screen.getAllByRole('row')
    const firstMalformedRowCells = within(rows[1]).getAllByRole('cell')
    const secondMalformedRowCells = within(rows[2]).getAllByRole('cell')

    expect(firstMalformedRowCells.map((cell) => cell.textContent)).toEqual([
      '—',
      '—',
      '—',
      '—',
      '—',
      '—',
      '—',
    ])
    expect(secondMalformedRowCells.map((cell) => cell.textContent)).toEqual([
      '—',
      '—',
      '—',
      '—',
      '—',
      '—',
      '—',
    ])
    expect(
      consoleErrorSpy.mock.calls.some((args) =>
        args.some((arg) => String(arg).includes('Encountered two children with the same key')),
      ),
    ).toBe(false)
  })
})
