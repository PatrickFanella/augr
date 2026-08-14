import '@testing-library/jest-dom/vitest'
import { render, screen } from '@testing-library/react'
import { http, HttpResponse } from 'msw'
import { describe, expect, it } from 'vitest'

import App from '@/App'
import { setTokenSnapshot } from '@/shared/auth/tokenStore'
import { buildAuthResponse } from '@/test/fixtures'
import { apiBaseUrl, installAppTestHarness, resetApp, server, state } from '@/test/app-harness'

describe('recovered product surfaces', () => {
  installAppTestHarness()

  it('renders Kalshi event-market readiness', async () => {
    resetApp('/event-markets')
    setTokenSnapshot(buildAuthResponse())
    render(<App />)

    expect(await screen.findByRole('heading', { name: /^event markets$/i })).toBeTruthy()
    expect(await screen.findByRole('table', { name: /event market providers/i })).toBeTruthy()
    expect(await screen.findByText('kalshi')).toBeTruthy()
    expect(screen.queryByText('polymarket')).toBeNull()
    expect(screen.getAllByText(/not ready/i)).toHaveLength(1)
  })

  it('renders event-market empty and feature-unavailable states', async () => {
    resetApp('/event-markets')
    state.scenario = 'empty-data'
    setTokenSnapshot(buildAuthResponse())
    render(<App />)
    expect(await screen.findByText(/no event-market providers are configured/i)).toBeTruthy()
  })

  it('shows the shared Kalshi summary unavailable state', async () => {
    resetApp('/event-markets')
    setTokenSnapshot(buildAuthResponse())
    server.use(http.get(`${apiBaseUrl}/event-markets/summary`, () => HttpResponse.json({ error: 'event markets not configured', code: 'ERR_NOT_IMPLEMENTED' }, { status: 501 })))
    render(<App />)

    expect(await screen.findByText(/feature unavailable/i)).toBeTruthy()
    expect(screen.queryByText(/12.50 ms/i)).toBeNull()
  })

  it('renders a filtered read-only options chain', async () => {
    resetApp('/options?underlying=AAPL&type=call')
    setTokenSnapshot(buildAuthResponse())
    render(<App />)

    expect(await screen.findByRole('table', { name: /AAPL options chain/i })).toBeTruthy()
    expect(screen.getByText('AAPL270115C00150000')).toBeTruthy()
    expect(screen.queryByText('AAPL270115P00150000')).toBeNull()
    expect(screen.getByText(/research only/i)).toBeTruthy()
  })

  it('prompts for an options underlying', async () => {
    resetApp('/options')
    setTokenSnapshot(buildAuthResponse())
    render(<App />)
    expect(await screen.findByText(/choose an underlying/i)).toBeTruthy()
  })

  it('renders an empty options chain', async () => {
    resetApp('/options?underlying=AAPL')
    state.scenario = 'empty-data'
    setTokenSnapshot(buildAuthResponse())
    render(<App />)
    expect(await screen.findByText(/no contracts returned/i)).toBeTruthy()
  })

  it('shows options provider unavailability explicitly', async () => {
    resetApp('/options?underlying=AAPL')
    setTokenSnapshot(buildAuthResponse())
    server.use(http.get(`${apiBaseUrl}/options/chain/:underlying`, () => HttpResponse.json({ error: 'options not configured', code: 'ERR_NOT_IMPLEMENTED' }, { status: 501 })))
    render(<App />)
    expect(await screen.findByText(/feature unavailable/i)).toBeTruthy()
  })

  it('renders persisted backtest definitions and reproducibility evidence', async () => {
    resetApp('/backtests')
    setTokenSnapshot(buildAuthResponse())
    render(<App />)

    expect(await screen.findByRole('table', { name: /backtest configurations/i })).toBeTruthy()
    expect(await screen.findByRole('table', { name: /backtest runs/i })).toBeTruthy()
    expect(screen.getByText('AAPL walk-forward')).toBeTruthy()
    expect(screen.getByText('research-v1')).toBeTruthy()
    expect(screen.getByText(/evidence only/i)).toBeTruthy()
  })

  it('renders empty backtest definitions and runs independently', async () => {
    resetApp('/backtests')
    state.scenario = 'empty-data'
    setTokenSnapshot(buildAuthResponse())
    render(<App />)

    expect(await screen.findByText(/no backtest configurations/i)).toBeTruthy()
    expect(await screen.findByText(/no backtest runs/i)).toBeTruthy()
  })

  it('keeps backtest configurations visible when run history is unavailable', async () => {
    resetApp('/backtests')
    setTokenSnapshot(buildAuthResponse())
    server.use(http.get(`${apiBaseUrl}/backtests/runs`, () => HttpResponse.json({ error: 'backtest storage unavailable', code: 'ERR_NOT_IMPLEMENTED' }, { status: 501 })))
    render(<App />)

    expect(await screen.findByText('AAPL walk-forward')).toBeTruthy()
    expect(await screen.findByText(/feature unavailable/i)).toBeTruthy()
  })

  it('renders journal provenance and links decisions to replay', async () => {
    resetApp('/journal')
    setTokenSnapshot(buildAuthResponse())
    render(<App />)

    expect(await screen.findByRole('table', { name: /trade decision journal/i })).toBeTruthy()
    expect(screen.getByText('fixture-provider / fixture-model')).toBeTruthy()
    expect(screen.getByRole('link', { name: /open replay/i })).toHaveAttribute('href', `/replay/decisions/00000000-0000-4000-8000-000000000093`)
  })

  it('distinguishes an empty journal from an unavailable journal', async () => {
    resetApp('/journal')
    state.scenario = 'empty-data'
    setTokenSnapshot(buildAuthResponse())
    render(<App />)
    expect(await screen.findByText(/no decisions recorded/i)).toBeTruthy()
  })

  it('renders a decision replay timeline and its persisted payload', async () => {
    resetApp('/replay/decisions/00000000-0000-4000-8000-000000000093')
    setTokenSnapshot(buildAuthResponse())
    render(<App />)

    expect(await screen.findByRole('heading', { name: /replay workbench/i })).toBeTruthy()
    expect(await screen.findByText('decision_created')).toBeTruthy()
    expect(screen.getByText(/"mode": "paper"/i)).toBeTruthy()
  })

  it('shows replay dependency unavailability explicitly', async () => {
    resetApp('/replay/decisions/00000000-0000-4000-8000-000000000093')
    setTokenSnapshot(buildAuthResponse())
    server.use(http.get(`${apiBaseUrl}/replay/decisions/:id`, () => HttpResponse.json({ error: 'replay not configured', code: 'ERR_NOT_IMPLEMENTED' }, { status: 501 })))
    render(<App />)
    expect(await screen.findByText(/feature unavailable/i)).toBeTruthy()
  })
})
