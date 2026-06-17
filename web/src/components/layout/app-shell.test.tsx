import { render, screen } from '@testing-library/react'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { afterEach, describe, expect, it } from 'vitest'

import { AppShell } from '@/components/layout/app-shell'
import { clearTokens, setTokens } from '@/lib/auth'

afterEach(() => {
  clearTokens()
})

describe('AppShell', () => {
  it('renders the navigation and nested route content', () => {
    setTokens('access-token', 'refresh-token', Date.now() + 60_000)

    render(
      <MemoryRouter initialEntries={['/portfolio']}>
        <Routes>
          <Route element={<AppShell />}>
            <Route path="/portfolio" element={<div>Portfolio page</div>} />
          </Route>
        </Routes>
      </MemoryRouter>,
    )

    expect(screen.getAllByText('Augr').length).toBeGreaterThanOrEqual(1)
    expect(screen.getAllByRole('link', { name: /memories/i }).length).toBeGreaterThanOrEqual(1)
    expect(screen.getAllByRole('link', { name: /settings/i }).length).toBeGreaterThanOrEqual(1)
    expect(screen.getAllByText(/Event Markets/i).length).toBeGreaterThanOrEqual(1)
    expect(screen.getAllByRole('link', { name: /kalshi/i }).length).toBeGreaterThanOrEqual(1)
    expect(screen.getAllByRole('link', { name: /surfers ops/i }).length).toBeGreaterThanOrEqual(1)
    const portfolioLinks = screen.getAllByRole('link', { name: /portfolio/i })
    expect(portfolioLinks.some((link) => link.getAttribute('aria-current') === 'page')).toBe(true)
    expect(screen.getByText('Portfolio page')).toBeInTheDocument()

    const shellStatus = screen.getByRole('contentinfo', { name: /shell status/i })
    expect(shellStatus).toHaveTextContent(/env/i)
    expect(shellStatus).toHaveTextContent(/auth/i)
    expect(shellStatus).toHaveTextContent(/route/i)
    expect(shellStatus).toHaveTextContent(/runtime/i)

    const desktopNav = screen.getByRole('navigation', { name: /^primary$/i })
    expect(desktopNav).toBeInTheDocument()
    expect(desktopNav).toHaveClass('hud-scrollbar')

    const main = document.querySelector('main')
    expect(main).toHaveClass('hud-scrollbar')
    expect(main).toHaveClass('overflow-y-auto')
  }, 10_000)
})
