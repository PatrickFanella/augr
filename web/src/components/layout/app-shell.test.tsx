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

    expect(screen.getByRole('heading', { name: 'Frontend scaffold' })).toBeInTheDocument()
    expect(screen.getByRole('link', { name: /portfolio/i })).toHaveAttribute('aria-current', 'page')
    expect(screen.getByText('Portfolio page')).toBeInTheDocument()
  })
})
