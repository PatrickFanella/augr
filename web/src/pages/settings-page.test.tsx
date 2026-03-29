import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { cleanup, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { SettingsPage } from '@/pages/settings-page'

const settingsResponse = {
  llm: {
    default_provider: 'openai',
    deep_think_model: 'gpt-5.2',
    quick_think_model: 'gpt-5-mini',
    providers: {
      openai: {
        api_key_configured: true,
        api_key_last4: '1234',
        base_url: 'https://api.openai.com/v1',
        model: 'gpt-5-mini',
      },
      anthropic: {
        api_key_configured: false,
        model: 'claude-3-7-sonnet-latest',
      },
      google: {
        api_key_configured: false,
        model: 'gemini-2.5-flash',
      },
      openrouter: {
        api_key_configured: true,
        api_key_last4: '5678',
        base_url: 'https://openrouter.ai/api/v1',
        model: 'openai/gpt-4.1-mini',
      },
      xai: {
        api_key_configured: false,
        base_url: 'https://api.x.ai/v1',
        model: 'grok-3-mini',
      },
      ollama: {
        base_url: 'http://localhost:11434',
        model: 'llama3.2',
      },
    },
  },
  risk: {
    max_position_size_pct: 10,
    max_daily_loss_pct: 2,
    max_drawdown_pct: 8,
    max_open_positions: 6,
    max_total_exposure_pct: 80,
    max_per_market_exposure_pct: 40,
    circuit_breaker_threshold_pct: 5,
    circuit_breaker_cooldown_min: 15,
  },
  system: {
    environment: 'development',
    version: 'v1.2.3',
    uptime_seconds: 5400,
    connected_brokers: [
      { name: 'alpaca', paper_mode: true, configured: true },
      { name: 'binance', paper_mode: false, configured: false },
    ],
  },
}

const riskStatusResponse = {
  risk_status: 'warning',
  circuit_breaker: { state: 'cooldown', reason: 'Loss threshold hit' },
  kill_switch: { active: false, reason: '', mechanisms: [], activated_at: null },
  position_limits: {
    max_per_position_pct: 10,
    max_total_pct: 80,
    max_concurrent: 6,
    max_per_market_pct: 40,
  },
  updated_at: '2025-01-01T00:00:00Z',
}

function Wrapper({ children }: { children: React.ReactNode }) {
  const client = new QueryClient({
    defaultOptions: {
      queries: { retry: false, refetchOnWindowFocus: false, refetchOnReconnect: false },
    },
  })

  return <QueryClientProvider client={client}>{children}</QueryClientProvider>
}

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})

describe('SettingsPage', () => {
  it('renders provider, risk, kill switch, and system info sections', async () => {
    const fetchMock = vi.fn((input: RequestInfo | URL) => {
      const url = typeof input === 'string' ? input : input.toString()

      if (url.includes('/api/v1/settings')) {
        return Promise.resolve({
          ok: true,
          status: 200,
          json: async () => settingsResponse,
        })
      }

      return Promise.resolve({
        ok: true,
        status: 200,
        json: async () => riskStatusResponse,
      })
    })
    vi.stubGlobal('fetch', fetchMock)

    render(<SettingsPage />, { wrapper: Wrapper })

    expect(await screen.findByTestId('settings-page')).toBeInTheDocument()
    expect(screen.getByText('Provider configuration')).toBeInTheDocument()
    expect(screen.getByText('Risk limits')).toBeInTheDocument()
    expect(screen.getByText('Kill switch')).toBeInTheDocument()
    expect(screen.getByTestId('system-info')).toBeInTheDocument()
    expect(screen.getByText('Configured ••••1234')).toBeInTheDocument()
    expect(screen.getByText('v1.2.3')).toBeInTheDocument()
    expect(screen.getByText('1h 30m')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Activate' })).toBeInTheDocument()
  })

  it('saves edited settings through the API', async () => {
    const user = userEvent.setup()
    let currentSettings = structuredClone(settingsResponse)
    let lastUpdateBody: unknown

    const fetchMock = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = typeof input === 'string' ? input : input.toString()

      if (url.includes('/api/v1/settings') && init?.method === 'PUT') {
        lastUpdateBody = JSON.parse(String(init.body))
        currentSettings = {
          ...currentSettings,
          ...(lastUpdateBody as typeof currentSettings),
          llm: {
            ...currentSettings.llm,
            ...(lastUpdateBody as typeof currentSettings).llm,
            providers: {
              ...currentSettings.llm.providers,
              ...(lastUpdateBody as typeof currentSettings).llm.providers,
              openai: {
                ...currentSettings.llm.providers.openai,
                ...((lastUpdateBody as typeof currentSettings).llm.providers.openai ?? {}),
                api_key_configured: true,
                api_key_last4: '4321',
              },
            },
          },
        }

        return {
          ok: true,
          status: 200,
          json: async () => currentSettings,
        }
      }

      if (url.includes('/api/v1/settings')) {
        return {
          ok: true,
          status: 200,
          json: async () => currentSettings,
        }
      }

      return {
        ok: true,
        status: 200,
        json: async () => riskStatusResponse,
      }
    })
    vi.stubGlobal('fetch', fetchMock)

    render(<SettingsPage />, { wrapper: Wrapper })

    await screen.findByTestId('settings-page')

    await user.clear(screen.getByLabelText('Deep think model'))
    await user.type(screen.getByLabelText('Deep think model'), 'claude-4-sonnet')
    await user.clear(screen.getByLabelText('Max open positions'))
    await user.type(screen.getByLabelText('Max open positions'), '12')
    await user.type(screen.getByLabelText('API key', { selector: '#openai-api-key' }), 'sk-new-4321')

    await user.click(screen.getByTestId('settings-save-button'))

    await waitFor(() => expect(screen.getByText('Settings saved.')).toBeInTheDocument())
    expect(lastUpdateBody).toMatchObject({
      llm: {
        deep_think_model: 'claude-4-sonnet',
        providers: {
          openai: {
            api_key: 'sk-new-4321',
          },
        },
      },
      risk: {
        max_open_positions: 12,
      },
    })
  })

  it('toggles the kill switch through the risk API', async () => {
    const user = userEvent.setup()
    const fetchMock = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = typeof input === 'string' ? input : input.toString()

      if (url.includes('/api/v1/risk/killswitch')) {
        return {
          ok: true,
          status: 200,
          json: async () => ({ active: true }),
        }
      }

      if (url.includes('/api/v1/risk/status')) {
        return {
          ok: true,
          status: 200,
          json: async () => ({
            ...riskStatusResponse,
            kill_switch:
              fetchMock.mock.calls.find(([, requestInit]) =>
                typeof input !== 'string' && requestInit?.method === 'POST',
              ) !== undefined
                ? {
                    active: true,
                    reason: 'Activated from settings page',
                    mechanisms: ['api_toggle'],
                    activated_at: '2025-01-01T00:00:00Z',
                  }
                : riskStatusResponse.kill_switch,
          }),
        }
      }

      return {
        ok: true,
        status: 200,
        json: async () => settingsResponse,
      }
    })
    vi.stubGlobal('fetch', fetchMock)

    render(<SettingsPage />, { wrapper: Wrapper })

    await screen.findByTestId('settings-page')
    await user.click(screen.getByTestId('settings-kill-switch-button'))

    await waitFor(() => {
      expect(fetchMock).toHaveBeenCalledWith(
        expect.any(URL),
        expect.objectContaining({
          method: 'POST',
          body: JSON.stringify({
            active: true,
            reason: 'Activated from settings page',
          }),
        }),
      )
    })
  })
})
