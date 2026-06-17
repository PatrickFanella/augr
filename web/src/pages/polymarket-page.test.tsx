import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { cleanup, fireEvent, render, screen, within, waitFor } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, expect, it, vi } from 'vitest'

import { PolymarketPage } from '@/pages/polymarket-page'

const apiMocks = vi.hoisted(() => ({
  listStrategies: vi.fn(),
  listPolymarketAccounts: vi.fn(),
  listPolymarketWatched: vi.fn(),
  getPolymarketJobsStatus: vi.fn(),
  listPolymarketRecentTrades: vi.fn(),
  listPolymarketRecentSignals: vi.fn(),
  listPolymarketDiscoveryLast: vi.fn(),
  runPolymarketDiscovery: vi.fn(),
  addPolymarketWatched: vi.fn(),
  removePolymarketWatched: vi.fn(),
  setPolymarketWatchedEnabled: vi.fn(),
  setPolymarketAccountTracked: vi.fn(),
  runAutomationJob: vi.fn(),
  listPolymarketOpportunities: vi.fn(),
}))

vi.mock('@/lib/api/client', async () => {
  const actual = await vi.importActual<typeof import('@/lib/api/client')>('@/lib/api/client')
  return { ...actual, apiClient: apiMocks as unknown as typeof actual.apiClient }
})

vi.mock('@/hooks/use-websocket-client', () => ({
  useWebSocketClient: () => ({ status: 'open', sendCommand: vi.fn().mockReturnValue(true) }),
}))

function Wrapper({ children }: { children: React.ReactNode }) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return <QueryClientProvider client={client}><MemoryRouter>{children}</MemoryRouter></QueryClientProvider>
}

afterEach(() => cleanup())

const baseAccount = {
  address: '0xabc',
  first_seen: '2025-01-01T00:00:00Z',
  total_trades: 1,
  total_volume: 10,
  markets_entered: 1,
  markets_won: 1,
  markets_lost: 0,
  win_rate: 1,
  resolved_markets: 1,
  bayesian_win_rate: 0.9,
  consistency_score: 1.23,
  avg_position: 1,
  max_position: 2,
  early_entry_rate: 0.5,
  tracked: true,
  updated_at: '2025-01-01T00:00:00Z',
}

function mockBaseResponses() {
  apiMocks.listStrategies.mockResolvedValue({ data: [{ id: '1', name: 'PM', market_type: 'polymarket' }] })
  apiMocks.listPolymarketAccounts.mockResolvedValue({ data: [baseAccount], limit: 25, offset: 0 })
  apiMocks.listPolymarketWatched.mockResolvedValue({ data: [] })
  apiMocks.getPolymarketJobsStatus.mockResolvedValue([])
  apiMocks.listPolymarketRecentTrades.mockResolvedValue({ data: [] })
  apiMocks.listPolymarketRecentSignals.mockResolvedValue({ data: [] })
  apiMocks.listPolymarketDiscoveryLast.mockResolvedValue({ last: null })
  apiMocks.runPolymarketDiscovery.mockResolvedValue({ status: 'ok', message: 'started' })
  apiMocks.addPolymarketWatched.mockResolvedValue({ slug: 'fed-rate-cut-december-2026', enabled: true, added_at: '2025-01-01T00:00:00Z' })
  apiMocks.removePolymarketWatched.mockResolvedValue(undefined)
  apiMocks.setPolymarketWatchedEnabled.mockResolvedValue({ slug: 'fed-rate-cut-december-2026', enabled: true, added_at: '2025-01-01T00:00:00Z' })
  apiMocks.setPolymarketAccountTracked.mockResolvedValue({ ok: true })
  apiMocks.runAutomationJob.mockResolvedValue({ ok: true })
  apiMocks.listPolymarketOpportunities.mockResolvedValue({ data: [] })
}

it('preset fills scanner fields', async () => {
  mockBaseResponses()

  render(<PolymarketPage />, { wrapper: Wrapper })

  expect(await screen.findByTestId('polymarket-page')).toBeInTheDocument()
  const sectionNav = screen.getByRole('navigation', { name: /polymarket sections/i })
  expect(within(sectionNav).getByRole('link', { name: 'Overview' })).toHaveAttribute('href', '#overview')
  expect(within(sectionNav).getByRole('link', { name: 'Intelligence' })).toHaveAttribute('href', '#intelligence')
  expect(within(sectionNav).getByRole('link', { name: 'Accounts' })).toHaveAttribute('href', '#accounts')
  expect(screen.getByTestId('polymarket-overview-section')).toBeInTheDocument()
  expect(screen.getByTestId('polymarket-intelligence-section')).toBeInTheDocument()
  expect(screen.getByTestId('polymarket-accounts-section')).toBeInTheDocument()
  expect(screen.getByTestId('polymarket-watched-section')).toBeInTheDocument()
  expect(screen.getByTestId('polymarket-operations-section')).toBeInTheDocument()
  expect(screen.getByTestId('polymarket-discovery-section')).toBeInTheDocument()
  expect(screen.getByRole('link', { name: /open kalshi hub/i })).toHaveAttribute('href', '/kalshi')

  fireEvent.click(screen.getByTestId('polymarket-scanner-preset-event-momentum'))

  await waitFor(() => {
    expect(screen.getByLabelText('Market slug')).toHaveValue('')
    expect(screen.getByLabelText('Outcome')).toHaveValue('Yes')
    expect(screen.getByLabelText('Probability')).toHaveValue('0.54')
    expect(screen.getByLabelText('Best bid')).toHaveValue('0.51')
    expect(screen.getByLabelText('Best ask')).toHaveValue('0.56')
  })
  expect(screen.getByText(/Preset draft: Event momentum/i)).toBeInTheDocument()
  expect(apiMocks.listPolymarketOpportunities).not.toHaveBeenCalled()
  fireEvent.click(screen.getByTestId('polymarket-scanner-reset'))
  await waitFor(() => expect(screen.getByLabelText('Market slug')).toHaveValue(''))
})

