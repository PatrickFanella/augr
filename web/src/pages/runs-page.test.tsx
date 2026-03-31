import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { cleanup, render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { RunsPage } from '@/pages/runs-page'

function Wrapper({ children }: { children: React.ReactNode }) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return (
    <QueryClientProvider client={client}>
      <MemoryRouter initialEntries={['/runs']}>
        <Routes>
          <Route path="runs" element={children} />
          <Route path="runs/:id" element={<div data-testid="run-detail-route">Run detail route</div>} />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>
  )
}

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})

describe('RunsPage', () => {
  it('renders the runs table on successful fetch', async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      json: async () => ({
        data: [
          {
            id: '00000000-0000-0000-0000-000000000010',
            strategy_id: '00000000-0000-0000-0000-000000000001',
            ticker: 'AAPL',
            trade_date: '2025-01-02',
            status: 'completed',
            signal: 'buy',
            started_at: '2025-01-02T09:00:00Z',
            completed_at: '2025-01-02T09:01:00Z',
          },
        ],
        total: 1,
        limit: 50,
        offset: 0,
      }),
    })
    vi.stubGlobal('fetch', fetchMock)

    render(<RunsPage />, { wrapper: Wrapper })

    const table = await screen.findByTestId('runs-table')

    expect(table).toBeInTheDocument()
    expect(screen.getByText('AAPL')).toBeInTheDocument()
    expect(screen.getByTestId('run-row-00000000-0000-0000-0000-000000000010')).toHaveTextContent(
      'Completed',
    )
    expect(screen.getByText('buy')).toBeInTheDocument()
  })

  it('navigates to the run detail route when a row is clicked', async () => {
    const user = userEvent.setup()
    const runId = '00000000-0000-0000-0000-000000000010'
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      json: async () => ({
        data: [
          {
            id: runId,
            strategy_id: '00000000-0000-0000-0000-000000000001',
            ticker: 'AAPL',
            trade_date: '2025-01-02',
            status: 'completed',
            signal: 'buy',
            started_at: '2025-01-02T09:00:00Z',
            completed_at: '2025-01-02T09:01:00Z',
          },
        ],
        total: 1,
        limit: 50,
        offset: 0,
      }),
    })
    vi.stubGlobal('fetch', fetchMock)

    render(<RunsPage />, { wrapper: Wrapper })

    const row = await screen.findByTestId(`run-row-${runId}`)
    const link = screen.getByTestId(`run-link-${runId}`)

    expect(row).toHaveClass('cursor-pointer')
    expect(row).toHaveClass('hover:bg-secondary/40')
    expect(link).toHaveClass('cursor-pointer')

    await user.click(row)

    expect(await screen.findByTestId('run-detail-route')).toBeInTheDocument()
  })

  it('navigates to the run detail route when the row link is activated with Enter', async () => {
    const user = userEvent.setup()
    const runId = '00000000-0000-0000-0000-000000000010'
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      json: async () => ({
        data: [
          {
            id: runId,
            strategy_id: '00000000-0000-0000-0000-000000000001',
            ticker: 'AAPL',
            trade_date: '2025-01-02',
            status: 'completed',
            signal: 'buy',
            started_at: '2025-01-02T09:00:00Z',
            completed_at: '2025-01-02T09:01:00Z',
          },
        ],
        total: 1,
        limit: 50,
        offset: 0,
      }),
    })
    vi.stubGlobal('fetch', fetchMock)

    render(<RunsPage />, { wrapper: Wrapper })

    const link = await screen.findByTestId(`run-link-${runId}`)
    link.focus()

    expect(link).toHaveFocus()

    await user.keyboard('{Enter}')

    expect(await screen.findByTestId('run-detail-route')).toBeInTheDocument()
  })

  it('shows empty state when no runs exist', async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      json: async () => ({ data: [], total: 0, limit: 50, offset: 0 }),
    })
    vi.stubGlobal('fetch', fetchMock)

    render(<RunsPage />, { wrapper: Wrapper })

    expect(await screen.findByTestId('runs-empty')).toBeInTheDocument()
  })

  it('shows error state when fetch fails', async () => {
    const fetchMock = vi.fn().mockRejectedValue(new Error('Network error'))
    vi.stubGlobal('fetch', fetchMock)

    render(<RunsPage />, { wrapper: Wrapper })

    expect(await screen.findByTestId('runs-error')).toBeInTheDocument()
    expect(screen.getByText('Unable to load runs')).toBeInTheDocument()
  })
})
