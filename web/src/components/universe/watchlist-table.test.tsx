import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { fireEvent, render, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { describe, expect, it, vi } from 'vitest'

import { WatchlistTable } from '@/components/universe/watchlist-table'

const navigateMock = vi.fn()
vi.mock('react-router-dom', async () => {
  const actual = await vi.importActual<typeof import('react-router-dom')>('react-router-dom')
  return {
    ...actual,
    useNavigate: () => navigateMock,
  }
})

function Wrapper({ children }: { children: React.ReactNode }) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return (
    <QueryClientProvider client={client}>
      <MemoryRouter>{children}</MemoryRouter>
    </QueryClientProvider>
  )
}

describe('WatchlistTable', () => {
  it('renders scored tickers using score field', () => {
    render(
      <WatchlistTable
        tickers={[
          {
            ticker: 'AAPL',
            score: 0.82,
            reasons: ['gap_up'],
            day_volume: 1200000,
            day_close: 182.35,
            change_pct: 1.2,
            gap_pct: 0.6,
          },
        ]}
      />,
      { wrapper: Wrapper },
    )

    expect(screen.getByText('AAPL')).toBeInTheDocument()
    expect(screen.getByText('0.82')).toBeInTheDocument()
    expect(screen.getByText('gap_up')).toBeInTheDocument()
  })

  it('renders 0.00 when score is undefined (null-guard)', () => {
    render(
      <WatchlistTable
        tickers={[
          {
            ticker: 'MSFT',
            score: undefined as unknown as number,
            reasons: [],
            day_volume: 500000,
            day_close: 300,
            change_pct: 0,
            gap_pct: 0,
          },
        ]}
      />,
      { wrapper: Wrapper },
    )

    expect(screen.getByText('MSFT')).toBeInTheDocument()
    expect(screen.getByText('0.00')).toBeInTheDocument()
  })

  it('navigates to discovery with selected ticker when row clicked', () => {
    render(
      <WatchlistTable
        tickers={[
          {
            ticker: 'TSLA',
            score: 0.55,
            reasons: [],
            day_volume: 1000,
            day_close: 250,
            change_pct: -0.5,
            gap_pct: 0,
          },
        ]}
      />,
      { wrapper: Wrapper },
    )

    fireEvent.click(screen.getByText('TSLA'))
    expect(navigateMock).toHaveBeenCalledWith('/discovery?tickers=TSLA')
  })
})
