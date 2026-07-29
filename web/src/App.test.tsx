import '@testing-library/jest-dom/vitest'
import { act, cleanup, render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { delay, http, HttpResponse } from 'msw'
import { describe, expect, it } from 'vitest'

import App from '@/App'
import { setTokenSnapshot } from '@/shared/auth/tokenStore'
import { buildAuthResponse, buildOrder, buildPosition, buildRiskStatus, buildRun, buildStrategy, fixtureDate } from '@/test/fixtures'
import { apiBaseUrl, FakeWebSocket, installAppTestHarness, resetApp, server, state, strategyId } from '@/test/app-harness'

describe('first vertical slice app', () => {
  installAppTestHarness()

  it('restores automation job list controls and detail links', async () => {
    resetApp('/automation')
    setTokenSnapshot(buildAuthResponse())
    let runCalls = 0
    server.use(
      http.post(`${apiBaseUrl}/automation/jobs/:name/run`, () => {
        runCalls += 1
        return HttpResponse.json({ status: 'triggered' })
      }),
    )
    render(<App />)

    expect(
      await screen.findByRole('heading', { name: /^automations$/i }, { timeout: 10_000 }),
    ).toBeTruthy()
    const deepScanLink = await screen.findByRole('link', { name: 'deep_scan' })
    expect(deepScanLink).toHaveAttribute('href', '/automation/deep_scan')
    const row = deepScanLink.closest('tr') as HTMLElement
    expect(within(row).getByText(/deep strategy scan/i)).toBeTruthy()
    expect(within(row).getByText(/healthy/i)).toBeTruthy()
    await userEvent.click(within(row).getByRole('button', { name: /run now/i }))
    await waitFor(() => expect(runCalls).toBe(1))
  })

  it('shows automation job details and recent run history', async () => {
    resetApp('/automation/deep_scan')
    setTokenSnapshot(buildAuthResponse())
    render(<App />)

    expect(await screen.findByRole('heading', { name: /^deep_scan$/i })).toBeTruthy()
    expect(await screen.findByText(/deep strategy scan/i)).toBeTruthy()
    expect(screen.getByText(/Every hour/i)).toBeTruthy()
    expect(screen.getByText(/"scanned": 12/i)).toBeTruthy()
    expect(await screen.findByRole('table', { name: /automation run history/i })).toBeTruthy()
    expect(screen.getAllByText('completed').length).toBeGreaterThan(0)
  })

  it('renders unsafe-looking event text as escaped content', async () => {
    resetApp('/cockpit')
    setTokenSnapshot(buildAuthResponse())
    render(<App />)
    expect(await screen.findByRole('heading', { name: /system overview/i })).toBeTruthy()
    await waitFor(() => expect(FakeWebSocket.instances[0]?.sent.some((item) => item.includes('subscribe_all'))).toBe(true))

    act(() => FakeWebSocket.instances[0]!.emit('<img src=x onerror=alert(1)>', { html: '<script>alert(1)</script>' }))

    expect((await screen.findAllByText('<img src=x onerror=alert(1)>')).length).toBeGreaterThan(0)
    expect(document.querySelector('img[src="x"]')).toBeNull()
    expect(document.querySelector('script')).toBeNull()
  })

  it('displays unknown status values without crashing', async () => {
    resetApp('/cockpit')
    setTokenSnapshot(buildAuthResponse())
    server.use(
      http.get(`${apiBaseUrl}/risk/status`, () => HttpResponse.json(buildRiskStatus({ risk_status: 'mystery_status' }))),
    )
    render(<App />)

    expect(await screen.findByText('mystery_status')).toBeTruthy()
  })

  it('lists strategies with paper/live clarity and deep links to detail', async () => {
    cleanup()
    server.resetHandlers()
    resetApp('/strategies')
    setTokenSnapshot(buildAuthResponse())
    render(<App />)

    expect(await screen.findByRole('heading', { name: /^strategies$/i })).toBeTruthy()
    expect((await screen.findAllByRole('link', { name: /dev paper mean reversion/i }))[0]).toHaveAttribute('href', '/strategies/00000000-0000-4000-8000-000000000010')
    expect(screen.getAllByText('PAPER').length).toBeGreaterThan(0)
    expect(screen.getAllByText('LIVE').length).toBeGreaterThan(0)
    expect(screen.getByRole('table')).toBeTruthy()
  })

  it('keeps strategy filters in the URL and filters rows', async () => {
    resetApp('/strategies')
    setTokenSnapshot(buildAuthResponse())
    render(<App />)

    expect((await screen.findAllByRole('link', { name: /dev paper mean reversion/i })).length).toBeGreaterThan(0)
    await userEvent.selectOptions(screen.getByLabelText(/mode/i), 'false')
    expect(window.location.search).toContain('is_paper=false')
    expect((await screen.findAllByRole('link', { name: /dev live breakout/i })).length).toBeGreaterThan(0)
    expect(screen.queryAllByRole('link', { name: /dev paper mean reversion/i })).toHaveLength(0)
  })

  it('shows empty strategy list state', async () => {
    resetApp('/strategies')
    state.scenario = 'empty-data'
    setTokenSnapshot(buildAuthResponse())
    render(<App />)

    expect(await screen.findByText('No strategies found')).toBeTruthy()
    expect(screen.getByText(/create a paper strategy/i)).toBeTruthy()
  })

  it('shows strategy list error, retry, and 501 states', async () => {
    resetApp('/strategies')
    setTokenSnapshot(buildAuthResponse())
    let calls = 0
    server.use(
      http.get(`${apiBaseUrl}/strategies`, () => {
        calls += 1
        if (calls === 1) return HttpResponse.json({ error: 'strategy list exploded', code: 'ERR_VALIDATION' }, { status: 400 })
        return HttpResponse.json({ data: [buildStrategy()], total: 1, limit: 20, offset: 0 })
      }),
    )
    render(<App />)

    expect(await screen.findByRole('alert')).toHaveTextContent('strategy list exploded')
    await userEvent.click(screen.getByRole('button', { name: /reload/i }))
    expect((await screen.findAllByRole('link', { name: /dev paper mean reversion/i })).length).toBeGreaterThan(0)

    resetApp('/strategies')
    setTokenSnapshot(buildAuthResponse())
    server.use(http.get(`${apiBaseUrl}/strategies`, () => HttpResponse.json({ error: 'not configured', code: 'ERR_NOT_IMPLEMENTED' }, { status: 501 })))
    render(<App />)
    expect(await screen.findByText(/feature unavailable/i)).toBeTruthy()
  })

  it('shows loading, stale realtime, and unknown strategy enum states', async () => {
    resetApp('/strategies')
    setTokenSnapshot(buildAuthResponse())
    state.scenario = 'partial-service-failure'
    server.use(
      http.get(`${apiBaseUrl}/strategies`, async ({ request }) => {
        await delay(100)
        const url = new URL(request.url)
        return HttpResponse.json({ data: [buildStrategy({ status: 'new_backend_status', market_type: url.searchParams.get('market_type') || 'new_market' })], total: 1, limit: 20, offset: 0 })
      }),
    )
    render(<App />)

    expect(await screen.findByText(/loading strategies/i)).toBeTruthy()
    expect((await screen.findAllByText(/Unknown: new_backend_status/i)).length).toBeGreaterThan(0)
    act(() => FakeWebSocket.instances[0]!.close())
    expect(await screen.findByText(/rows are read-only and may lag/i)).toBeTruthy()
  })

  it('lists runs with run and strategy deep links', async () => {
    resetApp('/runs')
    setTokenSnapshot(buildAuthResponse())
    render(<App />)

    expect(await screen.findByRole('heading', { name: /^runs$/i })).toBeTruthy()
    expect(await screen.findByRole('table')).toBeTruthy()
    expect(screen.getAllByRole('link', { name: /00000000-0000-4000-8000-000000000020/i })[0]).toHaveAttribute('href', '/runs/00000000-0000-4000-8000-000000000020')
    expect(screen.getAllByRole('link', { name: /strategy/i })[0]).toHaveAttribute('href', '/strategies/00000000-0000-4000-8000-000000000010')
    expect(screen.getAllByText(/running|completed|failed|cancelled/i).length).toBeGreaterThan(0)
  })

  it('keeps run filters in the URL and filters rows', async () => {
    resetApp('/runs')
    setTokenSnapshot(buildAuthResponse())
    render(<App />)

    expect(await screen.findByRole('heading', { name: /^runs$/i })).toBeTruthy()
    await userEvent.selectOptions(screen.getByLabelText(/^status/i), 'completed')
    await userEvent.type(screen.getByLabelText(/^ticker/i), 'live')
    expect(window.location.search).toContain('status=completed')
    expect(window.location.search).toContain('ticker=LIVE')
    expect((await screen.findAllByText('LIVE')).length).toBeGreaterThan(0)
    expect(screen.queryByText('AUGR')).toBeNull()
  })

  it('shows run empty, retry, and 501 states', async () => {
    resetApp('/runs')
    state.scenario = 'empty-data'
    setTokenSnapshot(buildAuthResponse())
    render(<App />)
    expect(await screen.findByText(/no runs found/i)).toBeTruthy()

    resetApp('/runs')
    setTokenSnapshot(buildAuthResponse())
    let calls = 0
    server.use(http.get(`${apiBaseUrl}/runs`, () => {
      calls += 1
      if (calls === 1) return HttpResponse.json({ error: 'run list exploded', code: 'ERR_VALIDATION' }, { status: 400 })
      return HttpResponse.json({ data: [buildRun()], total: 1, limit: 20, offset: 0 })
    }))
    render(<App />)
    expect(await screen.findByRole('alert')).toHaveTextContent('run list exploded')
    await userEvent.click(screen.getByRole('button', { name: /reload/i }))
    expect(await screen.findByRole('table')).toBeTruthy()

    resetApp('/runs')
    setTokenSnapshot(buildAuthResponse())
    server.use(http.get(`${apiBaseUrl}/runs`, () => HttpResponse.json({ error: 'runs not configured', code: 'ERR_NOT_IMPLEMENTED' }, { status: 501 })))
    render(<App />)
    expect(await screen.findByText(/feature unavailable/i)).toBeTruthy()
  })

  it('handles missing run totals, unknown status, and realtime stale events', async () => {
    resetApp('/runs')
    state.scenario = 'partial-service-failure'
    setTokenSnapshot(buildAuthResponse())
    render(<App />)

    expect((await screen.findAllByText(/total unavailable/i)).length).toBeGreaterThan(0)
    expect((await screen.findAllByText(/Unknown: new_run_status/i)).length).toBeGreaterThan(0)
    await userEvent.click(screen.getByRole('button', { name: /next/i }))
    expect(window.location.search).toContain('offset=20')
    act(() => {
      FakeWebSocket.instances[0]!.onmessage?.({ data: JSON.stringify({ type: 'pipeline_start', timestamp: fixtureDate }) })
    })
    expect(await screen.findByText(/run rows are read-only and may be stale/i)).toBeTruthy()
  })

  it('renders run detail overview with strategy link and JSON evidence', async () => {
    resetApp('/runs/00000000-0000-4000-8000-000000000020')
    setTokenSnapshot(buildAuthResponse())
    render(<App />)

    expect(await screen.findByRole('heading', { name: /augr run/i })).toBeTruthy()
    expect(screen.getAllByRole('link', { name: /open strategy/i })[0]).toHaveAttribute('href', '/strategies/00000000-0000-4000-8000-000000000010')
    expect(screen.getByText(/Config snapshot/i)).toBeTruthy()
    expect(screen.getByText(/Phase timings/i)).toBeTruthy()
    expect(screen.getByText(/"mode": "paper"/i)).toBeTruthy()
  })

  it('renders failed run errors and not-found or unavailable states', async () => {
    resetApp('/runs/00000000-0000-4000-8000-000000000022')
    setTokenSnapshot(buildAuthResponse())
    render(<App />)
    expect(await screen.findByText('fixture run failed')).toBeTruthy()
    expect(screen.getAllByText('failed').length).toBeGreaterThan(0)

    resetApp('/runs/00000000-0000-4000-8000-000000000999')
    setTokenSnapshot(buildAuthResponse())
    render(<App />)
    expect(await screen.findByText(/run not found/i)).toBeTruthy()

    resetApp('/runs/00000000-0000-4000-8000-000000000020')
    setTokenSnapshot(buildAuthResponse())
    server.use(http.get(`${apiBaseUrl}/runs/:id`, () => HttpResponse.json({ error: 'run detail not configured', code: 'ERR_NOT_IMPLEMENTED' }, { status: 501 })))
    render(<App />)
    expect(await screen.findByText(/feature unavailable/i)).toBeTruthy()
  })

  it('renders unknown run detail values safely and marks matching realtime stale', async () => {
    resetApp('/runs/00000000-0000-4000-8000-000000000020')
    state.scenario = 'partial-service-failure'
    setTokenSnapshot(buildAuthResponse())
    render(<App />)

    expect((await screen.findAllByText(/Unknown: new_run_status/i)).length).toBeGreaterThan(0)
    expect((await screen.findAllByText(/<script>alert\(1\)<\/script>/i)).length).toBeGreaterThan(0)
    expect(document.querySelector('script')).toBeNull()
    act(() => {
      FakeWebSocket.instances[0]!.onmessage?.({ data: JSON.stringify({ type: 'agent_decision', run_id: '00000000-0000-4000-8000-000000000020', timestamp: fixtureDate }) })
    })
    expect(await screen.findByText(/run detail is read-only and may be stale/i)).toBeTruthy()
  })

  it('renders run decisions with filters, prompt inclusion, and pagination URL state', async () => {
    resetApp('/runs/00000000-0000-4000-8000-000000000020?tab=decisions')
    setTokenSnapshot(buildAuthResponse())
    render(<App />)

    expect(await screen.findByRole('heading', { name: /agent decisions/i })).toBeTruthy()
    expect(await screen.findByText(/Hold until confirmation improves/i)).toBeTruthy()
    await userEvent.type(screen.getByLabelText(/agent role/i), 'risk')
    expect(window.location.search).toContain('agent_role=risk')
    expect(await screen.findByText(/Risk accepts paper-only exposure/i)).toBeTruthy()
    expect(screen.queryByText(/Hold until confirmation improves/i)).toBeNull()
    await userEvent.selectOptions(screen.getByLabelText(/prompt/i), 'true')
    expect(window.location.search).toContain('include_prompt=true')
    expect(await screen.findByText(/Risk prompt/i)).toBeTruthy()
  })

  it('shows run decision empty, retry, and 501 states', async () => {
    resetApp('/runs/00000000-0000-4000-8000-000000000020?tab=decisions')
    state.scenario = 'empty-data'
    setTokenSnapshot(buildAuthResponse())
    render(<App />)
    expect(await screen.findByText(/no decisions found/i)).toBeTruthy()

    resetApp('/runs/00000000-0000-4000-8000-000000000020?tab=decisions')
    setTokenSnapshot(buildAuthResponse())
    let calls = 0
    server.use(http.get(`${apiBaseUrl}/runs/:id/decisions`, () => {
      calls += 1
      if (calls === 1) return HttpResponse.json({ error: 'decisions exploded', code: 'ERR_VALIDATION' }, { status: 400 })
      return HttpResponse.json({ data: [{ id: '00000000-0000-4000-8000-000000000070', pipeline_run_id: '00000000-0000-4000-8000-000000000020', agent_role: 'analyst', phase: 'signal_generation', output_text: 'Recovered decision', created_at: fixtureDate }], total: 1, limit: 10, offset: 0 })
    }))
    render(<App />)
    expect(await screen.findByRole('alert')).toHaveTextContent('decisions exploded')
    await userEvent.click(screen.getByRole('button', { name: /reload/i }))
    expect(await screen.findByText(/Recovered decision/i)).toBeTruthy()

    resetApp('/runs/00000000-0000-4000-8000-000000000020?tab=decisions')
    setTokenSnapshot(buildAuthResponse())
    server.use(http.get(`${apiBaseUrl}/runs/:id/decisions`, () => HttpResponse.json({ error: 'decisions not configured', code: 'ERR_NOT_IMPLEMENTED' }, { status: 501 })))
    render(<App />)
    expect(await screen.findByText(/feature unavailable/i)).toBeTruthy()
  })

  it('renders unknown decision values safely and marks matching decision events stale', async () => {
    resetApp('/runs/00000000-0000-4000-8000-000000000020?tab=decisions&include_prompt=true')
    state.scenario = 'partial-service-failure'
    setTokenSnapshot(buildAuthResponse())
    render(<App />)

    expect(await screen.findByText(/new agent role/i)).toBeTruthy()
    expect((await screen.findAllByText(/<script>alert\(1\)<\/script>/i)).length).toBeGreaterThan(0)
    expect(document.querySelector('script')).toBeNull()
    act(() => {
      FakeWebSocket.instances[0]!.onmessage?.({ data: JSON.stringify({ type: 'agent_decision', run_id: '00000000-0000-4000-8000-000000000020', timestamp: fixtureDate }) })
    })
    expect(await screen.findByText(/Realtime decision activity was received/i)).toBeTruthy()
  })

  it('renders run snapshot payloads with obvious secret redaction', async () => {
    resetApp('/runs/00000000-0000-4000-8000-000000000020?tab=snapshot')
    setTokenSnapshot(buildAuthResponse())
    render(<App />)

    expect(await screen.findByRole('heading', { name: /run snapshot/i })).toBeTruthy()
    expect(await screen.findByText(/market_state JSON/i)).toBeTruthy()
    expect(screen.getByText(/"ticker": "AUGR"/i)).toBeTruthy()
    expect(screen.getAllByText(/\[REDACTED\]/i).length).toBeGreaterThan(0)
    expect(screen.queryByText(/fixture-secret-should-redact/i)).toBeNull()
    expect(await screen.findByText(/may be stale for running runs/i)).toBeTruthy()
  })

  it('shows run snapshot empty and feature unavailable states', async () => {
    resetApp('/runs/00000000-0000-4000-8000-000000000020?tab=snapshot')
    state.scenario = 'empty-data'
    setTokenSnapshot(buildAuthResponse())
    render(<App />)
    expect(await screen.findByText(/snapshot not recorded/i)).toBeTruthy()

    resetApp('/runs/00000000-0000-4000-8000-000000000020?tab=snapshot')
    setTokenSnapshot(buildAuthResponse())
    server.use(http.get(`${apiBaseUrl}/runs/:id/snapshot`, () => HttpResponse.json({ error: 'snapshots not configured', code: 'ERR_NOT_IMPLEMENTED' }, { status: 501 })))
    render(<App />)
    expect(await screen.findByText(/feature unavailable/i)).toBeTruthy()
  })

  it('renders large and unknown snapshot payloads safely and warns for running runs', async () => {
    resetApp('/runs/00000000-0000-4000-8000-000000000020?tab=snapshot')
    state.scenario = 'partial-service-failure'
    setTokenSnapshot(buildAuthResponse())
    render(<App />)

    expect(await screen.findByText(/huge_payload JSON/i)).toBeTruthy()
    expect(await screen.findByText(/unknown_backend_shape JSON/i)).toBeTruthy()
    expect((await screen.findAllByText(/<script>alert\(1\)<\/script>/i)).length).toBeGreaterThan(0)
    expect(screen.getAllByText(/\[REDACTED\]/i).length).toBeGreaterThan(0)
    expect(document.querySelector('script')).toBeNull()
  })

  it('renders persisted events with deep links and URL filters', async () => {
    resetApp('/events')
    setTokenSnapshot(buildAuthResponse())
    render(<App />)

    expect(await screen.findByRole('heading', { name: /^persisted events$/i })).toBeTruthy()
    expect(await screen.findByText(/Analyst decision recorded/i)).toBeTruthy()
    expect(screen.getAllByRole('link', { name: /Strategy/i })[0]).toHaveAttribute('href', '/strategies/00000000-0000-4000-8000-000000000010')
    expect(screen.getAllByRole('link', { name: /Run/i }).some((link) => link.getAttribute('href') === '/runs/00000000-0000-4000-8000-000000000020')).toBe(true)
    await userEvent.type(screen.getByLabelText(/event kind/i), 'signal')
    expect(window.location.search).toContain('event_kind=signal')
    expect(await screen.findByText(/Risk signal reviewed/i)).toBeTruthy()
  })

  it('adds copyable cross-entity links and preserves filtered source context', async () => {
    resetApp('/events?event_kind=agent_decision')
    setTokenSnapshot(buildAuthResponse())
    render(<App />)

    expect(await screen.findByText(/Analyst decision recorded/i)).toBeTruthy()
    expect(screen.getAllByRole('link', { name: /Order/i }).some((link) => link.getAttribute('href') === '/orders/00000000-0000-4000-8000-000000000040?from=%2Fevents%3Fevent_kind%3Dagent_decision')).toBe(true)
    expect(screen.getAllByRole('button', { name: /copy event id/i }).length).toBeGreaterThan(0)
    expect(screen.getByRole('link', { name: /Strategy/i })).toHaveAttribute('href', '/strategies/00000000-0000-4000-8000-000000000010?from=%2Fevents%3Fevent_kind%3Dagent_decision')
  })

  it('links realtime drawer activity to strategy and run when ids exist', async () => {
    resetApp('/cockpit')
    setTokenSnapshot(buildAuthResponse())
    render(<App />)

    expect(await screen.findByRole('heading', { name: /system overview/i })).toBeTruthy()
    await waitFor(() => expect(FakeWebSocket.instances.length).toBeGreaterThan(0))
    act(() => FakeWebSocket.instances[0]!.onmessage?.({ data: JSON.stringify({ type: 'pipeline_start', strategy_id: '00000000-0000-4000-8000-000000000010', run_id: '00000000-0000-4000-8000-000000000020', timestamp: fixtureDate }) }))

    const drawer = screen.getByRole('complementary', { name: /global realtime activity/i })
    expect(await within(drawer).findByRole('link', { name: /Strategy/i })).toHaveAttribute('href', '/strategies/00000000-0000-4000-8000-000000000010')
    expect(within(drawer).getByRole('link', { name: /Run/i })).toHaveAttribute('href', '/runs/00000000-0000-4000-8000-000000000020')
  })

  it('shows persisted event empty, retry, and 501 states', async () => {
    resetApp('/events')
    state.scenario = 'empty-data'
    setTokenSnapshot(buildAuthResponse())
    render(<App />)
    expect(await screen.findByText(/no persisted events/i)).toBeTruthy()

    resetApp('/events')
    setTokenSnapshot(buildAuthResponse())
    let calls = 0
    server.use(http.get(`${apiBaseUrl}/events`, () => {
      calls += 1
      if (calls === 1) return HttpResponse.json({ error: 'events exploded', code: 'ERR_VALIDATION' }, { status: 400 })
      return HttpResponse.json({ data: [{ id: '00000000-0000-4000-8000-000000000080', event_kind: 'signal', title: 'Recovered event', created_at: fixtureDate }], total: 1, limit: 20, offset: 0 })
    }))
    render(<App />)
    expect(await screen.findByText('events exploded')).toBeTruthy()
    await userEvent.click(screen.getByRole('button', { name: /reload/i }))
    expect(await screen.findByText(/Recovered event/i)).toBeTruthy()

    resetApp('/events')
    setTokenSnapshot(buildAuthResponse())
    server.use(http.get(`${apiBaseUrl}/events`, () => HttpResponse.json({ error: 'events not configured', code: 'ERR_NOT_IMPLEMENTED' }, { status: 501 })))
    render(<App />)
    expect(await screen.findByText(/feature unavailable/i)).toBeTruthy()
  })

  it('embeds run timeline and renders unknown event metadata safely', async () => {
    resetApp('/runs/00000000-0000-4000-8000-000000000020?tab=timeline')
    state.scenario = 'partial-service-failure'
    setTokenSnapshot(buildAuthResponse())
    render(<App />)

    expect(await screen.findByRole('heading', { name: /persisted event timeline/i })).toBeTruthy()
    expect(await screen.findByText(/Unsafe <script>alert\(1\)<\/script>/i)).toBeTruthy()
    expect(await screen.findByText(/new event kind/i)).toBeTruthy()
    expect(screen.getByText(/total unavailable/i)).toBeTruthy()
    expect(document.querySelector('script')).toBeNull()
  })

  it('renders portfolio summary and open positions with strategy links', async () => {
    resetApp('/portfolio')
    setTokenSnapshot(buildAuthResponse())
    render(<App />)

    expect(await screen.findByRole('heading', { name: /^portfolio$/i })).toBeTruthy()
    expect(await screen.findByRole('table', { name: /open positions/i })).toBeTruthy()
    expect(screen.getAllByText(/Unrealized P\/L/i).length).toBeGreaterThan(0)
    expect(screen.getAllByRole('link', { name: /Strategy/i })[0]).toHaveAttribute('href', '/strategies/00000000-0000-4000-8000-000000000010')
  })

  it('renders persisted option contract and Greek position metadata', async () => {
    resetApp('/portfolio')
    setTokenSnapshot(buildAuthResponse())
    server.use(http.get(`${apiBaseUrl}/portfolio/positions/open`, () => HttpResponse.json({ data: [buildPosition({ market_type: 'options', ticker: 'AAPL271217C00150000', asset_class: 'option', underlying_ticker: 'AAPL', option_type: 'call', strike: 150, expiry: '2027-12-17T00:00:00Z', contract_multiplier: 100, delta: 0.4 })], total: 1, limit: 20, offset: 0 })))
    render(<App />)

    expect((await screen.findAllByText('AAPL271217C00150000')).length).toBeGreaterThan(0)
    expect(screen.getAllByText(/AAPL · call \$150.00 · .* · 100×/i).length).toBeGreaterThan(0)
  })

  it('keeps portfolio filters in URL and handles unknown sides and missing totals', async () => {
    resetApp('/portfolio')
    state.scenario = 'partial-service-failure'
    setTokenSnapshot(buildAuthResponse())
    render(<App />)

    expect((await screen.findAllByText(/total unavailable/i)).length).toBeGreaterThan(0)
    expect((await screen.findAllByText(/Unknown: mystery_side/i)).length).toBeGreaterThan(0)
    await userEvent.type(screen.getByLabelText(/^ticker/i), 'live')
    expect(window.location.search).toContain('ticker=LIVE')
    expect((await screen.findAllByText('LIVE')).length).toBeGreaterThan(0)
  })

  it('shows portfolio empty, retry, and realtime stale states', async () => {
    resetApp('/portfolio')
    state.scenario = 'empty-data'
    setTokenSnapshot(buildAuthResponse())
    render(<App />)
    expect((await screen.findAllByText(/no open positions/i)).length).toBeGreaterThan(0)

    resetApp('/portfolio')
    setTokenSnapshot(buildAuthResponse())
    let calls = 0
    server.use(http.get(`${apiBaseUrl}/portfolio/positions/open`, () => {
      calls += 1
      if (calls === 1) return HttpResponse.json({ error: 'positions exploded', code: 'ERR_VALIDATION' }, { status: 400 })
      return HttpResponse.json({ data: [{ id: '00000000-0000-4000-8000-000000000030', ticker: 'AUGR', side: 'long', quantity: 1, avg_entry: 100, realized_pnl: 0, opened_at: fixtureDate }], total: 1, limit: 20, offset: 0 })
    }))
    render(<App />)
    expect(await screen.findByRole('alert')).toHaveTextContent('positions exploded')
    await userEvent.click(screen.getByRole('button', { name: /reload/i }))
    expect(await screen.findByRole('table', { name: /open positions/i })).toBeTruthy()

    act(() => FakeWebSocket.instances[0]!.onmessage?.({ data: JSON.stringify({ type: 'position_update', timestamp: fixtureDate }) }))
    expect(await screen.findByText(/portfolio data may be stale/i)).toBeTruthy()
  })

  it('renders allocator diagnostics with opportunities and decision tables', async () => {
    resetApp('/portfolio?tab=allocator')
    setTokenSnapshot(buildAuthResponse())
    render(<App />)

    expect(await screen.findByRole('heading', { name: /allocator diagnostics/i })).toBeTruthy()
    expect(await screen.findByRole('table', { name: /allocator opportunities/i })).toBeTruthy()
    expect(await screen.findByRole('table', { name: /allocator decisions/i })).toBeTruthy()
    expect(screen.getByText(/account_balance_unavailable/i)).toBeTruthy()
    expect(screen.getAllByRole('link', { name: /Strategy/i })[0]).toHaveAttribute('href', '/strategies/00000000-0000-4000-8000-000000000010?from=%2Fportfolio%3Ftab%3Dallocator')
    expect(screen.getAllByRole('link', { name: /Run/i }).some((link) => link.getAttribute('href') === '/runs/00000000-0000-4000-8000-000000000020?from=%2Fportfolio%3Ftab%3Dallocator')).toBe(true)
  })

  it('keeps allocator filters in URL and renders unknown allocator data safely', async () => {
    resetApp('/portfolio?tab=allocator')
    state.scenario = 'partial-service-failure'
    setTokenSnapshot(buildAuthResponse())
    render(<App />)

    expect((await screen.findAllByText(/total unavailable/i)).length).toBeGreaterThan(0)
    expect((await screen.findAllByText(/Unknown: new opportunity status/i)).length).toBeGreaterThan(0)
    expect((await screen.findAllByText(/Unknown: new mode/i)).length).toBeGreaterThan(0)
    expect(screen.getByText(/new_backend_warning_<script>alert\(1\)<\/script>/i)).toBeTruthy()
    expect(document.querySelector('script')).toBeNull()
    await userEvent.type(screen.getByLabelText(/^ticker/i), 'live')
    expect(window.location.search).toContain('tab=allocator')
    expect(window.location.search).toContain('ticker=LIVE')
    expect((await screen.findAllByText('LIVE')).length).toBeGreaterThan(0)
  })

  it('shows allocator empty, retry, and feature-unavailable states', async () => {
    resetApp('/portfolio?tab=allocator')
    state.scenario = 'empty-data'
    setTokenSnapshot(buildAuthResponse())
    render(<App />)
    expect((await screen.findAllByText(/no allocator opportunities/i)).length).toBeGreaterThan(0)
    expect((await screen.findAllByText(/no allocation decisions/i)).length).toBeGreaterThan(0)

    resetApp('/portfolio?tab=allocator')
    setTokenSnapshot(buildAuthResponse())
    let calls = 0
    server.use(http.get(`${apiBaseUrl}/portfolio/allocator/opportunities`, () => {
      calls += 1
      if (calls === 1) return HttpResponse.json({ error: 'allocator exploded', code: 'ERR_VALIDATION' }, { status: 400 })
      return HttpResponse.json({ data: [{ id: '00000000-0000-4000-8000-000000000090', strategy_id: strategyId, market_type: 'stock', ticker: 'AUGR', side: 'buy', signal: 'hold', status: 'queued', confidence: 0.5, edge_pct: 1, expected_return_pct: 2, max_loss_pct: 1, entry_price: 100, liquidity_usd: 1000, market_cap_usd: 10000, spread_pct: 0.1, proposed_notional: 100, selected_notional: 50, reason: 'Recovered opportunity', expires_at: fixtureDate, created_at: fixtureDate, updated_at: fixtureDate, dedupe_key: 'recovered' }], total: 1, limit: 10, offset: 0 })
    }))
    render(<App />)
    expect(await screen.findByRole('alert')).toHaveTextContent('allocator exploded')
    await userEvent.click(screen.getByRole('button', { name: /reload/i }))
    expect(await screen.findByText(/Recovered opportunity/i)).toBeTruthy()

    resetApp('/portfolio?tab=allocator')
    setTokenSnapshot(buildAuthResponse())
    server.use(http.get(`${apiBaseUrl}/portfolio/allocator/diagnostics`, () => HttpResponse.json({ error: 'allocator not configured', code: 'ERR_NOT_IMPLEMENTED' }, { status: 501 })))
    render(<App />)
    expect(await screen.findByText(/feature unavailable/i)).toBeTruthy()
  })

  it('lists orders with strategy and run deep links', async () => {
    resetApp('/orders')
    setTokenSnapshot(buildAuthResponse())
    render(<App />)

    expect(await screen.findByRole('heading', { name: /^orders$/i })).toBeTruthy()
    expect(await screen.findByRole('table', { name: /^orders$/i })).toBeTruthy()
    expect(screen.getAllByRole('link', { name: /Strategy/i })[0]).toHaveAttribute('href', '/strategies/00000000-0000-4000-8000-000000000010')
    expect(screen.getAllByRole('link', { name: /Run/i }).some((link) => link.getAttribute('href') === '/runs/00000000-0000-4000-8000-000000000020')).toBe(true)
    expect(screen.getAllByText(/paper-broker|backup-broker/i).length).toBeGreaterThan(0)
  })

  it('renders persisted option intent and leg grouping on orders', async () => {
    resetApp('/orders')
    setTokenSnapshot(buildAuthResponse())
    server.use(http.get(`${apiBaseUrl}/orders`, () => HttpResponse.json({ data: [buildOrder({ market_type: 'options', ticker: 'AAPL271217C00150000', asset_class: 'option', underlying_ticker: 'AAPL', option_type: 'call', strike: 150, expiry: '2027-12-17T00:00:00Z', contract_multiplier: 100, position_intent: 'buy_to_open', leg_group_id: '00000000-0000-4000-8000-000000000099' })], total: 1, limit: 20, offset: 0 })))
    render(<App />)

    expect((await screen.findAllByText('AAPL271217C00150000')).length).toBeGreaterThan(0)
    expect(screen.getAllByText(/100× · buy_to_open · leg 00000000/i).length).toBeGreaterThan(0)
  })

  it('keeps order filters in URL and renders unknown order states safely', async () => {
    resetApp('/orders')
    state.scenario = 'partial-service-failure'
    setTokenSnapshot(buildAuthResponse())
    render(<App />)

    expect((await screen.findAllByText(/total unavailable/i)).length).toBeGreaterThan(0)
    expect((await screen.findAllByText(/Unknown: new order status/i)).length).toBeGreaterThan(0)
    await userEvent.selectOptions(screen.getByLabelText(/^side/i), 'sell')
    await userEvent.type(screen.getByLabelText(/^ticker/i), 'live')
    expect(window.location.search).toContain('side=sell')
    expect(window.location.search).toContain('ticker=LIVE')
    expect((await screen.findAllByText('LIVE')).length).toBeGreaterThan(0)
    expect(screen.queryByText('AUGR')).toBeNull()
  })

  it('shows order empty, retry, 501, and realtime stale states', async () => {
    resetApp('/orders')
    state.scenario = 'empty-data'
    setTokenSnapshot(buildAuthResponse())
    render(<App />)
    expect(await screen.findByText(/no orders found/i)).toBeTruthy()

    resetApp('/orders')
    setTokenSnapshot(buildAuthResponse())
    let calls = 0
    server.use(http.get(`${apiBaseUrl}/orders`, () => {
      calls += 1
      if (calls === 1) return HttpResponse.json({ error: 'orders exploded', code: 'ERR_VALIDATION' }, { status: 400 })
      return HttpResponse.json({ data: [{ id: '00000000-0000-4000-8000-000000000040', ticker: 'AUGR', side: 'buy', order_type: 'market', quantity: 1, filled_quantity: 0, status: 'pending', broker: 'paper-broker', created_at: fixtureDate }], total: 1, limit: 20, offset: 0 })
    }))
    render(<App />)
    expect(await screen.findByRole('alert')).toHaveTextContent('orders exploded')
    await userEvent.click(screen.getByRole('button', { name: /reload/i }))
    expect(await screen.findByRole('table', { name: /^orders$/i })).toBeTruthy()

    act(() => FakeWebSocket.instances[0]!.onmessage?.({ data: JSON.stringify({ type: 'order_filled', timestamp: fixtureDate }) }))
    expect(await screen.findByText(/order rows are read-only and may be stale/i)).toBeTruthy()

    resetApp('/orders')
    setTokenSnapshot(buildAuthResponse())
    server.use(http.get(`${apiBaseUrl}/orders`, () => HttpResponse.json({ error: 'orders not configured', code: 'ERR_NOT_IMPLEMENTED' }, { status: 501 })))
    render(<App />)
    expect(await screen.findByText(/feature unavailable/i)).toBeTruthy()
  })

  it('renders order detail with fills and linked evidence', async () => {
    resetApp('/orders/00000000-0000-4000-8000-000000000040')
    setTokenSnapshot(buildAuthResponse())
    render(<App />)

    expect(await screen.findByRole('heading', { name: /augr order/i })).toBeTruthy()
    expect(screen.getByRole('table', { name: /order fills/i })).toBeTruthy()
    expect(screen.getByRole('link', { name: /open strategy/i })).toHaveAttribute('href', '/strategies/00000000-0000-4000-8000-000000000010')
    expect(screen.getByRole('link', { name: /open run/i })).toHaveAttribute('href', '/runs/00000000-0000-4000-8000-000000000020')
    expect((await screen.findAllByText(/DEV-PAPER-FILL-1/i)).length).toBeGreaterThan(0)
  })

  it('shows order detail empty fills, not-found, and feature-unavailable states', async () => {
    resetApp('/orders/00000000-0000-4000-8000-000000000040')
    state.scenario = 'empty-data'
    setTokenSnapshot(buildAuthResponse())
    render(<App />)
    expect(await screen.findByText(/no fills recorded/i)).toBeTruthy()

    resetApp('/orders/00000000-0000-4000-8000-000000000999')
    setTokenSnapshot(buildAuthResponse())
    render(<App />)
    expect(await screen.findByText(/order not found/i)).toBeTruthy()

    resetApp('/orders/00000000-0000-4000-8000-000000000040')
    setTokenSnapshot(buildAuthResponse())
    server.use(http.get(`${apiBaseUrl}/orders/:id`, () => HttpResponse.json({ error: 'orders not configured', code: 'ERR_NOT_IMPLEMENTED' }, { status: 501 })))
    render(<App />)
    expect(await screen.findByText(/feature unavailable/i)).toBeTruthy()
  })

  it('renders unknown order detail values safely and marks realtime fills stale', async () => {
    resetApp('/orders/00000000-0000-4000-8000-000000000040')
    state.scenario = 'partial-service-failure'
    setTokenSnapshot(buildAuthResponse())
    render(<App />)

    expect((await screen.findAllByText(/Unknown: new order status/i)).length).toBeGreaterThan(0)
    expect((await screen.findAllByText(/Unknown: new side/i)).length).toBeGreaterThan(0)
    expect((await screen.findAllByText(/<script>alert\(1\)<\/script>/i)).length).toBeGreaterThan(0)
    expect(document.querySelector('script')).toBeNull()

    act(() => FakeWebSocket.instances[0]!.onmessage?.({ data: JSON.stringify({ type: 'order_filled', data: { order_id: '00000000-0000-4000-8000-000000000040' }, timestamp: fixtureDate }) }))
    expect(await screen.findByText(/order detail and fills are read-only and may be stale/i)).toBeTruthy()
  })

  it('lists trades with order links and position evidence', async () => {
    resetApp('/trades')
    setTokenSnapshot(buildAuthResponse())
    render(<App />)

    expect(await screen.findByRole('heading', { name: /^trades$/i })).toBeTruthy()
    expect(await screen.findByRole('table', { name: /^trades$/i })).toBeTruthy()
    expect(screen.getAllByRole('link', { name: /open order/i })[0]).toHaveAttribute('href', '/orders/00000000-0000-4000-8000-000000000040')
    expect(screen.getAllByRole('link', { name: /position trades/i })[0]).toHaveAttribute('href', '/trades?position_id=00000000-0000-4000-8000-000000000030')
    expect((await screen.findAllByText(/Position/i)).length).toBeGreaterThan(0)
    expect((await screen.findAllByText(/DEV-PAPER-FILL-1/i)).length).toBeGreaterThan(0)
  })

  it('keeps trade filters in URL and renders unknown trade values safely', async () => {
    resetApp('/trades')
    state.scenario = 'partial-service-failure'
    setTokenSnapshot(buildAuthResponse())
    render(<App />)

    expect((await screen.findAllByText(/total unavailable/i)).length).toBeGreaterThan(0)
    expect((await screen.findAllByText(/Unknown: new trade side/i)).length).toBeGreaterThan(0)
    expect((await screen.findAllByText(/<script>alert\(1\)<\/script>/i)).length).toBeGreaterThan(0)
    expect(document.querySelector('script')).toBeNull()
    await userEvent.selectOptions(screen.getByLabelText(/^side/i), 'sell')
    await userEvent.type(screen.getByLabelText(/^ticker/i), 'live')
    expect(window.location.search).toContain('side=sell')
    expect(window.location.search).toContain('ticker=LIVE')
    expect((await screen.findAllByText('LIVE')).length).toBeGreaterThan(0)
  })

  it('shows trade empty, retry, 501, and realtime stale states', async () => {
    resetApp('/trades')
    state.scenario = 'empty-data'
    setTokenSnapshot(buildAuthResponse())
    render(<App />)
    expect(await screen.findByText(/no trades found/i)).toBeTruthy()

    resetApp('/trades')
    setTokenSnapshot(buildAuthResponse())
    let calls = 0
    server.use(http.get(`${apiBaseUrl}/trades`, () => {
      calls += 1
      if (calls === 1) return HttpResponse.json({ error: 'trades exploded', code: 'ERR_VALIDATION' }, { status: 400 })
      return HttpResponse.json({ data: [{ id: '00000000-0000-4000-8000-000000000050', order_id: '00000000-0000-4000-8000-000000000040', position_id: '00000000-0000-4000-8000-000000000030', ticker: 'AUGR', side: 'buy', quantity: 1, price: 100, fee: 0, executed_at: fixtureDate, created_at: fixtureDate }], total: 1, limit: 20, offset: 0 })
    }))
    render(<App />)
    expect(await screen.findByRole('alert')).toHaveTextContent('trades exploded')
    await userEvent.click(screen.getByRole('button', { name: /reload/i }))
    expect(await screen.findByRole('table', { name: /^trades$/i })).toBeTruthy()

    act(() => FakeWebSocket.instances[0]!.onmessage?.({ data: JSON.stringify({ type: 'order_filled', timestamp: fixtureDate }) }))
    expect(await screen.findByText(/trade rows are read-only and may be stale/i)).toBeTruthy()

    resetApp('/trades')
    setTokenSnapshot(buildAuthResponse())
    server.use(http.get(`${apiBaseUrl}/trades`, () => HttpResponse.json({ error: 'trades not configured', code: 'ERR_NOT_IMPLEMENTED' }, { status: 501 })))
    render(<App />)
    expect(await screen.findByText(/feature unavailable/i)).toBeTruthy()
  })

  it('creates a paper strategy only after confirmation and verified detail fetch', async () => {
    resetApp('/strategies/new')
    setTokenSnapshot(buildAuthResponse())
    let createCalls = 0
    let postedBody: Record<string, unknown> | null = null
    server.use(
      http.post(`${apiBaseUrl}/strategies`, async ({ request }) => {
        createCalls += 1
        postedBody = await request.json() as Record<string, unknown>
        return HttpResponse.json(buildStrategy({
          id: '00000000-0000-4000-8000-000000000012',
          name: 'Paper Alpha',
          ticker: 'PAPR',
          market_type: 'stock',
          config: postedBody.config ?? {},
          is_paper: true,
          status: 'active',
        }), { status: 201 })
      }),
      http.get(`${apiBaseUrl}/strategies/:id`, () => HttpResponse.json(buildStrategy({
        id: '00000000-0000-4000-8000-000000000012',
        name: 'Paper Alpha',
        ticker: 'PAPR',
        is_paper: true,
        status: 'active',
      }))),
    )
    render(<App />)

    expect(await screen.findByRole('heading', { name: /new paper strategy/i })).toBeTruthy()
    await userEvent.type(screen.getByLabelText(/^name/i), 'Paper Alpha')
    await userEvent.type(screen.getByLabelText(/^ticker/i), 'papr')
    await userEvent.click(screen.getByRole('button', { name: /review paper create/i }))
    expect(createCalls).toBe(0)
    const dialog = screen.getByRole('dialog', { name: /create paper strategy/i })
    expect(within(dialog).getByText(/paper only/i)).toBeTruthy()
    await userEvent.click(within(dialog).getByRole('button', { name: /create paper strategy/i }))

    await waitFor(() => expect(window.location.pathname).toBe('/strategies/00000000-0000-4000-8000-000000000012'))
    expect(createCalls).toBe(1)
    expect(postedBody).toMatchObject({ name: 'Paper Alpha', ticker: 'PAPR', is_paper: true })
    expect(postedBody).not.toHaveProperty('status')
    expect(postedBody).not.toHaveProperty('skip_next_run')
    expect(await screen.findByRole('heading', { name: /paper alpha/i })).toBeTruthy()
  })

  it('validates create form JSON before posting', async () => {
    resetApp('/strategies/new')
    setTokenSnapshot(buildAuthResponse())
    let createCalls = 0
    server.use(http.post(`${apiBaseUrl}/strategies`, () => { createCalls += 1; return HttpResponse.json(buildStrategy(), { status: 201 }) }))
    render(<App />)

    await screen.findByRole('heading', { name: /new paper strategy/i })
    await userEvent.type(screen.getByLabelText(/^name/i), 'Bad Config')
    await userEvent.type(screen.getByLabelText(/^ticker/i), 'BAD')
    const config = screen.getByLabelText(/config json/i)
    await userEvent.clear(config)
    await userEvent.type(config, 'not json')
    await userEvent.click(screen.getByRole('button', { name: /review paper create/i }))

    expect(await screen.findByText(/config must be valid json/i)).toBeTruthy()
    expect(createCalls).toBe(0)
  })

  it('shows server-side create validation and unknown completion messages', async () => {
    resetApp('/strategies/new')
    setTokenSnapshot(buildAuthResponse())
    server.use(http.post(`${apiBaseUrl}/strategies`, () => HttpResponse.json({ error: 'schedule cron rejected', code: 'ERR_VALIDATION' }, { status: 400 })))
    render(<App />)

    await userEvent.type(await screen.findByLabelText(/^name/i), 'Paper Beta')
    await userEvent.type(screen.getByLabelText(/^ticker/i), 'BETA')
    await userEvent.click(screen.getByRole('button', { name: /review paper create/i }))
    await userEvent.click(within(screen.getByRole('dialog')).getByRole('button', { name: /create paper strategy/i }))
    expect(await screen.findByText(/schedule cron rejected/i)).toBeTruthy()

    resetApp('/strategies/new')
    setTokenSnapshot(buildAuthResponse())
    server.use(http.post(`${apiBaseUrl}/strategies`, () => HttpResponse.error()))
    render(<App />)
    await userEvent.type(await screen.findByLabelText(/^name/i), 'Paper Gamma')
    await userEvent.type(screen.getByLabelText(/^ticker/i), 'GAM')
    await userEvent.click(screen.getByRole('button', { name: /review paper create/i }))
    await userEvent.click(within(screen.getByRole('dialog')).getByRole('button', { name: /create paper strategy/i }))
    expect(await screen.findByText(/completion is unknown/i)).toBeTruthy()
  })

  it('edits safe fields for paper strategies after confirmation and verified refetch', async () => {
    resetApp(`/strategies/${strategyId}/edit`)
    setTokenSnapshot(buildAuthResponse())
    let savedName = 'DEV PAPER Mean Reversion'
    let updateCalls = 0
    let postedBody: Record<string, unknown> | null = null
    server.use(
      http.get(`${apiBaseUrl}/strategies/:id`, () => HttpResponse.json(buildStrategy({ id: strategyId, name: savedName, is_paper: true, status: 'active' }))),
      http.put(`${apiBaseUrl}/strategies/:id`, async ({ request }) => {
        updateCalls += 1
        postedBody = await request.json() as Record<string, unknown>
        savedName = String(postedBody.name)
        return HttpResponse.json(buildStrategy({ id: strategyId, name: savedName, ticker: String(postedBody.ticker), is_paper: true, status: 'active' }))
      }),
    )
    render(<App />)

    expect(await screen.findByRole('heading', { name: /edit dev paper mean reversion/i })).toBeTruthy()
    const name = screen.getByLabelText(/^name/i)
    await userEvent.clear(name)
    await userEvent.type(name, 'Edited Paper')
    await userEvent.click(screen.getByRole('button', { name: /review paper edit/i }))
    expect(updateCalls).toBe(0)
    await userEvent.click(within(screen.getByRole('dialog', { name: /save paper strategy edit/i })).getByRole('button', { name: /save paper edit/i }))

    await waitFor(() => expect(window.location.pathname).toBe(`/strategies/${strategyId}`))
    expect(updateCalls).toBe(1)
    expect(postedBody).toMatchObject({ name: 'Edited Paper' })
    expect(postedBody).not.toHaveProperty('is_paper')
    expect(postedBody).not.toHaveProperty('status')
    expect(postedBody).not.toHaveProperty('skip_next_run')
    expect(await screen.findByRole('heading', { name: /edited paper/i })).toBeTruthy()
  })

  it('disables edit save for live strategies', async () => {
    resetApp(`/strategies/${strategyId}/edit`)
    setTokenSnapshot(buildAuthResponse())
    let updateCalls = 0
    server.use(
      http.get(`${apiBaseUrl}/strategies/:id`, () => HttpResponse.json(buildStrategy({ id: strategyId, is_paper: false, status: 'active' }))),
      http.put(`${apiBaseUrl}/strategies/:id`, () => { updateCalls += 1; return HttpResponse.json(buildStrategy()) }),
    )
    render(<App />)

    expect((await screen.findAllByText('LIVE')).length).toBeGreaterThan(0)
    expect(screen.getByText(/live strategies cannot be edited/i)).toBeTruthy()
    expect(screen.getByRole('button', { name: /review paper edit/i })).toBeDisabled()
    expect(updateCalls).toBe(0)
  })

  it('preserves edit input on validation and stale conflicts', async () => {
    resetApp(`/strategies/${strategyId}/edit`)
    setTokenSnapshot(buildAuthResponse())
    server.use(http.put(`${apiBaseUrl}/strategies/:id`, () => HttpResponse.json({ error: 'strategy changed since it was loaded', code: 'ERR_CONFLICT' }, { status: 409 })))
    render(<App />)

    await screen.findByRole('heading', { name: /edit dev paper mean reversion/i })
    const name = screen.getByLabelText(/^name/i)
    await userEvent.clear(name)
    await userEvent.type(name, 'Unsaved Edit')
    await userEvent.click(screen.getByRole('button', { name: /review paper edit/i }))
    await userEvent.click(within(screen.getByRole('dialog')).getByRole('button', { name: /save paper edit/i }))
    expect(await screen.findByText(/strategy changed since it was loaded/i)).toBeTruthy()
    expect(screen.getByDisplayValue('Unsaved Edit')).toBeTruthy()

    await userEvent.click(within(screen.getByRole('dialog')).getByRole('button', { name: /cancel/i }))
    const config = screen.getByLabelText(/config json/i)
    await userEvent.clear(config)
    await userEvent.type(config, 'not json')
    await userEvent.click(screen.getByRole('button', { name: /review paper edit/i }))
    expect(await screen.findByText(/config must be valid json/i)).toBeTruthy()
  })

  it('warns when edit verification refetch fails after successful save', async () => {
    resetApp(`/strategies/${strategyId}/edit`)
    setTokenSnapshot(buildAuthResponse())
    let failDetail = false
    server.use(
      http.get(`${apiBaseUrl}/strategies/:id`, () => failDetail ? HttpResponse.json({ error: 'detail unavailable', code: 'ERR_INTERNAL' }, { status: 500 }) : HttpResponse.json(buildStrategy({ id: strategyId, is_paper: true }))),
      http.put(`${apiBaseUrl}/strategies/:id`, () => { failDetail = true; return HttpResponse.json(buildStrategy({ id: strategyId, name: 'Saved Name', is_paper: true })) }),
    )
    render(<App />)

    await screen.findByRole('heading', { name: /edit dev paper mean reversion/i })
    const name = screen.getByLabelText(/^name/i)
    await userEvent.clear(name)
    await userEvent.type(name, 'Saved Name')
    await userEvent.click(screen.getByRole('button', { name: /review paper edit/i }))
    await userEvent.click(within(screen.getByRole('dialog')).getByRole('button', { name: /save paper edit/i }))
    expect(await screen.findByText(/verification fetch failed/i)).toBeTruthy()
  })

  it('renders strategy detail overview with breadcrumbs, latest run, and safe mode/status labels', async () => {
    resetApp(`/strategies/${strategyId}`)
    setTokenSnapshot(buildAuthResponse())
    render(<App />)

    expect(await screen.findByRole('heading', { name: /dev paper mean reversion/i })).toBeTruthy()
    expect(screen.getAllByRole('link', { name: /strategies/i }).some((link) => link.getAttribute('href') === '/strategies')).toBe(true)
    expect(screen.getAllByText('PAPER').length).toBeGreaterThan(0)
    expect(screen.getAllByText('active').length).toBeGreaterThan(0)
    expect(screen.getByRole('heading', { name: /identity/i })).toBeTruthy()
    expect(screen.getByRole('heading', { name: /latest run summary/i })).toBeTruthy()
    expect(screen.getByRole('link', { name: /open run/i })).toHaveAttribute('href', '/runs/00000000-0000-4000-8000-000000000020')
  })

  it('supports strategy detail config tab through URL and keyboard tab controls', async () => {
    resetApp(`/strategies/${strategyId}?tab=config`)
    setTokenSnapshot(buildAuthResponse())
    render(<App />)

    const configTab = await screen.findByRole('tab', { name: /config/i })
    expect(configTab).toHaveAttribute('aria-selected', 'true')
    expect(screen.getByRole('tabpanel', { name: /strategy config/i })).toBeTruthy()
    expect(screen.getByText(/"fixture": true/i)).toBeTruthy()
    configTab.focus()
    await userEvent.keyboard('{ArrowLeft}')
    expect(window.location.search).not.toContain('tab=config')
    expect(screen.getByRole('tab', { name: /overview/i })).toHaveAttribute('aria-selected', 'true')
  })

  it('renders strategy detail 404 and 501 states', async () => {
    resetApp('/strategies/00000000-0000-4000-8000-000000000999')
    setTokenSnapshot(buildAuthResponse())
    server.use(http.get(`${apiBaseUrl}/strategies/:id`, () => HttpResponse.json({ error: 'strategy not found', code: 'ERR_NOT_FOUND' }, { status: 404 })))
    render(<App />)

    expect(await screen.findByText(/strategy not found/i)).toBeTruthy()

    resetApp(`/strategies/${strategyId}`)
    setTokenSnapshot(buildAuthResponse())
    server.use(http.get(`${apiBaseUrl}/strategies/:id`, () => HttpResponse.json({ error: 'not configured', code: 'ERR_NOT_IMPLEMENTED' }, { status: 501 })))
    render(<App />)
    expect(await screen.findByText(/feature unavailable/i)).toBeTruthy()
  })

  it('renders unknown strategy detail status, missing summary, and escaped config JSON', async () => {
    resetApp(`/strategies/${strategyId}`)
    setTokenSnapshot(buildAuthResponse())
    server.use(
      http.get(`${apiBaseUrl}/strategies/:id`, () => HttpResponse.json(buildStrategy({
        status: 'new_backend_status',
        config: { fixture: true, nested: { threshold: 0.42 }, unsafe: '<script>alert(1)</script>' },
        latest_run_summary: {
          id: '00000000-0000-4000-8000-000000000020',
          strategy_id: strategyId,
          ticker: 'AUGR',
          status: 'new_run_status',
          signal: 'unknown_signal',
          started_at: fixtureDate,
        },
      }))),
    )
    render(<App />)

    expect((await screen.findAllByText(/Unknown: new_backend_status/i)).length).toBeGreaterThan(0)
    expect(screen.getByText(/new run status/i)).toBeTruthy()
    await userEvent.click(screen.getByRole('tab', { name: /config/i }))
    expect(screen.getByText(/<script>alert\(1\)<\/script>/i)).toBeTruthy()
    expect(document.querySelector('script')).toBeNull()

    resetApp(`/strategies/${strategyId}`)
    setTokenSnapshot(buildAuthResponse())
    server.use(http.get(`${apiBaseUrl}/strategies/:id`, () => HttpResponse.json(buildStrategy({ latest_run_summary: undefined }))))
    render(<App />)
    expect(await screen.findByText(/no latest run summary/i)).toBeTruthy()
  })

  it('renders strategy report latest JSON and historical report pagination', async () => {
    resetApp(`/strategies/${strategyId}?tab=reports`)
    setTokenSnapshot(buildAuthResponse())
    render(<App />)

    expect(await screen.findByRole('tab', { name: /reports/i })).toHaveAttribute('aria-selected', 'true')
    expect(await screen.findByRole('heading', { name: /latest report/i })).toBeTruthy()
    expect(screen.getByText(/paper validation passed/i)).toBeTruthy()
    expect(screen.getByRole('table', { name: /strategy report history/i })).toBeTruthy()
    expect(screen.getAllByText(/offset 0/i).length).toBeGreaterThan(0)
    const reportPagination = screen.getByLabelText(/report history pagination/i)
    await userEvent.click(within(reportPagination).getByRole('button', { name: /next/i }))
    expect(window.location.search).toContain('report_offset=5')
  })

  it('shows no-report and feature-unavailable report states', async () => {
    resetApp(`/strategies/${strategyId}?tab=reports`)
    state.scenario = 'empty-data'
    setTokenSnapshot(buildAuthResponse())
    render(<App />)
    expect(await screen.findByText(/no completed latest report/i)).toBeTruthy()
    expect(screen.getByText(/no historical reports/i)).toBeTruthy()

    resetApp(`/strategies/${strategyId}?tab=reports`)
    setTokenSnapshot(buildAuthResponse())
    server.use(
      http.get(`${apiBaseUrl}/strategies/:id/reports/latest`, () => HttpResponse.json({ error: 'reports not configured', code: 'ERR_NOT_IMPLEMENTED' }, { status: 501 })),
      http.get(`${apiBaseUrl}/strategies/:id/reports`, () => HttpResponse.json({ error: 'reports not configured', code: 'ERR_NOT_IMPLEMENTED' }, { status: 501 })),
    )
    render(<App />)
    expect(await screen.findAllByText(/feature unavailable/i)).toHaveLength(2)
  })

  it('renders unknown report metadata safely', async () => {
    resetApp(`/strategies/${strategyId}?tab=reports`)
    state.scenario = 'partial-service-failure'
    setTokenSnapshot(buildAuthResponse())
    render(<App />)

    expect((await screen.findAllByText(/Unknown: new_report_status/i)).length).toBeGreaterThan(0)
    expect(screen.getByText(/new backend report/i)).toBeTruthy()
    expect(screen.getByText(/<script>alert\(1\)<\/script>/i)).toBeTruthy()
    expect(document.querySelector('script')).toBeNull()
  })

  it('marks reports stale after matching realtime event', async () => {
    resetApp(`/strategies/${strategyId}?tab=reports`)
    setTokenSnapshot(buildAuthResponse())
    render(<App />)

    expect(await screen.findByRole('heading', { name: /latest report/i })).toBeTruthy()
    act(() => {
      FakeWebSocket.instances[0]!.onmessage?.({ data: JSON.stringify({ type: 'signal', strategy_id: strategyId, timestamp: fixtureDate }) })
    })
    expect(await screen.findByText(/do not infer trading safety from stale reports/i)).toBeTruthy()
  })

  it('marks strategy detail stale after matching realtime event and disables existing action controls', async () => {
    resetApp(`/strategies/${strategyId}`)
    setTokenSnapshot(buildAuthResponse())
    render(<App />)

    expect(await screen.findByRole('heading', { name: /dev paper mean reversion/i })).toBeTruthy()
    const pauseButton = screen.getByRole('button', { name: /pause paper strategy/i })
    expect(pauseButton).not.toBeDisabled()
    act(() => FakeWebSocket.instances[0]!.emit('pipeline_health', { ok: true }))
    act(() => {
      FakeWebSocket.instances[0]!.onmessage?.({ data: JSON.stringify({ type: 'pipeline_health', strategy_id: strategyId, timestamp: fixtureDate }) })
    })
    expect(await screen.findByText(/realtime activity was received/i)).toBeTruthy()
    expect(screen.getByRole('button', { name: /pause paper strategy/i })).toBeDisabled()
  })

  it('pauses a paper strategy only after explicit confirmation and verified refetch', async () => {
    resetApp(`/strategies/${strategyId}`)
    setTokenSnapshot(buildAuthResponse())
    let pauseCalls = 0
    let paused = false
    server.use(
      http.get(`${apiBaseUrl}/strategies/:id`, () => HttpResponse.json(buildStrategy({ status: paused ? 'paused' : 'active', is_paper: true }))),
      http.post(`${apiBaseUrl}/strategies/:id/pause`, () => {
        pauseCalls += 1
        paused = true
        return HttpResponse.json(buildStrategy({ status: 'paused', is_paper: true }))
      }),
    )
    render(<App />)

    expect(await screen.findByRole('heading', { name: /dev paper mean reversion/i })).toBeTruthy()
    expect(screen.getAllByText('PAPER').length).toBeGreaterThan(0)
    await userEvent.click(screen.getByRole('button', { name: /pause paper strategy/i }))
    expect(pauseCalls).toBe(0)
    const dialog = screen.getByRole('dialog', { name: /pause paper strategy/i })
    expect(within(dialog).getByText(/scheduled or active paper behavior/i)).toBeTruthy()
    await userEvent.click(within(dialog).getByRole('button', { name: /pause paper strategy/i }))

    expect(await screen.findByText(/confirmed server state: paused/i)).toBeTruthy()
    expect(pauseCalls).toBe(1)
  })

  it('does not submit pause for live strategies', async () => {
    resetApp(`/strategies/${strategyId}`)
    setTokenSnapshot(buildAuthResponse())
    let pauseCalls = 0
    server.use(
      http.get(`${apiBaseUrl}/strategies/:id`, () => HttpResponse.json(buildStrategy({ is_paper: false, status: 'active' }))),
      http.post(`${apiBaseUrl}/strategies/:id/pause`, () => {
        pauseCalls += 1
        return HttpResponse.json(buildStrategy({ is_paper: false, status: 'paused' }))
      }),
    )
    render(<App />)

    expect((await screen.findAllByText('LIVE')).length).toBeGreaterThan(0)
    expect(screen.getByRole('button', { name: /pause paper strategy/i })).toBeDisabled()
    expect(screen.getByText(/live strategies cannot use/i)).toBeTruthy()
    expect(pauseCalls).toBe(0)
  })

  it('resumes a paused paper strategy after confirmation and verified refetch', async () => {
    resetApp(`/strategies/${strategyId}`)
    setTokenSnapshot(buildAuthResponse())
    let resumed = false
    let resumeCalls = 0
    server.use(
      http.get(`${apiBaseUrl}/strategies/:id`, () => HttpResponse.json(buildStrategy({ status: resumed ? 'active' : 'paused', is_paper: true }))),
      http.post(`${apiBaseUrl}/strategies/:id/resume`, () => {
        resumeCalls += 1
        resumed = true
        return HttpResponse.json(buildStrategy({ status: 'active', is_paper: true }))
      }),
    )
    render(<App />)

    await screen.findByRole('heading', { name: /dev paper mean reversion/i })
    expect(screen.getByRole('button', { name: /resume paper strategy/i })).not.toBeDisabled()
    await userEvent.click(screen.getByRole('button', { name: /resume paper strategy/i }))
    expect(resumeCalls).toBe(0)
    const dialog = screen.getByRole('dialog', { name: /resume paper strategy/i })
    await userEvent.click(within(dialog).getByRole('button', { name: /resume paper strategy/i }))

    expect(await screen.findByText(/resume confirmed.*active/i)).toBeTruthy()
    expect(resumeCalls).toBe(1)
  })

  it('marks skip-next after confirmation and blocks duplicate skip state', async () => {
    resetApp(`/strategies/${strategyId}`)
    setTokenSnapshot(buildAuthResponse())
    let skipped = false
    let skipCalls = 0
    server.use(
      http.get(`${apiBaseUrl}/strategies/:id`, () => HttpResponse.json(buildStrategy({ status: 'active', is_paper: true, skip_next_run: skipped }))),
      http.post(`${apiBaseUrl}/strategies/:id/skip-next`, () => {
        skipCalls += 1
        skipped = true
        return HttpResponse.json(buildStrategy({ status: 'active', is_paper: true, skip_next_run: true }))
      }),
    )
    render(<App />)

    await screen.findByRole('heading', { name: /dev paper mean reversion/i })
    await userEvent.click(screen.getByRole('button', { name: /skip next paper run/i }))
    const dialog = screen.getByRole('dialog', { name: /skip next paper run/i })
    await userEvent.click(within(dialog).getByRole('button', { name: /skip next run/i }))

    expect(await screen.findByText(/skip-next confirmed.*skip next: yes/i)).toBeTruthy()
    expect(skipCalls).toBe(1)
    expect(screen.getByRole('button', { name: /skip next paper run/i })).toBeDisabled()
  })

  it('starts a manual paper run without optimistic state and handles unavailable runner', async () => {
    resetApp(`/strategies/${strategyId}`)
    setTokenSnapshot(buildAuthResponse())
    let runCalls = 0
    server.use(
      http.get(`${apiBaseUrl}/strategies/:id`, () => HttpResponse.json(buildStrategy({ status: 'active', is_paper: true }))),
      http.post(`${apiBaseUrl}/strategies/:id/run`, () => {
        runCalls += 1
        return HttpResponse.json({ status: 'accepted', strategy_id: strategyId, message: 'strategy run started' }, { status: 202 })
      }),
    )
    render(<App />)

    await screen.findByRole('heading', { name: /dev paper mean reversion/i })
    await userEvent.click(screen.getByRole('button', { name: /run paper strategy now/i }))
    expect(runCalls).toBe(0)
    const dialog = screen.getByRole('dialog', { name: /run paper strategy now/i })
    await userEvent.click(within(dialog).getByRole('button', { name: /start paper run/i }))

    expect(await screen.findByText(/manual run accepted.*active/i)).toBeTruthy()
    expect(runCalls).toBe(1)

    server.use(http.post(`${apiBaseUrl}/strategies/:id/run`, () => HttpResponse.json({ error: 'manual strategy runs are not configured', code: 'ERR_NOT_IMPLEMENTED' }, { status: 501 })))
    await userEvent.click(screen.getByRole('button', { name: /run paper strategy now/i }))
    await userEvent.click(within(screen.getByRole('dialog')).getByRole('button', { name: /start paper run/i }))
    expect(await within(screen.getByRole('dialog')).findByText(/not available on this server/i)).toBeTruthy()
  })

  it('prevents duplicate pause submissions and does not optimistically change status', async () => {
    resetApp(`/strategies/${strategyId}`)
    setTokenSnapshot(buildAuthResponse())
    let pauseCalls = 0
    server.use(
      http.get(`${apiBaseUrl}/strategies/:id`, () => HttpResponse.json(buildStrategy({ status: 'active', is_paper: true }))),
      http.post(`${apiBaseUrl}/strategies/:id/pause`, async () => {
        pauseCalls += 1
        await delay(100)
        return HttpResponse.json(buildStrategy({ status: 'paused', is_paper: true }))
      }),
    )
    render(<App />)

    await screen.findByRole('heading', { name: /dev paper mean reversion/i })
    await userEvent.click(screen.getByRole('button', { name: /pause paper strategy/i }))
    const dialog = screen.getByRole('dialog')
    await userEvent.dblClick(within(dialog).getByRole('button', { name: /pause paper strategy/i }))

    expect(pauseCalls).toBe(1)
    expect(within(screen.getByRole('dialog')).getByText(/currently/).textContent).toContain('active')
  })

  it('pre-refreshes before pause when access token is expired', async () => {
    resetApp(`/strategies/${strategyId}`)
    setTokenSnapshot(buildAuthResponse({ expires_at: '2027-01-15T13:00:00Z' }))
    let refreshCalls = 0
    let pauseAuthHeader: string | null = null
    server.use(
      http.post(`${apiBaseUrl}/auth/refresh`, () => {
        refreshCalls += 1
        return HttpResponse.json(buildAuthResponse({ access_token: 'dev-paper-access-token-refreshed' }))
      }),
      http.get(`${apiBaseUrl}/strategies/:id`, () => HttpResponse.json(buildStrategy({ status: 'active', is_paper: true }))),
      http.post(`${apiBaseUrl}/strategies/:id/pause`, ({ request }) => {
        pauseAuthHeader = request.headers.get('authorization')
        return HttpResponse.json(buildStrategy({ status: 'paused', is_paper: true }))
      }),
    )
    render(<App />)
    await screen.findByRole('heading', { name: /dev paper mean reversion/i })
    await userEvent.click(screen.getByRole('button', { name: /pause paper strategy/i }))
    setTokenSnapshot(buildAuthResponse({ access_token: 'expired-before-pause', expires_at: '2020-01-01T00:00:00Z' }))
    await userEvent.click(within(screen.getByRole('dialog')).getByRole('button', { name: /pause paper strategy/i }))

    await waitFor(() => expect(refreshCalls).toBe(1))
    expect(pauseAuthHeader).toBe('Bearer dev-paper-access-token-refreshed')
  })

  it.each([
    ['409 conflict', 409, 'ERR_CONFLICT', /state changed/i],
    ['validation error', 422, 'ERR_VALIDATION', /rejected|review/i],
    ['rate limit', 429, 'ERR_RATE_LIMITED', /rate limited/i],
    ['internal error', 500, 'ERR_INTERNAL', /server could not complete/i],
  ])('keeps confirmation open for %s pause errors', async (_name, status, code, message) => {
    resetApp(`/strategies/${strategyId}`)
    setTokenSnapshot(buildAuthResponse())
    server.use(
      http.get(`${apiBaseUrl}/strategies/:id`, () => HttpResponse.json(buildStrategy({ status: 'active', is_paper: true }))),
      http.post(`${apiBaseUrl}/strategies/:id/pause`, () => HttpResponse.json({ error: 'pause failed', code }, { status })),
    )
    render(<App />)
    await screen.findByRole('heading', { name: /dev paper mean reversion/i })
    await userEvent.click(screen.getByRole('button', { name: /pause paper strategy/i }))
    await userEvent.click(within(screen.getByRole('dialog')).getByRole('button', { name: /pause paper strategy/i }))

    expect(await within(screen.getByRole('dialog')).findByRole('alert')).toHaveTextContent(message)
  })

  it('shows unknown completion on network failure', async () => {
    resetApp(`/strategies/${strategyId}`)
    setTokenSnapshot(buildAuthResponse())
    server.use(
      http.get(`${apiBaseUrl}/strategies/:id`, () => HttpResponse.json(buildStrategy({ status: 'active', is_paper: true }))),
      http.post(`${apiBaseUrl}/strategies/:id/pause`, () => HttpResponse.error()),
    )
    render(<App />)
    await screen.findByRole('heading', { name: /dev paper mean reversion/i })
    await userEvent.click(screen.getByRole('button', { name: /pause paper strategy/i }))
    await userEvent.click(within(screen.getByRole('dialog')).getByRole('button', { name: /pause paper strategy/i }))

    expect(await within(screen.getByRole('dialog')).findByText(/completion is unknown/i)).toBeTruthy()
  })

  it('warns when verification refetch fails after successful pause', async () => {
    resetApp(`/strategies/${strategyId}`)
    setTokenSnapshot(buildAuthResponse())
    let pauseAccepted = false
    server.use(
      http.get(`${apiBaseUrl}/strategies/:id`, () => {
        if (pauseAccepted) return HttpResponse.json({ error: 'verification failed', code: 'ERR_INTERNAL' }, { status: 500 })
        return HttpResponse.json(buildStrategy({ status: 'active', is_paper: true }))
      }),
      http.post(`${apiBaseUrl}/strategies/:id/pause`, () => {
        pauseAccepted = true
        return HttpResponse.json(buildStrategy({ status: 'paused', is_paper: true }))
      }),
    )
    render(<App />)
    await screen.findByRole('heading', { name: /dev paper mean reversion/i })
    await userEvent.click(screen.getByRole('button', { name: /pause paper strategy/i }))
    await userEvent.click(within(screen.getByRole('dialog')).getByRole('button', { name: /pause paper strategy/i }))

    expect(await within(screen.getByRole('dialog')).findByText(/confirmed server state could not be refetched/i)).toBeTruthy()
  })

  it('deletes a paper strategy only after typed confirmation and verified absence', async () => {
    resetApp(`/strategies/${strategyId}`)
    setTokenSnapshot(buildAuthResponse())
    let deleted = false
    let deleteCalls = 0
    server.use(
      http.get(`${apiBaseUrl}/runs`, () => HttpResponse.json({ data: [], total: 0, limit: 1, offset: 0 })),
      http.get(`${apiBaseUrl}/strategies/:id`, () => {
        if (deleted) return HttpResponse.json({ error: 'strategy not found', code: 'ERR_NOT_FOUND' }, { status: 404 })
        return HttpResponse.json(buildStrategy({ status: 'active', is_paper: true }))
      }),
      http.delete(`${apiBaseUrl}/strategies/:id`, () => {
        deleteCalls += 1
        deleted = true
        return new HttpResponse(null, { status: 204 })
      }),
    )
    render(<App />)

    await screen.findByRole('heading', { name: /dev paper mean reversion/i })
    const deleteButton = await screen.findByRole('button', { name: /delete paper strategy/i })
    expect(deleteButton).not.toBeDisabled()
    await userEvent.click(deleteButton)
    expect(deleteCalls).toBe(0)
    const dialog = screen.getByRole('dialog', { name: /delete paper strategy/i })
    await userEvent.click(within(dialog).getByRole('button', { name: /delete paper strategy/i }))
    expect(await within(dialog).findByRole('alert')).toHaveTextContent(/type DELETE to confirm/i)
    await userEvent.type(within(dialog).getByLabelText(/type DELETE to confirm/i), 'DELETE')
    await userEvent.click(within(dialog).getByRole('button', { name: /delete paper strategy/i }))

    await waitFor(() => expect(window.location.pathname).toBe('/strategies'))
    expect(deleteCalls).toBe(1)
  })

  it('blocks strategy delete for live, running, and stale strategy states', async () => {
    resetApp(`/strategies/${strategyId}`)
    setTokenSnapshot(buildAuthResponse())
    server.use(
      http.get(`${apiBaseUrl}/strategies/:id`, () => HttpResponse.json(buildStrategy({ is_paper: false, status: 'active' }))),
      http.get(`${apiBaseUrl}/runs`, () => HttpResponse.json({ data: [], total: 0, limit: 1, offset: 0 })),
    )
    render(<App />)
    expect((await screen.findAllByText('LIVE')).length).toBeGreaterThan(0)
    expect(screen.getByRole('button', { name: /delete paper strategy/i })).toBeDisabled()

    cleanup()
    resetApp(`/strategies/${strategyId}`)
    setTokenSnapshot(buildAuthResponse())
    server.use(
      http.get(`${apiBaseUrl}/strategies/:id`, () => HttpResponse.json(buildStrategy({ is_paper: true, status: 'active' }))),
      http.get(`${apiBaseUrl}/runs`, () => HttpResponse.json({ data: [buildRun({ strategy_id: strategyId, status: 'running' })], total: 1, limit: 1, offset: 0 })),
    )
    render(<App />)
    await screen.findByRole('heading', { name: /dev paper mean reversion/i })
    expect(await screen.findByText(/running run.*delete is blocked/i)).toBeTruthy()
    expect(screen.getByRole('button', { name: /delete paper strategy/i })).toBeDisabled()

    cleanup()
    resetApp(`/strategies/${strategyId}`)
    setTokenSnapshot(buildAuthResponse())
    server.use(
      http.get(`${apiBaseUrl}/strategies/:id`, () => HttpResponse.json(buildStrategy({ is_paper: true, status: 'active' }))),
      http.get(`${apiBaseUrl}/runs`, () => HttpResponse.json({ data: [], total: 0, limit: 1, offset: 0 })),
    )
    render(<App />)
    await screen.findByRole('heading', { name: /dev paper mean reversion/i })
    const staleDeleteButton = screen.getByRole('button', { name: /delete paper strategy/i })
    await waitFor(() => expect(staleDeleteButton).not.toBeDisabled())
    act(() => {
      FakeWebSocket.instances[0]!.onmessage?.({ data: JSON.stringify({ type: 'pipeline_health', strategy_id: strategyId, timestamp: fixtureDate }) })
    })
    expect(await screen.findByText(/realtime activity was received/i)).toBeTruthy()
    expect(staleDeleteButton).toBeDisabled()
  })

  it('blocks duplicate delete submits, treats 404 as gone, and warns on failed verification', async () => {
    resetApp(`/strategies/${strategyId}`)
    setTokenSnapshot(buildAuthResponse())
    let resolveDelete: () => void = () => { throw new Error('resolveDelete was not assigned') }
    let deleteCalls = 0
    server.use(
      http.get(`${apiBaseUrl}/runs`, () => HttpResponse.json({ data: [], total: 0, limit: 1, offset: 0 })),
      http.get(`${apiBaseUrl}/strategies/:id`, () => HttpResponse.json(buildStrategy({ status: 'active', is_paper: true }))),
      http.delete(`${apiBaseUrl}/strategies/:id`, async () => {
        deleteCalls += 1
        await new Promise<void>((resolve) => { resolveDelete = resolve })
        return new HttpResponse(null, { status: 204 })
      }),
    )
    render(<App />)
    await screen.findByRole('heading', { name: /dev paper mean reversion/i })
    await userEvent.click(await screen.findByRole('button', { name: /delete paper strategy/i }))
    let dialog = screen.getByRole('dialog', { name: /delete paper strategy/i })
    await userEvent.type(within(dialog).getByLabelText(/type DELETE to confirm/i), 'DELETE')
    await userEvent.click(within(dialog).getByRole('button', { name: /delete paper strategy/i }))
    await waitFor(() => expect(deleteCalls).toBe(1))
    expect(within(dialog).getByRole('button', { name: /working/i })).toBeDisabled()
    await userEvent.click(within(dialog).getByRole('button', { name: /working/i }))
    expect(deleteCalls).toBe(1)
    resolveDelete()
    expect(await within(dialog).findByText(/verified absence failed/i)).toBeTruthy()

    cleanup()
    resetApp(`/strategies/${strategyId}`)
    setTokenSnapshot(buildAuthResponse())
    server.use(
      http.get(`${apiBaseUrl}/runs`, () => HttpResponse.json({ data: [], total: 0, limit: 1, offset: 0 })),
      http.get(`${apiBaseUrl}/strategies/:id`, () => HttpResponse.json(buildStrategy({ status: 'active', is_paper: true }))),
      http.delete(`${apiBaseUrl}/strategies/:id`, () => HttpResponse.json({ error: 'strategy not found', code: 'ERR_NOT_FOUND' }, { status: 404 })),
    )
    render(<App />)
    await screen.findByRole('heading', { name: /dev paper mean reversion/i })
    await userEvent.click(await screen.findByRole('button', { name: /delete paper strategy/i }))
    dialog = screen.getByRole('dialog', { name: /delete paper strategy/i })
    await userEvent.type(within(dialog).getByLabelText(/type DELETE to confirm/i), 'DELETE')
    await userEvent.click(within(dialog).getByRole('button', { name: /delete paper strategy/i }))
    await waitFor(() => expect(window.location.pathname).toBe('/strategies'))
  }, 10_000)

  it('supports keyboard focus behavior in the confirmation dialog', async () => {
    resetApp(`/strategies/${strategyId}`)
    setTokenSnapshot(buildAuthResponse())
    server.use(http.get(`${apiBaseUrl}/strategies/:id`, () => HttpResponse.json(buildStrategy({ status: 'active', is_paper: true }))))
    render(<App />)
    await screen.findByRole('heading', { name: /dev paper mean reversion/i })
    await userEvent.click(screen.getByRole('button', { name: /pause paper strategy/i }))
    const dialog = screen.getByRole('dialog')

    await waitFor(() => expect(within(dialog).getByRole('button', { name: /cancel/i })).toHaveFocus())
    await userEvent.tab({ shift: true })
    expect(within(dialog).getByRole('button', { name: /pause paper strategy/i })).toHaveFocus()
    await userEvent.keyboard('{Escape}')
    expect(screen.queryByRole('dialog')).toBeNull()
  })

  it('keeps typed confirmation fields inside dialog keyboard traversal', async () => {
    resetApp(`/strategies/${strategyId}`)
    setTokenSnapshot(buildAuthResponse())
    server.use(
      http.get(`${apiBaseUrl}/runs`, () => HttpResponse.json({ data: [], total: 0, limit: 1, offset: 0 })),
      http.get(`${apiBaseUrl}/strategies/:id`, () => HttpResponse.json(buildStrategy({ status: 'active', is_paper: true }))),
    )
    render(<App />)
    await screen.findByRole('heading', { name: /dev paper mean reversion/i })
    await userEvent.click(await screen.findByRole('button', { name: /delete paper strategy/i }))
    const dialog = screen.getByRole('dialog', { name: /delete paper strategy/i })
    const token = within(dialog).getByLabelText(/type DELETE to confirm/i)

    await waitFor(() => expect(within(dialog).getByRole('button', { name: /cancel/i })).toHaveFocus())
    await userEvent.tab({ shift: true })
    expect(token).toHaveFocus()
    await userEvent.tab()
    expect(within(dialog).getByRole('button', { name: /cancel/i })).toHaveFocus()
    await userEvent.tab()
    expect(within(dialog).getByRole('button', { name: /delete paper strategy/i })).toHaveFocus()
    await userEvent.tab()
    expect(token).toHaveFocus()
  })

  it('shows websocket disconnect, reconnect subscription restoration, buffer limit, and unknown event type', async () => {
    resetApp('/cockpit')
    setTokenSnapshot(buildAuthResponse())
    let refreshCalls = 0
    server.use(
      http.post(`${apiBaseUrl}/auth/refresh`, () => {
        refreshCalls += 1
        return HttpResponse.json(buildAuthResponse({ access_token: 'dev-paper-access-token-refreshed' }))
      }),
    )
    render(<App />)
    expect(await screen.findByRole('heading', { name: /system overview/i })).toBeTruthy()
    await waitFor(() => expect(FakeWebSocket.instances[0]?.sent.some((item) => item.includes('subscribe_all'))).toBe(true))
    const first = FakeWebSocket.instances[0]!
    act(() => first.emit('unknown_new_event'))
    expect((await screen.findAllByText('unknown_new_event')).length).toBeGreaterThan(0)
    act(() => {
      for (let i = 0; i < 260; i += 1) first.emit('pipeline_health')
    })
    await waitFor(() => {
      expect(screen.getByText((_, element) => element?.textContent === 'Buffered events: 250/250')).toBeTruthy()
    })

    act(() => first.close())
    expect(await screen.findByText(/WebSocket disconnected/i)).toBeTruthy()
    await waitFor(() => expect(FakeWebSocket.instances.length).toBeGreaterThan(1), { timeout: 2500 })
    await waitFor(() => expect(FakeWebSocket.instances.at(-1)?.sent.some((item) => item.includes('subscribe_all'))).toBe(true))
    expect(refreshCalls).toBeGreaterThanOrEqual(2)
    expect(await screen.findByText('normal')).toBeTruthy()
  })
})
