import { render, screen, within } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { MemoryRouter } from 'react-router-dom'
import { expect, it, vi } from 'vitest'

import { apiClient } from '@/lib/api/client'
import { KalshiPage } from '@/pages/kalshi-page'

it('renders a paper/data-first Kalshi hub with real summary data and cross-links', async () => {
  vi.spyOn(apiClient, 'getKalshiSummary').mockResolvedValue({
    watched_markets: [{ ticker: 'KX-ONE', event_ticker: 'EVT-ONE', title: 'Will one happen?', status: 'open', enabled: true }],
    latest_snapshots: [{ id: 'snap-1', ticker: 'KX-ONE', title: 'Will one happen?', status: 'open', yes_bid: 0.41, yes_ask: 0.43, no_bid: 0.57, no_ask: 0.59, volume: 1200, open_interest: 800, captured_at: '2026-06-18T12:00:00Z' }],
    discovery: { last_run: { id: 'run-1', status: 'completed', result: { fetched: 1, screened: 1, proposed: 1, deployed: 1 }, started_at: '2026-06-18T12:00:00Z' }, status: 'completed' },
    strategies: { active_paper: 2 },
  })
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })

  render(
    <QueryClientProvider client={queryClient}>
      <MemoryRouter>
        <KalshiPage />
      </MemoryRouter>
    </QueryClientProvider>,
  )

  expect(screen.getByTestId('kalshi-page')).toBeInTheDocument()
  expect(screen.getByRole('heading', { name: 'Kalshi', level: 1 })).toBeInTheDocument()
  expect(screen.getByText(/paper\/data-first Kalshi hub/i)).toBeInTheDocument()

  const sectionNav = screen.getByRole('navigation', { name: /kalshi sections/i })
  expect(within(sectionNav).getByRole('link', { name: 'Overview' })).toHaveAttribute('href', '#overview')
  expect(within(sectionNav).getByRole('link', { name: 'Setup' })).toHaveAttribute('href', '#setup')

  expect(screen.getByTestId('kalshi-overview-section')).toBeInTheDocument()
  expect(screen.getByTestId('kalshi-markets-section')).toBeInTheDocument()
  expect(screen.getByTestId('kalshi-paper-strategies-section')).toBeInTheDocument()
  expect(screen.getByTestId('kalshi-operations-section')).toBeInTheDocument()
  expect(screen.getByTestId('kalshi-setup-section')).toBeInTheDocument()
  const summaryCard = screen.getByTestId('event-market-summary-card')
  expect(summaryCard).toBeInTheDocument()
  expect(within(summaryCard).getByRole('status', { name: /paper\/data only warn/i })).toBeInTheDocument()
  expect(await within(summaryCard).findByRole('group', { name: 'Watched markets: 1' })).toBeInTheDocument()
  expect(await within(summaryCard).findByRole('group', { name: 'Active paper: 2' })).toBeInTheDocument()
  expect(await screen.findAllByText('KX-ONE')).toHaveLength(2)
  expect(screen.getByText(/2 active Kalshi paper strategies/i)).toBeInTheDocument()
  expect(screen.getByText(/Latest discovery run: completed/i)).toBeInTheDocument()

  expect(within(screen.getByTestId('kalshi-overview-section')).getByRole('link', { name: /open polymarket hub/i })).toHaveAttribute('href', '/polymarket')
  expect(within(screen.getByTestId('kalshi-overview-section')).getByRole('link', { name: /open surfers ops/i })).toHaveAttribute('href', '/surfers/ops')
  expect(screen.getAllByRole('link', { name: /polymarket hub/i }).some((link) => link.getAttribute('href') === '/polymarket')).toBe(true)
  expect(screen.getAllByRole('link', { name: /surfers ops/i }).some((link) => link.getAttribute('href') === '/surfers/ops')).toBe(true)
  expect(screen.getByRole('link', { name: /open runbook/i })).toHaveAttribute('href', '/docs/runbooks/kalshi-paper-data.md')
  expect(screen.getByText(/live trade execution stays disabled/i)).toBeInTheDocument()
})
