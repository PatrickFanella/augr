export type MockScenario =
  | 'success'
  | 'invalid-credentials'
  | 'expired-access-token'
  | 'failed-refresh'
  | 'empty-data'
  | 'slow-response'
  | 'unauthorized'
  | 'conflict'
  | 'validation-error'
  | 'rate-limited'
  | 'server-error'
  | 'not-implemented'
  | 'partial-service-failure'

export type MockScenarioState = {
  scenario: MockScenario
  delayMs: number
}

export function createMockScenarioState(initial: MockScenario = 'success'): MockScenarioState {
  return { scenario: initial, delayMs: initial === 'slow-response' ? 750 : 0 }
}
