import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { CalendarPage } from '@/pages/calendar-page'

const apiClientMock = vi.hoisted(() => ({
  getEarningsCalendar: vi.fn(),
  getEconomicCalendar: vi.fn(),
  getFilings: vi.fn(),
  getIPOCalendar: vi.fn(),
  analyzeFiling: vi.fn(),
}))

vi.mock('@/lib/api/client', () => ({
  apiClient: apiClientMock,
}))

function Wrapper({ children }: { children: React.ReactNode }) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false, refetchOnWindowFocus: false } } })
  return (
    <QueryClientProvider client={client}>
      <MemoryRouter>{children}</MemoryRouter>
    </QueryClientProvider>
  )
}

afterEach(() => {
  cleanup()
  vi.clearAllMocks()
})

describe('CalendarPage', () => {
  it('shows the month summary, filing analysis state, and a local-only note form', async () => {
    apiClientMock.getEarningsCalendar.mockResolvedValue([
      {
        symbol: 'AAPL',
        date: '2026-06-15T12:00:00Z',
        hour: 'bmo',
        quarter: 2,
        year: 2026,
      },
    ])
    apiClientMock.getEconomicCalendar.mockResolvedValue([
      {
        event: 'FOMC Statement',
        country: 'US',
        time: '2026-06-17T12:00:00Z',
        impact: 'high',
        unit: '%',
      },
    ])
    apiClientMock.getFilings.mockResolvedValue([
      {
        symbol: 'AAPL',
        form: '10-Q',
        filed_date: '2026-06-18T12:00:00Z',
        accepted_date: '2026-06-18T12:00:00Z',
        report_date: '2026-06-18T12:00:00Z',
        url: 'https://www.sec.gov/Archives/edgar/data/0000320193/0000320193-26-000010.txt',
        access_number: '0000320193-26-000010',
      },
    ])
    apiClientMock.getIPOCalendar.mockResolvedValue([
      {
        symbol: 'XYZ',
        date: '2026-06-19T12:00:00Z',
        exchange: 'NYSE',
        name: 'Example Corp',
        price_range: '$10-$12',
        shares_offered: 1000000,
        status: 'filed',
      },
    ])
    apiClientMock.analyzeFiling.mockResolvedValue({
      symbol: 'AAPL',
      form: '10-Q',
      filed_date: '2026-06-18T12:00:00Z',
      sentiment: 'bullish',
      impact: 'high',
      summary: 'Margins improved and the filing was constructive.',
      action: 'buy',
      confidence: 0.82,
      key_items: ['Margins improved'],
      reasoning: 'Revenue quality improved.',
    })

    render(<CalendarPage />, { wrapper: Wrapper })

    expect(await screen.findByTestId('calendar-page')).toBeInTheDocument()
    expect(screen.getByText('This month at a glance')).toBeInTheDocument()
    expect(screen.getByText('Local session note')).toBeInTheDocument()
    expect(screen.getByText(/session only/i)).toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: 'SEC Filings' }))
    expect(await screen.findByRole('button', { name: 'Analyze' })).toBeInTheDocument()

    fireEvent.change(screen.getByPlaceholderText('Why this event matters'), {
      target: { value: 'Watch after earnings' },
    })
    fireEvent.click(screen.getByRole('button', { name: 'Add local note' }))

    expect(screen.getByText('Watch after earnings')).toBeInTheDocument()
  }, 10_000)
})
