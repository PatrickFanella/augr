import '@testing-library/jest-dom/vitest'
import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { delay, http, HttpResponse } from 'msw'
import { describe, expect, it } from 'vitest'

import App from '@/App'
import { setTokenSnapshot } from '@/shared/auth/tokenStore'
import { buildAuthResponse, buildRiskBreakers, buildRiskStatus, fixtureDate, mockRefreshToken } from '@/test/fixtures'
import { apiBaseUrl, FakeWebSocket, installAppTestHarness, resetApp, server, state } from '@/test/app-harness'

describe('authentication and cockpit', () => {
  installAppTestHarness()

  it('logs in successfully and redirects to cockpit', async () => {
    resetApp('/login')
    render(<App />)
    await userEvent.type(screen.getByLabelText(/username/i), 'operator')
    await userEvent.type(screen.getByLabelText(/password/i), 'password')
    await userEvent.click(screen.getByRole('button', { name: /sign in/i }))

    expect(await screen.findByRole('heading', { name: /system overview/i })).toBeTruthy()
    expect(screen.getByText('dev-paper-operator')).toBeTruthy()
  }, 20_000)

  it('restores the session from a session refresh token after reload', async () => {
    resetApp('/cockpit')
    sessionStorage.setItem('augr.refresh-token.session', mockRefreshToken)
    render(<App />)

    expect(await screen.findByRole('heading', { name: /system overview/i })).toBeTruthy()
    expect(screen.getByText('dev-paper-operator')).toBeTruthy()
  })

  it('rejects login next targets that point back to login', async () => {
    resetApp('/login?next=/login')
    render(<App />)
    await userEvent.type(screen.getByLabelText(/username/i), 'operator')
    await userEvent.type(screen.getByLabelText(/password/i), 'password')
    await userEvent.click(screen.getByRole('button', { name: /sign in/i }))

    expect(await screen.findByRole('heading', { name: /system overview/i })).toBeTruthy()
  }, 10_000)

  it('does not reveal whether username exists for invalid credentials', async () => {
    resetApp('/login')
    state.scenario = 'invalid-credentials'
    render(<App />)
    await userEvent.type(screen.getByLabelText(/username/i), 'invalid')
    await userEvent.type(screen.getByLabelText(/password/i), 'bad')
    await userEvent.click(screen.getByRole('button', { name: /sign in/i }))

    expect(await screen.findByRole('alert')).toHaveTextContent('Invalid username or password.')
  })

  it('redirects protected routes to login', async () => {
    resetApp('/cockpit')
    render(<App />)
    expect(await screen.findByRole('heading', { name: /sign in/i })).toBeTruthy()
  })

  it('handles failed refresh by cleaning up the session', async () => {
    resetApp('/cockpit')
    state.scenario = 'failed-refresh'
    setTokenSnapshot(buildAuthResponse({ access_token: 'expired-access-token' }))
    server.use(
      http.get(`${apiBaseUrl}/me`, () => HttpResponse.json({ error: 'expired', code: 'ERR_UNAUTHORIZED' }, { status: 401 })),
    )
    render(<App />)
    expect(await screen.findByRole('heading', { name: /sign in/i })).toBeTruthy()
  })

  it('logs out and clears protected UI', async () => {
    resetApp('/cockpit')
    setTokenSnapshot(buildAuthResponse())
    render(<App />)
    expect(await screen.findByRole('heading', { name: /system overview/i })).toBeTruthy()
    await userEvent.click(screen.getByRole('button', { name: /logout/i }))
    expect(await screen.findByRole('heading', { name: /sign in/i })).toBeTruthy()
  })

  it('provides keyboard-accessible shell landmarks and skip navigation', async () => {
    resetApp('/cockpit')
    setTokenSnapshot(buildAuthResponse())
    render(<App />)

    expect(await screen.findByRole('heading', { name: /system overview/i })).toBeTruthy()
    const skip = screen.getByRole('link', { name: /skip to main content/i })
    expect(skip).toHaveAttribute('href', '#main-content')
    expect(screen.getByRole('navigation', { name: /primary/i })).toBeTruthy()
    expect(screen.getByRole('main')).toHaveAttribute('id', 'main-content')
    await userEvent.tab()
    expect(skip).toHaveFocus()
  })

  it('shows cockpit loading state during slow responses', async () => {
    resetApp('/cockpit')
    setTokenSnapshot(buildAuthResponse())
    server.use(
      http.get(`${apiBaseUrl}/risk/status`, async () => {
        await delay(1000)
        return HttpResponse.json({})
      }),
    )
    render(<App />)
    expect(await screen.findByRole('heading', { name: /system overview/i })).toBeTruthy()
    expect((await screen.findAllByText(/loading/i)).length).toBeGreaterThan(0)
  })

  it('classifies cockpit status and links shell widgets to entity routes', async () => {
    resetApp('/cockpit')
    setTokenSnapshot(buildAuthResponse())
    render(<App />)

    expect(await screen.findByText(/Cockpit classification: degraded/i)).toBeTruthy()
    expect(await screen.findByRole('heading', { name: /System health/i })).toBeTruthy()
    expect(await screen.findByRole('table', { name: /cockpit open positions/i })).toBeTruthy()
    expect(await screen.findByRole('table', { name: /cockpit recent orders/i })).toBeTruthy()
    expect(await screen.findByRole('table', { name: /cockpit recent trades/i })).toBeTruthy()
    expect(screen.getAllByRole('link', { name: /Order/i }).some((link) => link.getAttribute('href') === '/orders/00000000-0000-4000-8000-000000000040')).toBe(true)
    expect(screen.getAllByRole('link', { name: /Position/i }).some((link) => link.getAttribute('href') === '/trades?position_id=00000000-0000-4000-8000-000000000030')).toBe(true)
  })

  it('treats open circuit breaker state as safe when all cockpit signals are normal', async () => {
    resetApp('/cockpit')
    setTokenSnapshot(buildAuthResponse())
    server.use(
      http.get(`${apiBaseUrl}/risk/status`, () => HttpResponse.json(buildRiskStatus())),
      http.get(`${apiBaseUrl}/risk/cockpit`, () => HttpResponse.json({ generated_at: fixtureDate, kill_switch_active: false, circuit_breaker: false, exposures: [], warnings: [] })),
      http.get(`${apiBaseUrl}/risk/breakers`, () => HttpResponse.json(buildRiskBreakers({ tripped: [] }))),
    )
    render(<App />)

    await waitFor(() => expect(FakeWebSocket.instances[0]?.readyState).toBe(1))
    expect(await screen.findByText(/Cockpit classification: safe/i)).toBeTruthy()
  })

  it('shows empty active-runs state', async () => {
    resetApp('/cockpit')
    state.scenario = 'empty-data'
    setTokenSnapshot(buildAuthResponse())
    render(<App />)
    expect(await screen.findByText('No active runs.')).toBeTruthy()
  })

  it('shows partial service failure data', async () => {
    resetApp('/cockpit')
    state.scenario = 'partial-service-failure'
    setTokenSnapshot(buildAuthResponse())
    render(<App />)
    expect(await screen.findByText('warning')).toBeTruthy()
    expect(screen.getByText(/Cockpit classification: degraded/i)).toBeTruthy()
  })

  it('shows automation 501 as feature unavailable', async () => {
    resetApp('/cockpit')
    setTokenSnapshot(buildAuthResponse())
    server.use(
      http.get(`${apiBaseUrl}/automation/health`, () => HttpResponse.json({ error: 'not configured', code: 'ERR_NOT_IMPLEMENTED' }, { status: 501 })),
    )
    render(<App />)
    const panel = await screen.findByRole('heading', { name: /System health/i })
    expect(await within(panel.closest('section') as HTMLElement).findByText(/feature unavailable/i)).toBeTruthy()
  })

  it('keeps other cockpit data visible when one service returns 500', async () => {
    resetApp('/cockpit')
    setTokenSnapshot(buildAuthResponse())
    server.use(
      http.get(`${apiBaseUrl}/automation/health`, () => HttpResponse.json({ error: 'automation exploded', code: 'ERR_INTERNAL' }, { status: 500 })),
    )
    render(<App />)

    expect(await screen.findByRole('heading', { name: /system overview/i })).toBeTruthy()
    expect(await screen.findByText('normal')).toBeTruthy()
    const panel = await screen.findByRole('heading', { name: /System health/i })
    expect((await within(panel.closest('section') as HTMLElement).findByRole('alert')).textContent).toContain('automation exploded')
  })

})
