import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { cleanup, render, screen } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { RecentRuns } from '@/components/dashboard/recent-runs'

function Wrapper({ children }: { children: React.ReactNode }) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return <QueryClientProvider client={client}>{children}</QueryClientProvider>
}

function jsonResponse(body: unknown, status = 200) {
  return {
    ok: status >= 200 && status < 300,
    status,
    json: async () => body,
  }
}

describe('RecentRuns', () => {
  afterEach(() => {
    cleanup()
    vi.unstubAllGlobals()
  })

  it('renders recent pipeline runs from the API', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(() =>
        Promise.resolve(
          jsonResponse({
            data: [
              {
                id: 'run-1',
                strategy_id: 'strategy-1',
                ticker: 'AAPL',
                trade_date: '2026-04-22T00:00:00Z',
                status: 'completed',
                signal: 'buy',
                started_at: '2026-04-22T00:00:00Z',
                completed_at: '2026-04-22T00:03:00Z',
              },
            ],
            limit: 10,
            offset: 0,
          }),
        ),
      ),
    )

    render(<RecentRuns />, { wrapper: Wrapper })

    expect(await screen.findByText('Recent runs')).toBeInTheDocument()
    expect(await screen.findByText('AAPL')).toBeInTheDocument()
    expect(screen.getByText('Completed')).toBeInTheDocument()
    expect(screen.getByText('buy')).toBeInTheDocument()
  })

  it('shows empty state when no runs are available', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(() => Promise.resolve(jsonResponse({ data: [], limit: 10, offset: 0 }))),
    )

    render(<RecentRuns />, { wrapper: Wrapper })

    expect(await screen.findByTestId('recent-runs-empty')).toBeInTheDocument()
    expect(screen.getByText('No runs yet')).toBeInTheDocument()
  })
})
