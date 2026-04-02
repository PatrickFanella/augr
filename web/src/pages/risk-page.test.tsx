import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { cleanup, render, screen } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { RiskPage } from '@/pages/risk-page'
import type { EngineStatus } from '@/lib/api/types'

const mockEngineStatus: EngineStatus = {
  risk_status: 'normal',
  circuit_breaker: { state: 'open', reason: '' },
  kill_switch: { active: false, reason: '', mechanisms: [], activated_at: null as unknown as string },
  position_limits: {
    max_per_position_pct: 10,
    max_total_pct: 80,
    max_concurrent: 5,
    max_per_market_pct: 40,
  },
  updated_at: '2025-01-01T00:00:00Z',
}

function Wrapper({ children }: { children: React.ReactNode }) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return <QueryClientProvider client={client}>{children}</QueryClientProvider>
}

describe('RiskPage', () => {
  afterEach(() => {
    cleanup()
    vi.unstubAllGlobals()
  })

  it('shows circuit breaker open badge and inactive kill switch with activate button', async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      json: async () => mockEngineStatus,
    })
    vi.stubGlobal('fetch', fetchMock)

    render(<RiskPage />, { wrapper: Wrapper })

    expect(screen.getByTestId('risk-page')).toBeInTheDocument()

    // Wait for data to load
    expect(await screen.findByText('Open')).toBeInTheDocument()
    expect(screen.getByText('Inactive')).toBeInTheDocument()
    expect(screen.getByTestId('kill-switch-toggle')).toHaveTextContent('Activate')
  })

  it('shows loading skeletons while fetching', () => {
    // Never resolve fetch
    const fetchMock = vi.fn().mockReturnValue(new Promise(() => {}))
    vi.stubGlobal('fetch', fetchMock)

    render(<RiskPage />, { wrapper: Wrapper })

    expect(screen.getByTestId('circuit-breaker-loading')).toBeInTheDocument()
    expect(screen.getByTestId('kill-switch-loading')).toBeInTheDocument()
  })

  it('shows active kill switch with deactivate button', async () => {
    const activeStatus: EngineStatus = {
      ...mockEngineStatus,
      kill_switch: {
        active: true,
        reason: 'Emergency halt',
        mechanisms: ['api_toggle'],
        activated_at: '2025-06-15T12:00:00Z',
      },
    }

    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      json: async () => activeStatus,
    })
    vi.stubGlobal('fetch', fetchMock)

    render(<RiskPage />, { wrapper: Wrapper })

    expect(await screen.findByText('Active')).toBeInTheDocument()
    expect(screen.getByText('Emergency halt')).toBeInTheDocument()
    expect(screen.getByTestId('kill-switch-toggle')).toHaveTextContent('Deactivate')
  })
})
