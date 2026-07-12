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

  it('renders event-market paper readiness and feed status', async () => {
    resetApp('/event-markets')
    setTokenSnapshot(buildAuthResponse())
    render(<App />)

    expect(await screen.findByRole('heading', { name: /^event markets$/i })).toBeTruthy()
    expect(await screen.findByRole('table', { name: /event market providers/i })).toBeTruthy()
    expect(await screen.findByText('kalshi')).toBeTruthy()
    expect(screen.getByText('polymarket')).toBeTruthy()
    expect(screen.getAllByText(/not ready/i)).toHaveLength(2)
    expect(screen.getByText(/12.50 ms/i)).toBeTruthy()
  })

  it('renders event-market empty and feature-unavailable states', async () => {
    resetApp('/event-markets')
    state.scenario = 'empty-data'
    setTokenSnapshot(buildAuthResponse())
    render(<App />)
    expect(await screen.findByText(/no event-market providers are configured/i)).toBeTruthy()
  })

  it('keeps feed data visible when the shared summary is unavailable', async () => {
    resetApp('/event-markets')
    setTokenSnapshot(buildAuthResponse())
    server.use(http.get(`${apiBaseUrl}/event-markets/summary`, () => HttpResponse.json({ error: 'event markets not configured', code: 'ERR_NOT_IMPLEMENTED' }, { status: 501 })))
    render(<App />)

    expect(await screen.findByText(/feature unavailable/i)).toBeTruthy()
    expect(await screen.findByText(/12.50 ms/i)).toBeTruthy()
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
})