it('default market suggestion triggers add watched', async () => {
  mockBaseResponses()

  render(<PolymarketPage />, { wrapper: Wrapper })

  await screen.findByTestId('polymarket-page')
  fireEvent.click(screen.getByTestId('polymarket-watched-suggestion-fed-cut'))

  await waitFor(() => {
    expect(apiMocks.addPolymarketWatched).toHaveBeenCalledWith('fed-rate-cut-december-2026', 'Macro catalyst with frequent repricing around CPI and FOMC data.')
  })
})

it('signal details render', async () => {
  mockBaseResponses()
  apiMocks.listPolymarketRecentSignals.mockResolvedValue({
    data: [
      {
        id: 's1',
        received_at: '2025-01-01T00:00:00Z',
        source: 'polymarket-whale',
        title: 'Signal',
        body: 'Whale accumulation on a macro market.',
        urgency: 4,
        summary: 'Summary text',
        recommended_action: 'Watch for breakout',
        affected_strategy_ids: ['st-1', 'st-2'],
        metadata: { market: 'fed-rate-cut-december-2026', strategy: 'pm-core' },
      },
    ],
  })

  render(<PolymarketPage />, { wrapper: Wrapper })

  const signalCard = await screen.findByTestId('polymarket-signal-s1')
  expect(within(signalCard).getByText('Summary text')).toBeInTheDocument()
  expect(within(signalCard).getByText('Whale accumulation on a macro market.')).toBeInTheDocument()
  expect(within(signalCard).getByText('Watch for breakout')).toBeInTheDocument()
  expect(within(signalCard).getByText('Urgency 4')).toBeInTheDocument()
  expect(within(signalCard).getByText('2 strategies')).toBeInTheDocument()
  expect(within(signalCard).getByText(/"market": "fed-rate-cut-december-2026"/)).toBeInTheDocument()
})

it('job state details render', async () => {
  mockBaseResponses()
  apiMocks.getPolymarketJobsStatus.mockResolvedValue([
    {
      name: 'polymarket_whales',
      description: 'Scan whale trades and publish signals',
      schedule: 'Every 20 minutes',
      last_run: '2025-01-02T00:00:00Z',
      last_result: 'partial success',
      last_error: 'timeout while fetching market data',
      run_count: 12,
      error_count: 3,
      consecutive_failures: 1,
      running: false,
      enabled: true,
    },
  ])

  render(<PolymarketPage />, { wrapper: Wrapper })

  const jobCard = await screen.findByTestId('polymarket-job-polymarket_whales')
  expect(within(jobCard).getByText('Scan whale trades and publish signals')).toBeInTheDocument()
  expect(within(jobCard).getByText('Every 20 minutes')).toBeInTheDocument()
  expect(within(jobCard).getByText('Backend schedule description')).toBeInTheDocument()
  expect(within(jobCard).getByText('enabled')).toBeInTheDocument()
  expect(within(jobCard).getByText('active failure')).toBeInTheDocument()
  expect(within(jobCard).getByText('partial success')).toBeInTheDocument()
  expect(within(jobCard).getByText('12')).toBeInTheDocument()
  expect(within(jobCard).getByText('3')).toBeInTheDocument()
})

it('does not mark historical job errors as active failures', async () => {
  mockBaseResponses()
  apiMocks.getPolymarketJobsStatus.mockResolvedValue([
    {
      name: 'polymarket_profiles',
      description: 'Refresh tracked wallet profiles',
      schedule: 'Every 20 minutes',
      last_run: '2025-01-02T00:00:00Z',
      last_result: 'success',
      run_count: 12,
      error_count: 3,
      consecutive_failures: 0,
      running: false,
      enabled: true,
    },
  ])

  render(<PolymarketPage />, { wrapper: Wrapper })

  const jobCard = await screen.findByTestId('polymarket-job-polymarket_profiles')
  expect(within(jobCard).queryByText('active failure')).not.toBeInTheDocument()
  expect(within(jobCard).getByText('stable')).toBeInTheDocument()
  expect(within(jobCard).getByText(/Historical errors: 3/i)).toBeInTheDocument()
})
