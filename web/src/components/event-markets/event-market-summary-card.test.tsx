import { render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import { EventMarketSummaryCard } from './event-market-summary-card'

describe('EventMarketSummaryCard', () => {
  it('renders paper/data-only summary copy without live claims', () => {
    render(
      <EventMarketSummaryCard
        provider="Kalshi"
        watchedMarkets={15}
        activePaper={1}
        lastRunStatus="completed"
        liveTradingReady={false}
      />,
    )

    expect(screen.getByTestId('event-market-summary-card')).toBeInTheDocument()
    expect(screen.getByRole('heading', { name: 'Kalshi' })).toBeInTheDocument()
    expect(screen.getByRole('status', { name: /paper\/data only warn/i })).toBeInTheDocument()
    expect(screen.getByRole('group', { name: 'Watched markets: 15' })).toBeInTheDocument()
    expect(screen.getByRole('group', { name: 'Active paper: 1' })).toBeInTheDocument()
    expect(screen.getByText(/paper\/data status at a glance/i)).toBeInTheDocument()
    expect(screen.getByText(/no live submit/i)).toBeInTheDocument()
  })
})
