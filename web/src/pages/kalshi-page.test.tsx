import { render, screen, within } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { expect, it } from 'vitest'

import { KalshiPage } from '@/pages/kalshi-page'

it('renders a paper/data-first Kalshi hub with cross-links', () => {
  render(
    <MemoryRouter>
      <KalshiPage />
    </MemoryRouter>,
  )

  expect(screen.getByTestId('kalshi-page')).toBeInTheDocument()
  expect(screen.getByRole('heading', { name: 'Kalshi' })).toBeInTheDocument()
  expect(screen.getByText(/paper\/data-first Kalshi hub/i)).toBeInTheDocument()

  const sectionNav = screen.getByRole('navigation', { name: /kalshi sections/i })
  expect(within(sectionNav).getByRole('link', { name: 'Overview' })).toHaveAttribute('href', '#overview')
  expect(within(sectionNav).getByRole('link', { name: 'Setup' })).toHaveAttribute('href', '#setup')

  expect(screen.getByTestId('kalshi-overview-section')).toBeInTheDocument()
  expect(screen.getByTestId('kalshi-markets-section')).toBeInTheDocument()
  expect(screen.getByTestId('kalshi-paper-strategies-section')).toBeInTheDocument()
  expect(screen.getByTestId('kalshi-operations-section')).toBeInTheDocument()
  expect(screen.getByTestId('kalshi-setup-section')).toBeInTheDocument()

  expect(within(screen.getByTestId('kalshi-overview-section')).getByRole('link', { name: /open polymarket hub/i })).toHaveAttribute('href', '/polymarket')
  expect(within(screen.getByTestId('kalshi-overview-section')).getByRole('link', { name: /open surfers ops/i })).toHaveAttribute('href', '/surfers/ops')
  expect(screen.getAllByRole('link', { name: /polymarket hub/i }).some((link) => link.getAttribute('href') === '/polymarket')).toBe(true)
  expect(screen.getAllByRole('link', { name: /surfers ops/i }).some((link) => link.getAttribute('href') === '/surfers/ops')).toBe(true)
  expect(screen.getByRole('link', { name: /open runbook/i })).toHaveAttribute('href', '/docs/runbooks/kalshi-paper-data.md')
  expect(screen.getByText(/live trade execution stays disabled/i)).toBeInTheDocument()
})
