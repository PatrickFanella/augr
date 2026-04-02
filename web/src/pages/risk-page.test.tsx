import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { RiskPage } from '@/pages/risk-page'
import type { EngineStatus } from '@/lib/api/types'

const mockEngineStatus: EngineStatus = {
  risk_status: 'normal',
  circuit_breaker: { state: 'open', reason: '' },
  kill_switch: { active: false, reason: '', mechanisms: [] },
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

  it('shows circuit breaker open badge, inactive kill switch, and audit log table', async () => {
    const fetchMock = vi.fn((input: RequestInfo | URL) => {
      const url = typeof input === 'string' ? input : input.toString()
      if (url.includes('/api/v1/audit-log')) {
        return Promise.resolve({
          ok: true,
          status: 200,
          json: async () => ({
            data: [
              {
                id: 'audit-1',
                event_type: 'kill_switch_toggled',
                entity_type: 'risk',
                details: { reason: 'manual test' },
                created_at: '2025-01-01T00:00:00Z',
              },
            ],
            limit: 10,
            offset: 0,
          }),
        })
      }

      return Promise.resolve({
        ok: true,
        status: 200,
        json: async () => mockEngineStatus,
      })
    })
    vi.stubGlobal('fetch', fetchMock)

    render(<RiskPage />, { wrapper: Wrapper })

    expect(screen.getByTestId('risk-page')).toBeInTheDocument()
    expect(await screen.findByText('Open')).toBeInTheDocument()
    expect(screen.getByText('Inactive')).toBeInTheDocument()
    expect(screen.getByTestId('kill-switch-toggle')).toHaveTextContent('Activate')
    expect(await screen.findByTestId('audit-log-table')).toBeInTheDocument()
    expect(screen.getByText('kill_switch_toggled')).toBeInTheDocument()
    expect(screen.getByText('risk')).toBeInTheDocument()
  })

  it('shows loading skeletons while fetching', () => {
    const fetchMock = vi.fn((input: RequestInfo | URL) => {
      const url = typeof input === 'string' ? input : input.toString()
      if (url.includes('/api/v1/audit-log')) {
        return new Promise(() => {})
      }
      return new Promise(() => {})
    })
    vi.stubGlobal('fetch', fetchMock)

    render(<RiskPage />, { wrapper: Wrapper })

    expect(screen.getByTestId('circuit-breaker-loading')).toBeInTheDocument()
    expect(screen.getByTestId('kill-switch-loading')).toBeInTheDocument()
    expect(screen.getByTestId('audit-log-loading')).toBeInTheDocument()
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

    const fetchMock = vi.fn((input: RequestInfo | URL) => {
      const url = typeof input === 'string' ? input : input.toString()
      if (url.includes('/api/v1/audit-log')) {
        return Promise.resolve({
          ok: true,
          status: 200,
          json: async () => ({ data: [], limit: 10, offset: 0 }),
        })
      }

      return Promise.resolve({
        ok: true,
        status: 200,
        json: async () => activeStatus,
      })
    })
    vi.stubGlobal('fetch', fetchMock)

    render(<RiskPage />, { wrapper: Wrapper })

    expect(await screen.findByText('Active')).toBeInTheDocument()
    expect(screen.getByText('Emergency halt')).toBeInTheDocument()
    expect(screen.getByTestId('kill-switch-toggle')).toHaveTextContent('Deactivate')
  })

  it('loads more audit entries when requested', async () => {
    const fetchMock = vi.fn((input: RequestInfo | URL) => {
      const url = typeof input === 'string' ? input : input.toString()
      if (url.includes('/api/v1/audit-log')) {
        const parsed = new URL(url)
        const limit = Number(parsed.searchParams.get('limit') ?? '10')
        return Promise.resolve({
          ok: true,
          status: 200,
          json: async () => ({
            data: Array.from({ length: limit }, (_, index) => ({
              id: `audit-${index}`,
              event_type: 'kill_switch_toggled',
              entity_type: 'risk',
              details: { index },
              created_at: '2025-01-01T00:00:00Z',
            })),
            limit,
            offset: 0,
          }),
        })
      }

      return Promise.resolve({
        ok: true,
        status: 200,
        json: async () => mockEngineStatus,
      })
    })
    vi.stubGlobal('fetch', fetchMock)

    render(<RiskPage />, { wrapper: Wrapper })

    expect(await screen.findByTestId('load-more-audit')).toBeInTheDocument()
    fireEvent.click(screen.getByTestId('load-more-audit'))

    await waitFor(() => {
      const auditCalls = fetchMock.mock.calls
        .map((call) => call[0].toString())
        .filter((url) => url.includes('/api/v1/audit-log'))
      expect(auditCalls.some((url) => url.includes('limit=20'))).toBe(true)
    })
  })

  it('shows empty audit log state', async () => {
    const fetchMock = vi.fn((input: RequestInfo | URL) => {
      const url = typeof input === 'string' ? input : input.toString()
      if (url.includes('/api/v1/audit-log')) {
        return Promise.resolve({
          ok: true,
          status: 200,
          json: async () => ({ data: [], limit: 10, offset: 0 }),
        })
      }

      return Promise.resolve({
        ok: true,
        status: 200,
        json: async () => mockEngineStatus,
      })
    })
    vi.stubGlobal('fetch', fetchMock)

    render(<RiskPage />, { wrapper: Wrapper })

    expect(await screen.findByText('No audit log entries.')).toBeInTheDocument()
  })
})
