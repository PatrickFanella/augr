import { setupServer } from 'msw/node'
import { afterAll, afterEach, beforeAll, describe, expect, it } from 'vitest'

import { mockAccessToken, mockRefreshToken } from '@/test/fixtures'
import { createP0RestHandlers } from '@/test/mocks/rest'
import { createMockScenarioState } from '@/test/mocks/scenarios'

const apiBaseUrl = 'http://localhost/api/v1'
const state = createMockScenarioState('success')
const server = setupServer(...createP0RestHandlers({ apiBaseUrl, state }))

async function json(response: Response) {
  return (await response.json()) as unknown
}

describe('P0 REST mock handlers', () => {
  beforeAll(() => server.listen({ onUnhandledRequest: 'error' }))
  afterEach(() => {
    state.scenario = 'success'
    state.delayMs = 0
    server.resetHandlers()
  })
  afterAll(() => server.close())

  it('returns successful login and current user', async () => {
    const login = await fetch(`${apiBaseUrl}/auth/login`, {
      method: 'POST',
      body: JSON.stringify({ username: 'operator', password: 'password' }),
    })
    expect(login.status).toBe(200)
    expect(await json(login)).toHaveProperty('access_token')

    const me = await fetch(`${apiBaseUrl}/me`, { headers: { authorization: `Bearer ${mockAccessToken}` } })
    expect(me.status).toBe(200)
    expect(await json(me)).toHaveProperty('username', 'dev-paper-operator')
  })

  it('mocks invalid credentials', async () => {
    state.scenario = 'invalid-credentials'
    const response = await fetch(`${apiBaseUrl}/auth/login`, { method: 'POST', body: JSON.stringify({ username: 'invalid', password: 'bad' }) })
    expect(response.status).toBe(401)
    expect(await json(response)).toEqual({ error: 'invalid username or password', code: 'ERR_UNAUTHORIZED' })
  })

  it('mocks refresh success and failure', async () => {
    const ok = await fetch(`${apiBaseUrl}/auth/refresh`, { method: 'POST', body: JSON.stringify({ refresh_token: mockRefreshToken }) })
    expect(ok.status).toBe(200)

    state.scenario = 'failed-refresh'
    const failed = await fetch(`${apiBaseUrl}/auth/refresh`, { method: 'POST', body: JSON.stringify({ refresh_token: mockRefreshToken }) })
    expect(failed.status).toBe(401)
  })

  it('mocks empty running runs', async () => {
    state.scenario = 'empty-data'
    const response = await fetch(`${apiBaseUrl}/runs?status=running`, { headers: { authorization: `Bearer ${mockAccessToken}` } })
    expect(response.status).toBe(200)
    expect(await json(response)).toMatchObject({ data: [], total: 0, limit: 50, offset: 0 })
  })

  it.each([
    ['unauthorized', 401, 'ERR_UNAUTHORIZED'],
    ['conflict', 409, 'ERR_CONFLICT'],
    ['validation-error', 422, 'ERR_VALIDATION'],
    ['rate-limited', 429, 'ERR_RATE_LIMITED'],
    ['server-error', 500, 'ERR_INTERNAL'],
    ['not-implemented', 501, 'ERR_NOT_IMPLEMENTED'],
  ] as const)('mocks %s errors', async (scenario, status, code) => {
    state.scenario = scenario
    const response = await fetch(`${apiBaseUrl}/risk/status`, { headers: { authorization: `Bearer ${mockAccessToken}` } })
    expect(response.status).toBe(status)
    expect(await json(response)).toHaveProperty('code', code)
  })

  it('mocks partial service failure without failing the whole shell', async () => {
    state.scenario = 'partial-service-failure'
    const settings = await fetch(`${apiBaseUrl}/settings`, { headers: { authorization: `Bearer ${mockAccessToken}` } })
    expect(settings.status).toBe(200)
    expect(await json(settings)).toMatchObject({ system: { schema_status: 'degraded' } })

    const automation = await fetch(`${apiBaseUrl}/automation/health`, { headers: { authorization: `Bearer ${mockAccessToken}` } })
    expect(await json(automation)).toMatchObject({ healthy: false, failing_jobs: 1 })
  })
})
