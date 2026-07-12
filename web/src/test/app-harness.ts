import { cleanup, configure } from '@testing-library/react'
import { setupServer } from 'msw/node'
import { afterAll, afterEach, beforeAll, vi } from 'vitest'

import { configureApiClient } from '@/shared/api/client'
import { clearTokenSnapshot } from '@/shared/auth/tokenStore'
import { fixtureDate, fixtureId } from '@/test/fixtures'
import { createP0RestHandlers } from '@/test/mocks/rest'
import { createMockScenarioState } from '@/test/mocks/scenarios'

export const apiBaseUrl = '/api/v1'
export const strategyId = fixtureId(10)
export const state = createMockScenarioState('success')
export const server = setupServer(...createP0RestHandlers({ apiBaseUrl, state }))

// Route components are lazy-loaded in production. Give async DOM queries enough
// time to include the dynamic import without requiring per-test timeout tuning.
configure({ asyncUtilTimeout: 5_000 })

type Listener = (event: { data: string }) => void

export class FakeWebSocket {
  static instances: FakeWebSocket[] = []
  readyState = 0
  sent: string[] = []
  onopen: (() => void) | null = null
  onclose: (() => void) | null = null
  onerror: (() => void) | null = null
  onmessage: Listener | null = null

  readonly url: string

  constructor(url: string) {
    this.url = url
    FakeWebSocket.instances.push(this)
    queueMicrotask(() => {
      this.readyState = 1
      this.onopen?.()
    })
  }

  send(payload: string) {
    this.sent.push(payload)
  }

  close() {
    this.readyState = 3
    this.onclose?.()
  }

  emit(type = 'pipeline_start', data?: unknown) {
    this.onmessage?.({ data: JSON.stringify({ type, data, timestamp: fixtureDate }) })
  }
}

export function resetApp(path = '/login') {
  document.body.innerHTML = ''
  window.history.pushState({}, '', path)
  localStorage.clear()
  sessionStorage.clear()
  clearTokenSnapshot()
  FakeWebSocket.instances = []
  state.scenario = 'success'
  state.delayMs = 0
  configureApiClient({ baseUrl: apiBaseUrl })
}

export function installAppTestHarness() {
  beforeAll(() => {
    server.listen({ onUnhandledRequest: 'bypass' })
    vi.stubGlobal('WebSocket', FakeWebSocket)
  })
  afterEach(() => {
    cleanup()
    resetApp()
    server.resetHandlers()
    vi.useRealTimers()
  })
  afterAll(() => {
    server.close()
    vi.unstubAllGlobals()
  })
}
