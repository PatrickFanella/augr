import '@testing-library/jest-dom/vitest'
import { render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { http, HttpResponse } from 'msw'
import { describe, expect, it } from 'vitest'

import App from '@/App'
import { setTokenSnapshot } from '@/shared/auth/tokenStore'
import { buildAuthResponse } from '@/test/fixtures'
import { apiBaseUrl, installAppTestHarness, resetApp, server, state } from '@/test/app-harness'

const assessmentId = '00000000-0000-4000-8000-0000000000c1'
const transactionId = '00000000-0000-4000-8000-0000000000c2'

describe('total-overhaul operator workspace', () => {
  installAppTestHarness()

  it('renders truthful readiness, adoption gates, and exact economic projections', async () => {
    resetApp('/overhaul')
    setTokenSnapshot(buildAuthResponse())
    render(<App />)

    expect(await screen.findByRole('heading', { name: /^capital & evidence$/i })).toBeTruthy()
    expect(await screen.findByText(/paper release gates are ready/i)).toBeTruthy()
    expect(screen.getByText(/live disabled/i)).toBeTruthy()
    expect(screen.getByText('R0')).toBeTruthy()
    expect(screen.getByText('R6')).toBeTruthy()
    expect(await screen.findByRole('list', { name: /economic accounts/i })).toBeTruthy()
    expect(await screen.findByText('USD 515.00000000')).toBeTruthy()
    expect(screen.getByText(/promotion eligible evidence/i)).toBeTruthy()
    expect(screen.getByText(/inspection is not activation/i)).toBeTruthy()
    expect(screen.getAllByText(/read only/i).length).toBeGreaterThan(0)
  })

  it('validates lookup IDs and reconstructs milestone and balanced-ledger evidence independently', async () => {
    resetApp('/overhaul')
    setTokenSnapshot(buildAuthResponse())
    render(<App />)
    await screen.findByRole('heading', { name: /^capital & evidence$/i })

    const assessmentInput = screen.getByLabelText(/assessment uuid/i)
    await userEvent.type(assessmentInput, 'not-an-id')
    expect(screen.getByText(/enter a valid uuid/i)).toBeTruthy()
    expect(screen.getByRole('button', { name: /inspect/i })).toBeDisabled()
    await userEvent.clear(assessmentInput)
    await userEvent.type(assessmentInput, assessmentId)
    await userEvent.click(screen.getByRole('button', { name: /inspect/i }))
    expect(await screen.findByText('fixture-shadow-campaign')).toBeTruthy()
    expect(screen.getByText(/30 elapsed days are required/i)).toBeTruthy()

    const ledgerInput = screen.getByLabelText(/transaction uuid/i)
    await userEvent.type(ledgerInput, transactionId)
    await userEvent.click(screen.getByRole('button', { name: /^trace$/i }))
    const ledgerPanel = screen.getByRole('heading', { name: /ledger trace/i }).closest('section') as HTMLElement
    expect(await within(ledgerPanel).findByText('capital.deposit')).toBeTruthy()
    expect(within(ledgerPanel).getByText('-25.00000000')).toBeTruthy()
  })

  it('distinguishes an empty account projection from a disabled runtime gate', async () => {
    resetApp('/overhaul')
    state.scenario = 'empty-data'
    setTokenSnapshot(buildAuthResponse())
    render(<App />)
    expect(await screen.findByText(/no economic accounts/i)).toBeTruthy()

    resetApp('/overhaul')
    setTokenSnapshot(buildAuthResponse())
    server.use(http.get(`${apiBaseUrl}/economic/accounts`, () => HttpResponse.json({ error: 'economic account reads are disabled', code: 'ERR_NOT_IMPLEMENTED' }, { status: 501 })))
    render(<App />)
    expect(await screen.findByText(/read model is disabled/i)).toBeTruthy()
    expect(screen.getByText(/paper release gates are ready/i)).toBeTruthy()
  })
})
