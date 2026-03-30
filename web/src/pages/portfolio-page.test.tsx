import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { cleanup, render, screen } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { PortfolioPage } from '@/pages/portfolio-page'

function Wrapper({ children }: { children: React.ReactNode }) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return <QueryClientProvider client={client}>{children}</QueryClientProvider>
}

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})

describe('PortfolioPage', () => {
  it('renders all portfolio sections', async () => {
    const fetchMock = vi.fn((input: RequestInfo | URL) => {
      const url = typeof input === 'string' ? input : input.toString()

      if (url.includes('portfolio/summary')) {
        return Promise.resolve({
          ok: true,
          status: 200,
          json: async () => ({ open_positions: 2, unrealized_pnl: 500, realized_pnl: 300 }),
        })
      }

      if (url.includes('positions/open')) {
        return Promise.resolve({
          ok: true,
          status: 200,
          json: async () => ({ data: [], total: 0, limit: 50, offset: 0 }),
        })
      }

      if (url.includes('positions')) {
        return Promise.resolve({
          ok: true,
          status: 200,
          json: async () => ({ data: [], total: 0, limit: 100, offset: 0 }),
        })
      }

      if (url.includes('trades')) {
        return Promise.resolve({
          ok: true,
          status: 200,
          json: async () => ({ data: [], total: 0, limit: 50, offset: 0 }),
        })
      }

      return Promise.reject(new Error(`Unhandled fetch URL in test: ${url}`))
    })
    vi.stubGlobal('fetch', fetchMock)

    render(<PortfolioPage />, { wrapper: Wrapper })

    expect(screen.getByTestId('portfolio-page')).toBeInTheDocument()
    expect(await screen.findByTestId('portfolio-chart')).toBeInTheDocument()
    expect(screen.getByTestId('positions-table')).toBeInTheDocument()
    expect(screen.getByTestId('trade-history')).toBeInTheDocument()
  })

  it('renders safely when portfolio endpoints return null data arrays', async () => {
    const fetchMock = vi.fn((input: RequestInfo | URL) => {
      const url = typeof input === 'string' ? input : input.toString()

      if (url.includes('portfolio/summary')) {
        return Promise.resolve({
          ok: true,
          status: 200,
          json: async () => ({ open_positions: 0, unrealized_pnl: 0, realized_pnl: 0 }),
        })
      }

      if (url.includes('positions/open')) {
        return Promise.resolve({
          ok: true,
          status: 200,
          json: async () => ({ data: null, total: 0, limit: 50, offset: 0 }),
        })
      }

      if (url.includes('positions')) {
        return Promise.resolve({
          ok: true,
          status: 200,
          json: async () => ({ data: null, total: 0, limit: 100, offset: 0 }),
        })
      }

      if (url.includes('trades')) {
        return Promise.resolve({
          ok: true,
          status: 200,
          json: async () => ({ data: null, total: 0, limit: 50, offset: 0 }),
        })
      }

      return Promise.reject(new Error(`Unhandled fetch URL in test: ${url}`))
    })
    vi.stubGlobal('fetch', fetchMock)

    render(<PortfolioPage />, { wrapper: Wrapper })

    expect(await screen.findByTestId('portfolio-chart-empty')).toBeInTheDocument()
    expect(screen.getByTestId('positions-table-empty')).toBeInTheDocument()
    expect(screen.getByTestId('trade-history-empty')).toBeInTheDocument()
  })
})
