import '@testing-library/jest-dom/vitest'
import { act, cleanup, render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { http, HttpResponse } from 'msw'
import { describe, expect, it } from 'vitest'

import App from '@/App'
import { setTokenSnapshot } from '@/shared/auth/tokenStore'
import { buildAuthResponse, buildRiskBreakers, buildRiskStatus, fixtureDate } from '@/test/fixtures'
import { apiBaseUrl, FakeWebSocket, installAppTestHarness, resetApp, server, state } from '@/test/app-harness'
import { createP0RestHandlers } from '@/test/mocks/rest'

describe('risk console', () => {
  installAppTestHarness()

  it('renders risk console status, cockpit exposure, and tripped breakers', async () => {
    resetApp('/risk')
    setTokenSnapshot(buildAuthResponse())
    render(<App />)

    expect(await screen.findByRole('heading', { name: /^risk$/i })).toBeTruthy()
    expect(await screen.findByRole('heading', { name: /risk engine status/i })).toBeTruthy()
    expect(await screen.findByRole('table', { name: /risk exposures/i })).toBeTruthy()
    expect(await screen.findByRole('table', { name: /tripped breakers/i })).toBeTruthy()
    expect(screen.getByText(/paper drawdown guard/i)).toBeTruthy()
  })

  it('renders risk unknown values safely and stale realtime updates', async () => {
    resetApp('/risk')
    state.scenario = 'partial-service-failure'
    setTokenSnapshot(buildAuthResponse())
    render(<App />)

    expect((await screen.findAllByText(/Unknown: new breaker state/i)).length).toBeGreaterThan(0)
    expect((await screen.findAllByText(/Unknown: new market/i)).length).toBeGreaterThan(0)
    expect((await screen.findAllByText(/<script>alert\(1\)<\/script>/i)).length).toBeGreaterThan(0)
    expect(document.querySelector('script')).toBeNull()
    act(() => FakeWebSocket.instances[0]!.onmessage?.({ data: JSON.stringify({ type: 'circuit_breaker', timestamp: fixtureDate }) }))
    expect(await screen.findByText(/risk console data is read-only and may be stale/i)).toBeTruthy()
  })

  it('shows risk empty, retry, and feature-unavailable states', async () => {
    resetApp('/risk')
    state.scenario = 'empty-data'
    setTokenSnapshot(buildAuthResponse())
    render(<App />)
    expect(await screen.findByText(/no cockpit exposure/i)).toBeTruthy()
    expect(await screen.findByText(/no tripped breakers/i)).toBeTruthy()

    resetApp('/risk')
    setTokenSnapshot(buildAuthResponse())
    let calls = 0
    server.use(http.get(`${apiBaseUrl}/risk/status`, () => {
      calls += 1
      if (calls === 1) return HttpResponse.json({ error: 'risk exploded', code: 'ERR_VALIDATION' }, { status: 400 })
      return HttpResponse.json(buildRiskStatus())
    }))
    render(<App />)
    const statusHeading = await screen.findByRole('heading', { name: /risk engine status/i })
    const statusPanel = statusHeading.closest('section') as HTMLElement
    expect(await within(statusPanel).findByRole('alert')).toHaveTextContent('risk exploded')
    await userEvent.click(within(statusPanel).getByRole('button', { name: /reload/i }))
    expect(await screen.findByText('normal')).toBeTruthy()

    resetApp('/risk')
    setTokenSnapshot(buildAuthResponse())
    server.use(http.get(`${apiBaseUrl}/risk/cockpit`, () => HttpResponse.json({ error: 'risk cockpit unavailable', code: 'ERR_NOT_IMPLEMENTED' }, { status: 501 })))
    render(<App />)
    expect(await screen.findByText(/feature unavailable/i)).toBeTruthy()
  })

  it('activates the global kill switch only after reason, confirmation, and verified status', async () => {
    resetApp('/risk')
    setTokenSnapshot(buildAuthResponse())
    let postCalls = 0
    render(<App />)

    expect(await screen.findByRole('heading', { name: /global kill switch controls/i })).toBeTruthy()
    await screen.findByText((_, el) => el?.textContent === 'Inactive')
    await userEvent.click(await screen.findByRole('button', { name: /^Activate global kill switch$/i }))
    expect(postCalls).toBe(0)
    const dialog = screen.getByRole('dialog', { name: /activate global kill switch/i })
    await userEvent.type(within(dialog).getByLabelText(/reason/i), 'operator halt')
    server.use(http.post(`${apiBaseUrl}/risk/killswitch`, async ({ request }) => {
      postCalls += 1
      const body = await request.json() as Record<string, unknown>
      expect(body).toMatchObject({ active: true, reason: 'operator halt' })
      return HttpResponse.json({ active: true, reason: 'operator halt', mechanisms: ['api_toggle'], updated_at: fixtureDate })
    }))
    server.use(http.get(`${apiBaseUrl}/risk/status`, () => HttpResponse.json(buildRiskStatus({ kill_switch: { active: true, reason: 'operator halt', mechanisms: ['api_toggle'] } }))))
    await userEvent.click(within(dialog).getByRole('button', { name: /activate kill switch/i }))

    expect(await screen.findByText(/verified kill switch active/i)).toBeTruthy()
    expect(postCalls).toBe(1)
  })

  it('validates kill switch reason, blocks duplicate submits, and surfaces conflict/rate/server errors', async () => {
    resetApp('/risk')
    setTokenSnapshot(buildAuthResponse())
    render(<App />)

    await screen.findByRole('heading', { name: /global kill switch controls/i })
    await screen.findByText((_, el) => el?.textContent === 'Inactive')
    const activateButton = screen.getByRole('button', { name: /^Activate global kill switch$/i })
    await userEvent.click(activateButton)
    const dialog = screen.getByRole('dialog', { name: /activate global kill switch/i })
    await userEvent.click(within(dialog).getByRole('button', { name: /activate kill switch/i }))
    expect(await within(dialog).findByRole('alert')).toHaveTextContent(/reason is required/i)

    await userEvent.type(within(dialog).getByLabelText(/reason/i), 'operator halt')
    let resolvePost: () => void = () => { throw new Error('resolvePost was not assigned') }
    let postCalls = 0
    server.use(http.post(`${apiBaseUrl}/risk/killswitch`, async ({ request }) => {
      postCalls += 1
      expect(await request.json()).toMatchObject({ active: true, reason: 'operator halt' })
      await new Promise<void>((resolve) => { resolvePost = resolve })
      return HttpResponse.json({ active: true, reason: 'operator halt', mechanisms: ['api_toggle'], updated_at: fixtureDate })
    }))
    await userEvent.click(within(dialog).getByRole('button', { name: /activate kill switch/i }))
    await waitFor(() => expect(postCalls).toBe(1))
    expect(within(dialog).getByRole('button', { name: /working/i })).toBeDisabled()
    await userEvent.click(within(dialog).getByRole('button', { name: /working/i }))
    expect(postCalls).toBe(1)
    resolvePost()
    server.use(http.get(`${apiBaseUrl}/risk/status`, () => HttpResponse.json(buildRiskStatus({ kill_switch: { active: true, reason: 'operator halt', mechanisms: ['api_toggle'] } }))))
    expect(await screen.findByText(/verified kill switch active/i)).toBeTruthy()

    for (const { status, code, message } of [
      { status: 409, code: 'ERR_CONFLICT', message: /kill switch activation did not complete/i },
      { status: 429, code: 'ERR_RATE_LIMITED', message: /rate limited/i },
      { status: 500, code: 'ERR_INTERNAL', message: /server could not safely complete kill switch activation/i },
      { status: 401, code: 'ERR_UNAUTHORIZED', message: /unauthorized/i },
    ] as const) {
      cleanup()
      server.resetHandlers(...createP0RestHandlers({ apiBaseUrl, state }))
      resetApp('/risk')
      setTokenSnapshot(buildAuthResponse())
      server.use(
        http.post(`${apiBaseUrl}/risk/killswitch`, () => HttpResponse.json({ error: 'boom', code }, { status })),
      )
      render(<App />)
      await screen.findByRole('heading', { name: /global kill switch controls/i })
      await screen.findByText((_, el) => el?.textContent === 'Inactive')
      const retryActivateButton = screen.getByRole('button', { name: /^Activate global kill switch$/i })
      await userEvent.click(retryActivateButton)
      const errorDialog = screen.getByRole('dialog', { name: /activate global kill switch/i })
      await userEvent.type(within(errorDialog).getByLabelText(/reason/i), 'operator halt')
      await userEvent.click(within(errorDialog).getByRole('button', { name: /activate kill switch/i }))
      expect(await within(errorDialog).findByRole('alert')).toHaveTextContent(message)
    }
  }, 10_000)

  it('shows failed verification and websocket stale or disconnected kill switch behavior', async () => {
    resetApp('/risk')
    setTokenSnapshot(buildAuthResponse())
    let statusCalls = 0
    server.use(
      http.post(`${apiBaseUrl}/risk/killswitch`, () => HttpResponse.json({ active: true, reason: 'operator halt', mechanisms: ['api_toggle'], updated_at: fixtureDate })),
      http.get(`${apiBaseUrl}/risk/status`, () => {
        statusCalls += 1
        if (statusCalls === 1) return HttpResponse.json(buildRiskStatus())
        return HttpResponse.json({ error: 'verification failed', code: 'ERR_INTERNAL' }, { status: 500 })
      }),
    )
    render(<App />)
    await screen.findByRole('heading', { name: /global kill switch controls/i })
    await screen.findByText((_, el) => el?.textContent === 'Inactive')
    const verifyActivateButton = screen.getByRole('button', { name: /^Activate global kill switch$/i })
    await userEvent.click(verifyActivateButton)
    const dialog = screen.getByRole('dialog', { name: /activate global kill switch/i })
    await userEvent.type(within(dialog).getByLabelText(/reason/i), 'operator halt')
    await userEvent.click(within(dialog).getByRole('button', { name: /activate kill switch/i }))
    expect(await within(dialog).findByRole('alert')).toHaveTextContent(/risk status verification failed/i)

    cleanup()
    server.resetHandlers(...createP0RestHandlers({ apiBaseUrl, state }))
    resetApp('/risk')
    setTokenSnapshot(buildAuthResponse())
    render(<App />)
    await screen.findByRole('heading', { name: /global kill switch controls/i })
    const staleActivateButton = await screen.findByRole('button', { name: /^Activate global kill switch$/i })
    act(() => FakeWebSocket.instances[0]!.close())
    expect(await screen.findByText(/risk console data is read-only and may be stale/i)).toBeTruthy()
    expect(staleActivateButton).toBeDisabled()

    cleanup()
    resetApp('/risk')
    setTokenSnapshot(buildAuthResponse())
    render(<App />)
    await screen.findByRole('heading', { name: /global kill switch controls/i })
    const staleEventActivateButton = await screen.findByRole('button', { name: /^Activate global kill switch$/i })
    act(() => {
      FakeWebSocket.instances[0]!.emit('circuit_breaker', { scope: 'global' })
    })
    expect(await screen.findByText(/risk console data is read-only and may be stale/i)).toBeTruthy()
    expect(staleEventActivateButton).toBeDisabled()
  })

  it('requires admin key for global kill switch deactivation and verifies cleared status', async () => {
    resetApp('/risk')
    setTokenSnapshot(buildAuthResponse())
    let active = true
    let adminHeader: string | null = null
    server.use(
      http.get(`${apiBaseUrl}/risk/status`, () => HttpResponse.json(buildRiskStatus({ kill_switch: { active, reason: 'operator halt', mechanisms: active ? ['api_toggle'] : undefined } }))),
      http.post(`${apiBaseUrl}/risk/killswitch`, async ({ request }) => {
        adminHeader = request.headers.get('x-admin-key')
        const body = await request.json() as Record<string, unknown>
        if (adminHeader !== 'test-admin-key') return HttpResponse.json({ error: 'admin key required', code: 'ERR_UNAUTHORIZED' }, { status: 401 })
        expect(body).toMatchObject({ active: false, reason: 'cleared after review' })
        active = false
        return HttpResponse.json({ active: false, updated_at: fixtureDate })
      }),
    )
    render(<App />)

    const deactivateButton = await screen.findByRole('button', { name: /deactivate global kill switch/i })
    expect(deactivateButton).not.toBeDisabled()
    await userEvent.click(deactivateButton)
    const dialog = screen.getByRole('dialog', { name: /deactivate global kill switch/i })
    await userEvent.type(within(dialog).getByLabelText(/reason/i), 'cleared after review')
    await userEvent.click(within(dialog).getByRole('button', { name: /deactivate kill switch/i }))
    expect(await within(dialog).findByLabelText(/admin key/i)).toBeTruthy()
    expect(adminHeader).toBe(null)

    await userEvent.type(within(dialog).getByLabelText(/admin key/i), 'test-admin-key')
    await userEvent.click(within(dialog).getByRole('button', { name: /deactivate kill switch/i }))
    expect(await screen.findByText(/verified kill switch inactive/i)).toBeTruthy()
    expect(adminHeader).toBe('test-admin-key')
  })

  it('keeps kill switch dialog open when verified status does not match requested state', async () => {
    resetApp('/risk')
    setTokenSnapshot(buildAuthResponse())
    server.use(
      http.post(`${apiBaseUrl}/risk/killswitch`, () => HttpResponse.json({ active: false, updated_at: fixtureDate })),
      http.get(`${apiBaseUrl}/risk/status`, () => HttpResponse.json(buildRiskStatus({ kill_switch: { active: true, reason: 'file flag still active', mechanisms: ['file_flag'] } }))),
    )
    render(<App />)
    await screen.findByRole('heading', { name: /global kill switch controls/i })
    await userEvent.click(await screen.findByRole('button', { name: /deactivate global kill switch/i }))
    const dialog = screen.getByRole('dialog', { name: /deactivate global kill switch/i })
    await userEvent.type(within(dialog).getByLabelText(/reason/i), 'cleared but file flag remains')
    await userEvent.type(within(dialog).getByLabelText(/admin key/i), 'test-admin-key')
    await userEvent.click(within(dialog).getByRole('button', { name: /deactivate kill switch/i }))
    expect(await within(dialog).findByText(/verified status is still active/i)).toBeTruthy()
  })

  it('stops and resumes a market only after confirmation and verified risk status', async () => {
    resetApp('/risk?market_type=stock')
    setTokenSnapshot(buildAuthResponse())
    render(<App />)

    expect(await screen.findByRole('heading', { name: /per-market stop and resume/i })).toBeTruthy()
    expect(screen.getByRole('combobox', { name: /market filter/i })).toHaveValue('stock')
    await screen.findByRole('button', { name: /stop stock market/i })
    expect(screen.queryByRole('button', { name: /stop crypto market/i })).toBeNull()

    await userEvent.click(screen.getByRole('button', { name: /stop stock market/i }))
    const stopDialog = screen.getByRole('dialog', { name: /stop stock market/i })
    await userEvent.click(within(stopDialog).getByRole('button', { name: /stop market/i }))
    expect(await within(stopDialog).findByRole('alert')).toHaveTextContent(/reason is required/i)
    await userEvent.type(within(stopDialog).getByLabelText(/reason/i), 'market venue outage')
    await userEvent.click(within(stopDialog).getByRole('button', { name: /stop market/i }))
    expect(await screen.findByText(/verified stock market stopped/i)).toBeTruthy()

    await userEvent.click(await screen.findByRole('button', { name: /resume stock market/i }))
    const resumeDialog = screen.getByRole('dialog', { name: /resume stock market/i })
    expect(within(resumeDialog).queryByLabelText(/reason/i)).toBeNull()
    expect(within(resumeDialog).getByText(/backend rbac and resume reason support are unresolved/i)).toBeTruthy()
    await userEvent.click(within(resumeDialog).getByRole('button', { name: /resume market/i }))
    expect(await screen.findByText(/verified stock market open/i)).toBeTruthy()
  })

  it('blocks duplicate market stop submits and surfaces market control errors', async () => {
    resetApp('/risk')
    setTokenSnapshot(buildAuthResponse())
    render(<App />)

    await screen.findByRole('heading', { name: /per-market stop and resume/i })
    await userEvent.click(await screen.findByRole('button', { name: /stop stock market/i }))
    const dialog = screen.getByRole('dialog', { name: /stop stock market/i })
    await userEvent.type(within(dialog).getByLabelText(/reason/i), 'market halt')
    let resolvePost: () => void = () => { throw new Error('resolvePost was not assigned') }
    let postCalls = 0
    server.use(http.post(`${apiBaseUrl}/risk/market/stock/stop`, async ({ request }) => {
      postCalls += 1
      expect(await request.json()).toMatchObject({ reason: 'market halt' })
      await new Promise<void>((resolve) => { resolvePost = resolve })
      return HttpResponse.json({ market_type: 'stock', active: true })
    }))
    await userEvent.click(within(dialog).getByRole('button', { name: /stop market/i }))
    await waitFor(() => expect(postCalls).toBe(1))
    expect(within(dialog).getByRole('button', { name: /working/i })).toBeDisabled()
    await userEvent.click(within(dialog).getByRole('button', { name: /working/i }))
    expect(postCalls).toBe(1)
    resolvePost()
    server.use(http.get(`${apiBaseUrl}/risk/status`, () => HttpResponse.json(buildRiskStatus({ market_kill_switches: { stock: { active: true, reason: 'market halt' } } }))))
    expect(await screen.findByText(/verified stock market stopped/i)).toBeTruthy()

    for (const { status, code, message } of [
      { status: 400, code: 'ERR_VALIDATION', message: /server rejected the market stop reason/i },
      { status: 409, code: 'ERR_CONFLICT', message: /market stop did not complete/i },
      { status: 429, code: 'ERR_RATE_LIMITED', message: /rate limited/i },
      { status: 500, code: 'ERR_INTERNAL', message: /server could not safely complete market stop/i },
      { status: 501, code: 'ERR_NOT_IMPLEMENTED', message: /not configured/i },
      { status: 401, code: 'ERR_UNAUTHORIZED', message: /unauthorized/i },
    ] as const) {
      cleanup()
      server.resetHandlers(...createP0RestHandlers({ apiBaseUrl, state }))
      resetApp('/risk')
      setTokenSnapshot(buildAuthResponse())
      server.use(http.post(`${apiBaseUrl}/risk/market/stock/stop`, () => HttpResponse.json({ error: 'boom', code }, { status })))
      render(<App />)
      await screen.findByRole('heading', { name: /per-market stop and resume/i })
      await userEvent.click(await screen.findByRole('button', { name: /stop stock market/i }))
      const errorDialog = screen.getByRole('dialog', { name: /stop stock market/i })
      await userEvent.type(within(errorDialog).getByLabelText(/reason/i), 'market halt')
      await userEvent.click(within(errorDialog).getByRole('button', { name: /stop market/i }))
      expect(await within(errorDialog).findByRole('alert')).toHaveTextContent(message)
    }
  }, 10_000)

  it('keeps market dialog open on failed verification and disables market controls when realtime is stale', async () => {
    resetApp('/risk')
    setTokenSnapshot(buildAuthResponse())
    server.use(
      http.post(`${apiBaseUrl}/risk/market/stock/stop`, () => HttpResponse.json({ market_type: 'stock', active: true })),
      http.get(`${apiBaseUrl}/risk/status`, () => HttpResponse.json(buildRiskStatus({ market_kill_switches: { stock: { active: false } } }))),
    )
    render(<App />)
    await screen.findByRole('heading', { name: /per-market stop and resume/i })
    await userEvent.click(await screen.findByRole('button', { name: /stop stock market/i }))
    const dialog = screen.getByRole('dialog', { name: /stop stock market/i })
    await userEvent.type(within(dialog).getByLabelText(/reason/i), 'market halt')
    await userEvent.click(within(dialog).getByRole('button', { name: /stop market/i }))
    expect(await within(dialog).findByText(/verified stock status is still open/i)).toBeTruthy()

    cleanup()
    server.resetHandlers(...createP0RestHandlers({ apiBaseUrl, state }))
    resetApp('/risk')
    setTokenSnapshot(buildAuthResponse())
    render(<App />)
    await screen.findByRole('heading', { name: /per-market stop and resume/i })
    const stopButton = await screen.findByRole('button', { name: /stop stock market/i })
    act(() => FakeWebSocket.instances[0]!.close())
    expect(await screen.findByText(/risk console data is read-only and may be stale/i)).toBeTruthy()
    expect(stopButton).toBeDisabled()
  })

  it('resets a circuit breaker only with one-shot admin key and verified breaker refetch', async () => {
    resetApp('/risk?tab=breakers')
    setTokenSnapshot(buildAuthResponse())
    let adminHeader: string | null = null
    let postCalls = 0
    let resetDone = false
    server.use(http.post(`${apiBaseUrl}/risk/breaker/reset`, async ({ request }) => {
      postCalls += 1
      adminHeader = request.headers.get('x-admin-key')
      expect(await request.json()).toMatchObject({ scope: 'global' })
      if (adminHeader !== 'test-admin-key') return HttpResponse.json({ error: 'admin key required', code: 'ERR_UNAUTHORIZED' }, { status: 401 })
      resetDone = true
      return HttpResponse.json({ scope: 'global', reset: true })
    }))
    server.use(http.get(`${apiBaseUrl}/risk/breakers`, () => HttpResponse.json(resetDone ? buildRiskBreakers({ tripped: [] }) : buildRiskBreakers())))
    render(<App />)

    expect(await screen.findByRole('heading', { name: /tripped breakers/i })).toBeTruthy()
    await userEvent.click(await screen.findByRole('button', { name: /reset global breaker/i }))
    const dialog = screen.getByRole('dialog', { name: /reset global breaker/i })
    await userEvent.click(within(dialog).getByRole('button', { name: /reset breaker/i }))
    expect(await within(dialog).findByRole('alert')).toHaveTextContent(/admin key is required/i)
    expect(postCalls).toBe(0)

    await userEvent.type(within(dialog).getByLabelText(/admin key/i), 'test-admin-key')
    await userEvent.click(within(dialog).getByRole('button', { name: /reset breaker/i }))
    expect(await screen.findByText(/verified breaker global reset/i)).toBeTruthy()
    expect(postCalls).toBe(1)
    expect(adminHeader).toBe('test-admin-key')
    expect(await screen.findByText(/no persisted risk breakers are currently tripped/i)).toBeTruthy()
  })

  it('clears breaker admin key, blocks duplicate reset submits, and surfaces reset errors', async () => {
    resetApp('/risk')
    setTokenSnapshot(buildAuthResponse())
    render(<App />)

    await screen.findByRole('heading', { name: /tripped breakers/i })
    await userEvent.click(await screen.findByRole('button', { name: /reset global breaker/i }))
    const dialog = screen.getByRole('dialog', { name: /reset global breaker/i })
    await userEvent.type(within(dialog).getByLabelText(/admin key/i), 'wrong-key')
    await userEvent.click(within(dialog).getByRole('button', { name: /reset breaker/i }))
    expect(await within(dialog).findByRole('alert')).toHaveTextContent(/admin key was rejected/i)
    expect(within(dialog).getByLabelText(/admin key/i)).toHaveValue('')

    await userEvent.type(within(dialog).getByLabelText(/admin key/i), 'test-admin-key')
    let resolvePost: () => void = () => { throw new Error('resolvePost was not assigned') }
    let postCalls = 0
    let resetDone = false
    server.use(http.post(`${apiBaseUrl}/risk/breaker/reset`, async () => {
      postCalls += 1
      await new Promise<void>((resolve) => { resolvePost = resolve })
      resetDone = true
      return HttpResponse.json({ scope: 'global', reset: true })
    }))
    server.use(http.get(`${apiBaseUrl}/risk/breakers`, () => HttpResponse.json(resetDone ? buildRiskBreakers({ tripped: [] }) : buildRiskBreakers())))
    await userEvent.click(within(dialog).getByRole('button', { name: /reset breaker/i }))
    await waitFor(() => expect(postCalls).toBe(1))
    expect(within(dialog).getByRole('button', { name: /working/i })).toBeDisabled()
    await userEvent.click(within(dialog).getByRole('button', { name: /working/i }))
    expect(postCalls).toBe(1)
    resolvePost()
    expect(await screen.findByText(/verified breaker global reset/i)).toBeTruthy()

    for (const { status, code, message } of [
      { status: 400, code: 'ERR_VALIDATION', message: /rejected the breaker reset scope/i },
      { status: 429, code: 'ERR_RATE_LIMITED', message: /rate limited/i },
      { status: 500, code: 'ERR_INTERNAL', message: /server could not safely complete breaker reset/i },
      { status: 501, code: 'ERR_NOT_IMPLEMENTED', message: /not configured/i },
      { status: 401, code: 'ERR_UNAUTHORIZED', message: /admin key was rejected/i },
    ] as const) {
      cleanup()
      server.resetHandlers(...createP0RestHandlers({ apiBaseUrl, state }))
      resetApp('/risk')
      setTokenSnapshot(buildAuthResponse())
      server.use(http.post(`${apiBaseUrl}/risk/breaker/reset`, () => HttpResponse.json({ error: 'boom', code }, { status })))
      render(<App />)
      await screen.findByRole('heading', { name: /tripped breakers/i })
      await userEvent.click(await screen.findByRole('button', { name: /reset global breaker/i }))
      const errorDialog = screen.getByRole('dialog', { name: /reset global breaker/i })
      await userEvent.type(within(errorDialog).getByLabelText(/admin key/i), 'test-admin-key')
      await userEvent.click(within(errorDialog).getByRole('button', { name: /reset breaker/i }))
      expect(await within(errorDialog).findByRole('alert')).toHaveTextContent(message)
      expect(within(errorDialog).getByLabelText(/admin key/i)).toHaveValue('')
    }
  }, 20_000)

  it('keeps breaker reset dialog open on failed verification and disables reset when realtime is stale', async () => {
    resetApp('/risk')
    setTokenSnapshot(buildAuthResponse())
    server.use(
      http.post(`${apiBaseUrl}/risk/breaker/reset`, () => HttpResponse.json({ scope: 'global', reset: true })),
      http.get(`${apiBaseUrl}/risk/breakers`, () => HttpResponse.json(buildRiskBreakers())),
    )
    render(<App />)
    await screen.findByRole('heading', { name: /tripped breakers/i })
    await userEvent.click(await screen.findByRole('button', { name: /reset global breaker/i }))
    const dialog = screen.getByRole('dialog', { name: /reset global breaker/i })
    await userEvent.type(within(dialog).getByLabelText(/admin key/i), 'test-admin-key')
    await userEvent.click(within(dialog).getByRole('button', { name: /reset breaker/i }))
    expect(await within(dialog).findByText(/verified scope global is still tripped/i)).toBeTruthy()

    cleanup()
    server.resetHandlers(...createP0RestHandlers({ apiBaseUrl, state }))
    resetApp('/risk')
    setTokenSnapshot(buildAuthResponse())
    render(<App />)
    await screen.findByRole('heading', { name: /tripped breakers/i })
    const resetButton = await screen.findByRole('button', { name: /reset global breaker/i })
    act(() => FakeWebSocket.instances[0]!.close())
    expect(await screen.findByText(/risk console data is read-only and may be stale/i)).toBeTruthy()
    expect(resetButton).toBeDisabled()
  })

})

