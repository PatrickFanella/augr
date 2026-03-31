import { cleanup, render, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { AppRoutes } from '@/App'
import { isAuthenticated } from '@/lib/auth'

vi.mock('@/lib/auth', () => ({
  isAuthenticated: vi.fn(),
}))

vi.mock('@/pages/dashboard-page', () => ({
  DashboardPage: () => <div>Dashboard page</div>,
}))

vi.mock('@/pages/runs-page', () => ({
  RunsPage: () => <div>Runs page</div>,
}))

describe('AppRoutes auth guards', () => {
  beforeEach(() => {
    vi.mocked(isAuthenticated).mockReset()
  })

  afterEach(() => {
    cleanup()
  })

  it('redirects unauthenticated users from protected routes to /login', () => {
    vi.mocked(isAuthenticated).mockReturnValue(false)

    render(
      <MemoryRouter initialEntries={['/portfolio']}>
        <AppRoutes />
      </MemoryRouter>,
    )

    expect(screen.getByRole('heading', { name: 'Login' })).toBeInTheDocument()
    expect(screen.queryByRole('heading', { name: 'Frontend scaffold' })).not.toBeInTheDocument()
  })

  it('redirects authenticated users away from /login to /', () => {
    vi.mocked(isAuthenticated).mockReturnValue(true)

    render(
      <MemoryRouter initialEntries={['/login']}>
        <AppRoutes />
      </MemoryRouter>,
    )

    expect(screen.getByText('Dashboard page')).toBeInTheDocument()
    expect(screen.queryByRole('heading', { name: 'Login' })).not.toBeInTheDocument()
  })

  it('renders the runs page at /runs for authenticated users', () => {
    vi.mocked(isAuthenticated).mockReturnValue(true)

    render(
      <MemoryRouter initialEntries={['/runs']}>
        <AppRoutes />
      </MemoryRouter>,
    )

    expect(screen.getByText('Runs page')).toBeInTheDocument()
  })
})
