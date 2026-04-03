import { render, screen } from '@testing-library/react'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { describe, expect, it } from 'vitest'

import { AppShell } from '@/components/layout/app-shell'

describe('AppShell', () => {
  it('renders the navigation and nested route content', () => {
    render(
      <MemoryRouter initialEntries={['/portfolio']}>
        <Routes>
          <Route element={<AppShell />}>
            <Route path="/portfolio" element={<div>Portfolio page</div>} />
          </Route>
        </Routes>
      </MemoryRouter>,
    )

    expect(screen.getAllByText('Get Rich Quick').length).toBeGreaterThanOrEqual(1)
    expect(screen.getAllByRole('link', { name: /memories/i }).length).toBeGreaterThanOrEqual(1)
    expect(screen.getAllByRole('link', { name: /settings/i }).length).toBeGreaterThanOrEqual(1)
    const portfolioLinks = screen.getAllByRole('link', { name: /portfolio/i })
    expect(portfolioLinks.some((link) => link.getAttribute('aria-current') === 'page')).toBe(true)
    expect(screen.getByText('Portfolio page')).toBeInTheDocument()
  })
})
