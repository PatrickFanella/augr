import { delay, http, HttpResponse } from 'msw'

import type { ApiError } from '@/shared/types/api'
import type { LoginRequest, RefreshRequest } from '@/shared/types/auth'
import { expiredAccessToken, mockAccessToken, mockRefreshToken } from '@/test/fixtures/builders'
import { fixtureDate, fixtureId } from '@/test/fixtures/ids'
import {
  buildAuthResponse,
  buildAgentDecision,
  buildAgentEvent,
  buildAllocationDecision,
  buildAllocatorDiagnostics,
  buildAllocatorOpportunity,
  buildAllocatorSummary,
  buildAutomationHealth,
  buildAutomationJobRun,
  buildAutomationJobStatus,
  buildLatestReport,
  buildOrder,
  buildPortfolioSummary,
  buildPosition,
  buildReportArtifact,
  buildRiskBreakers,
  buildRiskCockpit,
  buildRiskStatus,
  buildRun,
  buildRunSnapshot,
  buildSettings,
  buildStrategy,
  buildTrade,
  buildUser,
} from '@/test/fixtures/builders'
import type { MockScenarioState } from '@/test/mocks/scenarios'
import { createMockScenarioState } from '@/test/mocks/scenarios'

export type P0MockHandlersOptions = {
  apiBaseUrl?: string
  state?: MockScenarioState
}

const defaultApiBaseUrl = '/api/v1'
const knownMarketTypes = ['stock', 'crypto', 'polymarket', 'kalshi', 'options'] as const

function endpoint(apiBaseUrl: string, path: string) {
  return `${apiBaseUrl}${path}`
}

function errorJson(status: number, error: string, code: ApiError['code']) {
  return HttpResponse.json({ error, code } satisfies ApiError, { status })
}

async function applyScenarioDelay(state: MockScenarioState) {
  if (state.scenario === 'slow-response' || state.delayMs > 0) {
    await delay(state.delayMs || 750)
  }
}

function scenarioError(state: MockScenarioState): Response | undefined {
  switch (state.scenario) {
    case 'unauthorized':
    case 'expired-access-token':
      return errorJson(401, 'not authenticated', 'ERR_UNAUTHORIZED')
    case 'conflict':
      return errorJson(409, 'mock conflict', 'ERR_CONFLICT')
    case 'validation-error':
      return errorJson(422, 'mock validation failed', 'ERR_VALIDATION')
    case 'rate-limited':
      return errorJson(429, 'rate limit exceeded', 'ERR_RATE_LIMITED')
    case 'server-error':
      return errorJson(500, 'mock internal error', 'ERR_INTERNAL')
    case 'not-implemented':
      return errorJson(501, 'mock feature not configured', 'ERR_NOT_IMPLEMENTED')
    default:
      return undefined
  }
}

function hasValidBearer(request: Request) {
  const auth = request.headers.get('authorization')
  return auth === `Bearer ${mockAccessToken}` || auth === 'Bearer dev-paper-access-token-refreshed'
}

function authGuard(request: Request, state: MockScenarioState): Response | undefined {
  if (state.scenario === 'expired-access-token') {
    return errorJson(401, 'access token expired', 'ERR_UNAUTHORIZED')
  }
  if (request.headers.get('authorization') === `Bearer ${expiredAccessToken}`) {
    return errorJson(401, 'access token expired', 'ERR_UNAUTHORIZED')
  }
  if (!hasValidBearer(request)) {
    return errorJson(401, 'not authenticated', 'ERR_UNAUTHORIZED')
  }
  return undefined
}

export function createP0RestHandlers(options: P0MockHandlersOptions = {}) {
  const apiBaseUrl = options.apiBaseUrl ?? defaultApiBaseUrl
  const state = options.state ?? createMockScenarioState()
  let killSwitchActive = false
  let killSwitchReason: string | undefined
  const marketKillSwitches: Record<string, { active: boolean; reason?: string }> = {}
  let trippedBreakers = buildRiskBreakers().tripped

  return [
    http.post(endpoint(apiBaseUrl, '/auth/login'), async ({ request }) => {
      await applyScenarioDelay(state)
      const body = (await request.json().catch(() => ({}))) as Partial<LoginRequest>
      if (state.scenario === 'invalid-credentials' || body.username === 'invalid') {
        return errorJson(401, 'invalid username or password', 'ERR_UNAUTHORIZED')
      }
      const error = scenarioError(state)
      if (error && state.scenario !== 'expired-access-token') return error
      if (!body.username || !body.password) {
        return errorJson(400, 'username and password are required', 'ERR_VALIDATION')
      }
      return HttpResponse.json(buildAuthResponse())
    }),

    http.post(endpoint(apiBaseUrl, '/auth/refresh'), async ({ request }) => {
      await applyScenarioDelay(state)
      const body = (await request.json().catch(() => ({}))) as Partial<RefreshRequest>
      if (state.scenario === 'failed-refresh' || body.refresh_token !== mockRefreshToken) {
        return errorJson(401, 'invalid or expired refresh token', 'ERR_UNAUTHORIZED')
      }
      return HttpResponse.json(buildAuthResponse({ access_token: 'dev-paper-access-token-refreshed' }))
    }),

    http.get(endpoint(apiBaseUrl, '/me'), async ({ request }) => {
      await applyScenarioDelay(state)
      const authError = authGuard(request, state)
      if (authError) return authError
      const error = scenarioError(state)
      if (error) return error
      return HttpResponse.json(buildUser())
    }),

    http.get(endpoint(apiBaseUrl, '/settings'), async ({ request }) => {
      await applyScenarioDelay(state)
      const authError = authGuard(request, state)
      if (authError) return authError
      const error = scenarioError(state)
      if (error) return error
      if (state.scenario === 'partial-service-failure') {
        return HttpResponse.json(
          buildSettings({
            system: {
              ...buildSettings().system,
              schema_status: 'degraded',
              connected_brokers: [{ name: 'paper-broker', paper_mode: true, configured: false }],
            },
          }),
        )
      }
      return HttpResponse.json(buildSettings())
    }),

    http.get(endpoint(apiBaseUrl, '/event-markets/summary'), async ({ request }) => {
      await applyScenarioDelay(state)
      const authError = authGuard(request, state)
      if (authError) return authError
      const error = scenarioError(state)
      if (error) return error
      if (state.scenario === 'empty-data') return HttpResponse.json({ providers: [] })
      return HttpResponse.json({ providers: [{ provider: 'kalshi', watched_markets: 4, active_paper: 2, last_run_status: 'completed', live_trading_ready: false }, { provider: 'polymarket', watched_markets: 3, active_paper: 1, last_run_status: state.scenario === 'partial-service-failure' ? 'new_discovery_status' : 'running', live_trading_ready: false }] })
    }),

    http.get(endpoint(apiBaseUrl, '/marketdata/polymarket/status'), async ({ request }) => {
      await applyScenarioDelay(state)
      const authError = authGuard(request, state)
      if (authError) return authError
      const error = scenarioError(state)
      if (error) return error
      return HttpResponse.json({ enabled: true, ws_connections: 2, avg_jitter_ms: 12.5, dropped: 0, ready_slugs: ['fixture-market'], recorder_lag_seconds: 0.5, updated_at: fixtureDate })
    }),

    http.get(endpoint(apiBaseUrl, '/options/chain/:underlying'), async ({ request, params }) => {
      await applyScenarioDelay(state)
      const authError = authGuard(request, state)
      if (authError) return authError
      const error = scenarioError(state)
      if (error) return error
      if (state.scenario === 'empty-data') return HttpResponse.json([])
      const url = new URL(request.url)
      const requestedType = url.searchParams.get('type')
      const contracts = ['call', 'put'].filter((optionType) => !requestedType || requestedType === optionType).map((optionType, index) => ({ contract: { occ_symbol: `${params.underlying}270115${optionType === 'call' ? 'C' : 'P'}00150000`, underlying: String(params.underlying), option_type: optionType, strike: 150, expiry: '2027-01-15T00:00:00Z', multiplier: 100, style: 'american' }, greeks: { delta: optionType === 'call' ? 0.4 : -0.4, gamma: 0.02, theta: -0.1, vega: 0.2, iv: 0.25 }, bid: 2 + index, ask: 4 + index, mid: 3 + index, last: 2.5 + index, volume: 12, open_interest: 30 }))
      return HttpResponse.json(contracts)
    }),

    http.get(endpoint(apiBaseUrl, '/events'), async ({ request }) => {
      await applyScenarioDelay(state)
      const authError = authGuard(request, state)
      if (authError) return authError
      const error = scenarioError(state)
      if (error) return error
      const url = new URL(request.url)
      const eventKind = url.searchParams.get('event_kind')
      const runId = url.searchParams.get('pipeline_run_id')
      const strategyId = url.searchParams.get('strategy_id')
      const agentRole = url.searchParams.get('agent_role')
      const limit = Number(url.searchParams.get('limit') ?? '20')
      const offset = Number(url.searchParams.get('offset') ?? '0')
      const all = state.scenario === 'empty-data'
        ? []
        : [
          buildAgentEvent({ id: '00000000-0000-4000-8000-000000000080', event_kind: state.scenario === 'partial-service-failure' ? 'new_event_kind' : 'agent_decision', title: state.scenario === 'partial-service-failure' ? 'Unsafe <script>alert(1)</script>' : 'Analyst decision recorded', metadata: state.scenario === 'partial-service-failure' ? { unsafe: '<script>alert(1)</script>', order_id: 'ORD-42' } : { signal: 'hold', order_id: '00000000-0000-4000-8000-000000000040' } }),
          buildAgentEvent({ id: '00000000-0000-4000-8000-000000000081', event_kind: 'debate_round', agent_role: 'critic', title: 'Critic challenged thesis', summary: 'Challenge recorded.', tags: ['debate'] }),
          buildAgentEvent({ id: '00000000-0000-4000-8000-000000000082', event_kind: 'signal', agent_role: 'risk', title: 'Risk signal reviewed', summary: 'Risk accepted paper exposure.' }),
        ]
      const filtered = all.filter((event) => {
        if (eventKind && event.event_kind !== eventKind) return false
        if (runId && event.pipeline_run_id !== runId) return false
        if (strategyId && event.strategy_id !== strategyId) return false
        if (agentRole && event.agent_role !== agentRole) return false
        return true
      })
      const page = filtered.slice(offset, offset + limit)
      const response: { data: ReturnType<typeof buildAgentEvent>[]; total?: number; limit: number; offset: number } = { data: page, total: filtered.length, limit, offset }
      if (state.scenario === 'partial-service-failure') delete response.total
      return HttpResponse.json(response)
    }),

    http.get(endpoint(apiBaseUrl, '/risk/status'), async ({ request }) => {
      await applyScenarioDelay(state)
      const authError = authGuard(request, state)
      if (authError) return authError
      const error = scenarioError(state)
      if (error) return error
      return HttpResponse.json(
        state.scenario === 'partial-service-failure'
          ? buildRiskStatus({ risk_status: 'warning', circuit_breaker: { state: 'new_breaker_state', reason: 'unsafe <script>alert(1)</script>' }, kill_switch: { active: false, reason: 'automation degraded' } })
          : buildRiskStatus({
            kill_switch: { active: killSwitchActive, reason: killSwitchReason, mechanisms: killSwitchActive ? ['api_toggle'] : undefined },
            market_kill_switches: marketKillSwitches,
          }),
      )
    }),

    http.post(endpoint(apiBaseUrl, '/risk/killswitch'), async ({ request }) => {
      await applyScenarioDelay(state)
      const authError = authGuard(request, state)
      if (authError) return authError
      const error = scenarioError(state)
      if (error) return error
      const body = await request.json().catch(() => ({})) as { active?: unknown; reason?: unknown }
      const reason = typeof body.reason === 'string' ? body.reason.trim() : ''
      if (!reason) return errorJson(400, 'reason is required for kill switch changes', 'ERR_VALIDATION')
      if (body.active === false && request.headers.get('x-admin-key') !== 'test-admin-key') return errorJson(401, 'admin key required', 'ERR_UNAUTHORIZED')
      killSwitchActive = body.active === true
      killSwitchReason = reason
      return HttpResponse.json({ active: killSwitchActive, reason, mechanisms: killSwitchActive ? ['api_toggle'] : undefined, updated_at: fixtureDate })
    }),

    http.post(endpoint(apiBaseUrl, '/risk/market/:type/stop'), async ({ request, params }) => {
      await applyScenarioDelay(state)
      const authError = authGuard(request, state)
      if (authError) return authError
      const error = scenarioError(state)
      if (error) return error
      const marketType = String(params.type)
      if (!knownMarketTypes.includes(marketType as (typeof knownMarketTypes)[number])) return errorJson(400, 'unknown market type', 'ERR_VALIDATION')
      const body = await request.json().catch(() => ({})) as { reason?: unknown }
      const reason = typeof body.reason === 'string' ? body.reason.trim() : ''
      if (!reason) return errorJson(400, 'reason is required for market stop', 'ERR_VALIDATION')
      marketKillSwitches[marketType] = { active: true, reason }
      return HttpResponse.json({ market_type: marketType, active: true })
    }),

    http.post(endpoint(apiBaseUrl, '/risk/market/:type/resume'), async ({ request, params }) => {
      await applyScenarioDelay(state)
      const authError = authGuard(request, state)
      if (authError) return authError
      const error = scenarioError(state)
      if (error) return error
      const marketType = String(params.type)
      if (!knownMarketTypes.includes(marketType as (typeof knownMarketTypes)[number])) return errorJson(400, 'unknown market type', 'ERR_VALIDATION')
      marketKillSwitches[marketType] = { active: false }
      return HttpResponse.json({ market_type: marketType, active: false })
    }),

    http.get(endpoint(apiBaseUrl, '/risk/cockpit'), async ({ request }) => {
      await applyScenarioDelay(state)
      const authError = authGuard(request, state)
      if (authError) return authError
      const error = scenarioError(state)
      if (error) return error
      if (state.scenario === 'empty-data') return HttpResponse.json(buildRiskCockpit({ exposures: [], warnings: [] }))
      if (state.scenario === 'partial-service-failure') return HttpResponse.json(buildRiskCockpit({ exposures: [{ market_type: 'new_market', open_positions: 1, approved_decisions: 1, rejected_decisions: 1, gross_exposure: 0.11, net_expected_value: -5 }], warnings: ['unsafe <script>alert(1)</script>'] }))
      return HttpResponse.json(buildRiskCockpit())
    }),

    http.get(endpoint(apiBaseUrl, '/risk/breakers'), async ({ request }) => {
      await applyScenarioDelay(state)
      const authError = authGuard(request, state)
      if (authError) return authError
      const error = scenarioError(state)
      if (error) return error
      if (state.scenario === 'empty-data') return HttpResponse.json(buildRiskBreakers({ tripped: [] }))
      if (state.scenario === 'partial-service-failure') return HttpResponse.json(buildRiskBreakers({ tripped: [{ scope: 'strategy:unsafe-<script>alert(1)</script>', tripped_at: '2026-01-15T12:00:00.000Z', reason: 'new breaker reason <script>alert(1)</script>' }] }))
      return HttpResponse.json(buildRiskBreakers({ tripped: trippedBreakers }))
    }),

    http.post(endpoint(apiBaseUrl, '/risk/breaker/reset'), async ({ request }) => {
      await applyScenarioDelay(state)
      const authError = authGuard(request, state)
      if (authError) return authError
      const error = scenarioError(state)
      if (error) return error
      if (request.headers.get('x-admin-key') !== 'test-admin-key') return errorJson(401, 'admin key required', 'ERR_UNAUTHORIZED')
      const body = await request.json().catch(() => ({})) as { scope?: unknown }
      const scope = typeof body.scope === 'string' ? body.scope.trim() : ''
      if (!scope) return errorJson(400, 'missing_scope', 'ERR_VALIDATION')
      trippedBreakers = trippedBreakers.filter((breaker) => breaker.scope !== scope)
      return HttpResponse.json({ scope, reset: true })
    }),

    http.get(endpoint(apiBaseUrl, '/portfolio/summary'), async ({ request }) => {
      await applyScenarioDelay(state)
      const authError = authGuard(request, state)
      if (authError) return authError
      const error = scenarioError(state)
      if (error) return error
      return HttpResponse.json(state.scenario === 'empty-data' ? buildPortfolioSummary({ open_positions: 0, unrealized_pnl: 0 }) : buildPortfolioSummary())
    }),

    http.get(endpoint(apiBaseUrl, '/portfolio/positions/open'), async ({ request }) => {
      await applyScenarioDelay(state)
      const authError = authGuard(request, state)
      if (authError) return authError
      const error = scenarioError(state)
      if (error) return error
      const url = new URL(request.url)
      const ticker = url.searchParams.get('ticker')
      const side = url.searchParams.get('side')
      const limit = Number(url.searchParams.get('limit') ?? '20')
      const offset = Number(url.searchParams.get('offset') ?? '0')
      const all = state.scenario === 'empty-data'
        ? []
        : Array.from({ length: 24 }, (_, index) => buildPosition({
          id: `00000000-0000-4000-8000-${String(30 + index).padStart(12, '0')}`,
          ticker: index % 2 === 0 ? 'AUGR' : 'LIVE',
          side: state.scenario === 'partial-service-failure' && index === 0 ? 'mystery_side' : (index % 2 === 0 ? 'long' : 'short'),
          quantity: 10 + index,
          current_price: index % 3 === 0 ? undefined : 101.5 + index,
          unrealized_pnl: index % 3 === 0 ? undefined : 15 + index,
        }))
      const filtered = all.filter((position) => {
        if (ticker && position.ticker !== ticker) return false
        if (side && position.side !== side) return false
        return true
      })
      const page = filtered.slice(offset, offset + limit)
      const response: { data: ReturnType<typeof buildPosition>[]; total?: number; limit: number; offset: number } = { data: page, total: filtered.length, limit, offset }
      if (state.scenario === 'partial-service-failure') delete response.total
      return HttpResponse.json(response)
    }),

    http.get(endpoint(apiBaseUrl, '/portfolio/allocator/diagnostics'), async ({ request }) => {
      await applyScenarioDelay(state)
      const authError = authGuard(request, state)
      if (authError) return authError
      const error = scenarioError(state)
      if (error) return error
      if (state.scenario === 'partial-service-failure') {
        return HttpResponse.json(buildAllocatorDiagnostics({
          run_counts_by_signal: { hold: 4, new_signal: 1 },
          active_strategies_by_market: { stock: 2, new_market: 1 },
          warnings: ['account_balance_unavailable', 'new_backend_warning_<script>alert(1)</script>'],
        }))
      }
      return HttpResponse.json(buildAllocatorDiagnostics())
    }),

    http.get(endpoint(apiBaseUrl, '/portfolio/allocator/summary'), async ({ request }) => {
      await applyScenarioDelay(state)
      const authError = authGuard(request, state)
      if (authError) return authError
      const error = scenarioError(state)
      if (error) return error
      if (state.scenario === 'empty-data') return HttpResponse.json(buildAllocatorSummary({ opportunity_counts_by_status: {}, recent_decisions: [], warnings: [] }))
      if (state.scenario === 'partial-service-failure') return HttpResponse.json(buildAllocatorSummary({ opportunity_counts_by_status: { queued: 1, new_status: 1 }, warnings: ['allocation_decisions_unavailable'] }))
      return HttpResponse.json(buildAllocatorSummary())
    }),

    http.get(endpoint(apiBaseUrl, '/portfolio/allocator/opportunities'), async ({ request }) => {
      await applyScenarioDelay(state)
      const authError = authGuard(request, state)
      if (authError) return authError
      const error = scenarioError(state)
      if (error) return error
      const url = new URL(request.url)
      const status = url.searchParams.get('status')
      const marketType = url.searchParams.get('market_type')
      const ticker = url.searchParams.get('ticker')
      const strategyId = url.searchParams.get('strategy_id')
      const limit = Number(url.searchParams.get('limit') ?? '10')
      const offset = Number(url.searchParams.get('offset') ?? '0')
      const all = state.scenario === 'empty-data'
        ? []
        : Array.from({ length: 12 }, (_, index) => buildAllocatorOpportunity({
          id: `00000000-0000-4000-8000-${String(90 + index).padStart(12, '0')}`,
          ticker: index % 2 === 0 ? 'AUGR' : 'LIVE',
          market_type: state.scenario === 'partial-service-failure' && index === 0 ? 'new_market' : (index % 2 === 0 ? 'stock' : 'polymarket'),
          status: state.scenario === 'partial-service-failure' && index === 0 ? 'new_opportunity_status' : (index % 3 === 0 ? 'queued' : index % 3 === 1 ? 'selected' : 'rejected'),
          evidence: state.scenario === 'partial-service-failure' && index === 0 ? { unsafe: '<script>alert(1)</script>' } : { model_score: 0.7 + index / 100 },
        }))
      const filtered = all.filter((opportunity) => {
        if (status && opportunity.status !== status) return false
        if (marketType && opportunity.market_type !== marketType) return false
        if (ticker && opportunity.ticker !== ticker) return false
        if (strategyId && opportunity.strategy_id !== strategyId) return false
        return true
      })
      const page = filtered.slice(offset, offset + limit)
      const response: { data: ReturnType<typeof buildAllocatorOpportunity>[]; total?: number; limit: number; offset: number } = { data: page, total: filtered.length, limit, offset }
      if (state.scenario === 'partial-service-failure') delete response.total
      return HttpResponse.json(response)
    }),

    http.get(endpoint(apiBaseUrl, '/portfolio/allocator/decisions'), async ({ request }) => {
      await applyScenarioDelay(state)
      const authError = authGuard(request, state)
      if (authError) return authError
      const error = scenarioError(state)
      if (error) return error
      const url = new URL(request.url)
      const mode = url.searchParams.get('mode')
      const action = url.searchParams.get('action')
      const strategyId = url.searchParams.get('strategy_id')
      const limit = Number(url.searchParams.get('limit') ?? '10')
      const offset = Number(url.searchParams.get('offset') ?? '0')
      const all = state.scenario === 'empty-data'
        ? []
        : Array.from({ length: 8 }, (_, index) => buildAllocationDecision({
          id: `00000000-0000-4000-8000-${String(100 + index).padStart(12, '0')}`,
          mode: state.scenario === 'partial-service-failure' && index === 0 ? 'new_mode' : (index % 2 === 0 ? 'shadow' : 'paper'),
          action: state.scenario === 'partial-service-failure' && index === 0 ? 'new_action' : (index % 2 === 0 ? 'select' : 'reject'),
          reasons: state.scenario === 'partial-service-failure' && index === 0 ? ['unsafe <script>alert(1)</script>'] : ['paper-only fixture'],
        }))
      const filtered = all.filter((decision) => {
        if (mode && decision.mode !== mode) return false
        if (action && decision.action !== action) return false
        if (strategyId && decision.strategy_id !== strategyId) return false
        return true
      })
      const page = filtered.slice(offset, offset + limit)
      const response: { data: ReturnType<typeof buildAllocationDecision>[]; total?: number; limit: number; offset: number } = { data: page, total: filtered.length, limit, offset }
      if (state.scenario === 'partial-service-failure') delete response.total
      return HttpResponse.json(response)
    }),

    http.get(endpoint(apiBaseUrl, '/runs/:id/snapshot'), async ({ request, params }) => {
      await applyScenarioDelay(state)
      const authError = authGuard(request, state)
      if (authError) return authError
      const error = scenarioError(state)
      if (error) return error
      if (params.id === '00000000-0000-4000-8000-000000000999') return errorJson(404, 'run not found', 'ERR_NOT_FOUND')
      if (state.scenario === 'empty-data') return HttpResponse.json({})
      if (state.scenario === 'partial-service-failure') {
        return HttpResponse.json(buildRunSnapshot({
          huge_payload: { rows: Array.from({ length: 30 }, (_, index) => ({ index, value: `fixture-${index}` })) },
          unknown_backend_shape: { unsafe: '<script>alert(1)</script>', password: 'fixture-password-should-redact' },
        }))
      }
      return HttpResponse.json(buildRunSnapshot())
    }),

    http.get(endpoint(apiBaseUrl, '/runs/:id'), async ({ request, params }) => {
      await applyScenarioDelay(state)
      const authError = authGuard(request, state)
      if (authError) return authError
      const error = scenarioError(state)
      if (error) return error
      if (params.id === '00000000-0000-4000-8000-000000000999') return errorJson(404, 'run not found', 'ERR_NOT_FOUND')
      if (state.scenario === 'partial-service-failure') {
        return HttpResponse.json(buildRun({
          id: String(params.id),
          status: 'new_run_status',
          signal: 'unknown_signal',
          error_message: 'fixture failure <script>alert(1)</script>',
          config_snapshot: { unsafe: '<script>alert(1)</script>', nested: { threshold: 0.42 } },
          phase_timings: { unknown_phase_ms: 123 },
        }))
      }
      if (params.id === '00000000-0000-4000-8000-000000000022') {
        return HttpResponse.json(buildRun({ id: String(params.id), status: 'failed', signal: 'sell', error_message: 'fixture run failed', completed_at: '2026-01-15T12:10:00.000Z' }))
      }
      return HttpResponse.json(buildRun({ id: String(params.id) }))
    }),

    http.get(endpoint(apiBaseUrl, '/runs/:id/decisions'), async ({ request, params }) => {
      await applyScenarioDelay(state)
      const authError = authGuard(request, state)
      if (authError) return authError
      const error = scenarioError(state)
      if (error) return error
      const url = new URL(request.url)
      const includePrompt = url.searchParams.get('include_prompt') === 'true'
      const agentRole = url.searchParams.get('agent_role')
      const phase = url.searchParams.get('phase')
      const limit = Number(url.searchParams.get('limit') ?? '10')
      const offset = Number(url.searchParams.get('offset') ?? '0')
      const runId = String(params.id)
      const all = state.scenario === 'empty-data'
        ? []
        : [
          buildAgentDecision({ id: '00000000-0000-4000-8000-000000000070', pipeline_run_id: runId, agent_role: state.scenario === 'partial-service-failure' ? 'new_agent_role' : 'analyst', phase: 'signal_generation', prompt_text: includePrompt ? 'Fixture prompt <script>alert(1)</script>' : undefined }),
          buildAgentDecision({ id: '00000000-0000-4000-8000-000000000071', pipeline_run_id: runId, agent_role: 'risk', phase: 'risk_debate', round_number: 2, output_text: 'Risk accepts paper-only exposure.', output_structured: { risk: 'normal' }, prompt_text: includePrompt ? 'Risk prompt' : undefined }),
          buildAgentDecision({ id: '00000000-0000-4000-8000-000000000072', pipeline_run_id: runId, agent_role: 'critic', phase: 'debate', round_number: 3, output_text: 'Challenge signal confidence.', output_structured: state.scenario === 'partial-service-failure' ? { unsafe: '<script>alert(1)</script>' } : { challenge: true }, prompt_text: includePrompt ? 'Critic prompt' : undefined }),
        ]
      const filtered = all.filter((decision) => {
        if (agentRole && decision.agent_role !== agentRole) return false
        if (phase && decision.phase !== phase) return false
        return true
      })
      const page = filtered.slice(offset, offset + limit)
      return HttpResponse.json({ data: page, total: filtered.length, limit, offset })
    }),

    http.get(endpoint(apiBaseUrl, '/runs'), async ({ request }) => {
      await applyScenarioDelay(state)
      const authError = authGuard(request, state)
      if (authError) return authError
      const error = scenarioError(state)
      if (error) return error
      const url = new URL(request.url)
      const status = url.searchParams.get('status')
      const strategyId = url.searchParams.get('strategy_id')
      const ticker = url.searchParams.get('ticker')
      const limit = Number(url.searchParams.get('limit') ?? '50')
      const offset = Number(url.searchParams.get('offset') ?? '0')
      const all = state.scenario === 'empty-data'
        ? []
        : Array.from({ length: 24 }, (_, index) => buildRun({
          id: `00000000-0000-4000-8000-${String(20 + index).padStart(12, '0')}`,
          strategy_id: index % 2 === 0 ? '00000000-0000-4000-8000-000000000010' : '00000000-0000-4000-8000-000000000011',
          ticker: index % 2 === 0 ? 'AUGR' : 'LIVE',
          status: state.scenario === 'partial-service-failure' && index === 0 ? 'new_run_status' : (['running', 'completed', 'failed', 'cancelled'] as const)[index % 4],
          signal: index % 3 === 0 ? 'buy' : index % 3 === 1 ? 'sell' : 'hold',
          completed_at: index % 4 === 0 ? undefined : '2026-01-15T12:10:00.000Z',
        }))
      const filtered = all.filter((run) => {
        if (status && run.status !== status) return false
        if (strategyId && run.strategy_id !== strategyId) return false
        if (ticker && run.ticker !== ticker) return false
        return true
      })
      const page = filtered.slice(offset, offset + limit)
      const response: { data: ReturnType<typeof buildRun>[]; total?: number; limit: number; offset: number } = { data: page, total: filtered.length, limit, offset }
      if (state.scenario === 'partial-service-failure') delete response.total
      return HttpResponse.json(response)
    }),

    http.get(endpoint(apiBaseUrl, '/orders/:id'), async ({ request, params }) => {
      await applyScenarioDelay(state)
      const authError = authGuard(request, state)
      if (authError) return authError
      const error = scenarioError(state)
      if (error) return error
      const id = String(params.id)
      if (id.endsWith('999')) return errorJson(404, 'order not found', 'ERR_NOT_FOUND')
      const baseOrder = buildOrder({
        id,
        status: id.endsWith('041') ? 'partial' : 'filled',
        filled_quantity: id.endsWith('041') ? 4 : 10,
      })
      if (state.scenario === 'empty-data') {
        return HttpResponse.json({ order: baseOrder, fills: [] })
      }
      if (state.scenario === 'partial-service-failure') {
        return HttpResponse.json({
          order: buildOrder({
            id,
            status: 'new_order_status',
            side: 'new_side',
            order_type: 'new_order_type',
            broker: 'unsafe <script>alert(1)</script>',
          }),
          fills: [
            buildTrade({
              order_id: id,
              side: 'new_fill_side',
              external_id: 'fill <script>alert(1)</script>',
            }),
          ],
        })
      }
      return HttpResponse.json({
        order: baseOrder,
        fills: [
          buildTrade({ order_id: id }),
          buildTrade({ id: '00000000-0000-4000-8000-000000000051', order_id: id, quantity: 2, price: 101.25, fee: 0.05 }),
        ],
      })
    }),

    http.get(endpoint(apiBaseUrl, '/orders'), async ({ request }) => {
      await applyScenarioDelay(state)
      const authError = authGuard(request, state)
      if (authError) return authError
      const error = scenarioError(state)
      if (error) return error
      const url = new URL(request.url)
      const ticker = url.searchParams.get('ticker')
      const broker = url.searchParams.get('broker')
      const marketType = url.searchParams.get('market_type')
      const status = url.searchParams.get('status')
      const side = url.searchParams.get('side')
      const orderType = url.searchParams.get('order_type')
      const limit = Number(url.searchParams.get('limit') ?? '20')
      const offset = Number(url.searchParams.get('offset') ?? '0')
      const statuses = ['pending', 'submitted', 'partial', 'filled', 'cancelled', 'rejected'] as const
      const types = ['market', 'limit', 'stop', 'stop_limit', 'trailing_stop'] as const
      const all = state.scenario === 'empty-data'
        ? []
        : Array.from({ length: 24 }, (_, index) => buildOrder({
          id: `00000000-0000-4000-8000-${String(40 + index).padStart(12, '0')}`,
          ticker: index % 2 === 0 ? 'AUGR' : 'LIVE',
          side: index % 2 === 0 ? 'buy' : 'sell',
          order_type: types[index % types.length],
          status: state.scenario === 'partial-service-failure' && index === 0 ? 'new_order_status' : statuses[index % statuses.length],
          market_type: state.scenario === 'partial-service-failure' && index === 0 ? 'new_market' : (index % 2 === 0 ? 'stock' : 'crypto'),
          broker: index % 2 === 0 ? 'paper-broker' : 'backup-broker',
          quantity: 10 + index,
          filled_quantity: index % 3 === 0 ? 0 : 5 + index,
          limit_price: index % 2 === 0 ? 100 + index : undefined,
        }))
      const filtered = all.filter((order) => {
        if (ticker && order.ticker !== ticker) return false
        if (broker && order.broker !== broker) return false
        if (marketType && order.market_type !== marketType) return false
        if (status && order.status !== status) return false
        if (side && order.side !== side) return false
        if (orderType && order.order_type !== orderType) return false
        return true
      })
      const page = filtered.slice(offset, offset + limit)
      const response: { data: ReturnType<typeof buildOrder>[]; total?: number; limit: number; offset: number } = { data: page, total: filtered.length, limit, offset }
      if (state.scenario === 'partial-service-failure') delete response.total
      return HttpResponse.json(response)
    }),

    http.get(endpoint(apiBaseUrl, '/trades'), async ({ request }) => {
      await applyScenarioDelay(state)
      const authError = authGuard(request, state)
      if (authError) return authError
      const error = scenarioError(state)
      if (error) return error
      const url = new URL(request.url)
      const orderId = url.searchParams.get('order_id')
      const positionId = url.searchParams.get('position_id')
      const ticker = url.searchParams.get('ticker')
      const side = url.searchParams.get('side')
      const limit = Number(url.searchParams.get('limit') ?? '20')
      const offset = Number(url.searchParams.get('offset') ?? '0')
      if (orderId && positionId) return errorJson(400, 'order_id and position_id cannot be combined', 'ERR_BAD_REQUEST')
      const all = state.scenario === 'empty-data'
        ? []
        : Array.from({ length: 24 }, (_, index) => buildTrade({
          id: `00000000-0000-4000-8000-${String(50 + index).padStart(12, '0')}`,
          order_id: index % 2 === 0 ? '00000000-0000-4000-8000-000000000040' : '00000000-0000-4000-8000-000000000041',
          position_id: index % 2 === 0 ? '00000000-0000-4000-8000-000000000030' : '00000000-0000-4000-8000-000000000031',
          ticker: index % 2 === 0 ? 'AUGR' : 'LIVE',
          side: state.scenario === 'partial-service-failure' && index === 0 ? 'new_trade_side' : (index % 2 === 0 ? 'buy' : 'sell'),
          quantity: 1 + index,
          price: 100 + index,
          fee: index / 100,
          external_id: state.scenario === 'partial-service-failure' && index === 0 ? 'unsafe <script>alert(1)</script>' : `DEV-PAPER-FILL-${index + 1}`,
        }))
      const filtered = all.filter((trade) => {
        if (orderId && trade.order_id !== orderId) return false
        if (positionId && trade.position_id !== positionId) return false
        if (ticker && trade.ticker !== ticker) return false
        if (side && trade.side !== side) return false
        return true
      })
      const page = filtered.slice(offset, offset + limit)
      const response: { data: ReturnType<typeof buildTrade>[]; total?: number; limit: number; offset: number } = { data: page, total: filtered.length, limit, offset }
      if (state.scenario === 'partial-service-failure') delete response.total
      return HttpResponse.json(response)
    }),

    http.get(endpoint(apiBaseUrl, '/automation/health'), async ({ request }) => {
      await applyScenarioDelay(state)
      const authError = authGuard(request, state)
      if (authError) return authError
      const error = scenarioError(state)
      if (error) return error
      if (state.scenario === 'partial-service-failure') {
        return HttpResponse.json(
          buildAutomationHealth({
            healthy: false,
            failing_jobs: 1,
            jobs: [
              {
                name: 'dev-paper-pipeline',
                enabled: true,
                running: false,
                last_error: 'fixture partial service failure',
                error_count: 3,
                consecutive_failures: 3,
                run_count: 3,
              },
            ],
          }),
        )
      }
      return HttpResponse.json(state.scenario === 'empty-data' ? buildAutomationHealth({ jobs: [], total_jobs: 0 }) : buildAutomationHealth())
    }),

    http.get(endpoint(apiBaseUrl, '/automation/status'), async ({ request }) => {
      await applyScenarioDelay(state)
      const authError = authGuard(request, state)
      if (authError) return authError
      const error = scenarioError(state)
      if (error) return error
      if (state.scenario === 'empty-data') return HttpResponse.json([])
      return HttpResponse.json([
        buildAutomationJobStatus(),
        buildAutomationJobStatus({ name: 'portfolio_allocator', description: 'Shadow portfolio allocator', enabled: true, last_run: undefined, run_count: 0, last_result: '' }),
      ])
    }),

    http.get(endpoint(apiBaseUrl, '/automation/runs'), async ({ request }) => {
      await applyScenarioDelay(state)
      const authError = authGuard(request, state)
      if (authError) return authError
      const error = scenarioError(state)
      if (error) return error
      const url = new URL(request.url)
      const limit = Number(url.searchParams.get('limit') ?? '20')
      const offset = Number(url.searchParams.get('offset') ?? '0')
      const all = state.scenario === 'empty-data' ? [] : [buildAutomationJobRun(), buildAutomationJobRun({ id: fixtureId(71), job_name: 'hot_scan', status: 'failed', error: 'fixture failed' })]
      const page = all.slice(offset, offset + limit)
      return HttpResponse.json({ data: page, total: all.length, limit, offset })
    }),

    http.post(endpoint(apiBaseUrl, '/automation/jobs/:name/run'), async ({ request }) => {
      await applyScenarioDelay(state)
      const authError = authGuard(request, state)
      if (authError) return authError
      return HttpResponse.json({ status: 'triggered' })
    }),

    http.post(endpoint(apiBaseUrl, '/automation/jobs/:name/enable'), async ({ request }) => {
      await applyScenarioDelay(state)
      const authError = authGuard(request, state)
      if (authError) return authError
      const body = await request.json().catch(() => ({ enabled: false })) as { enabled?: boolean }
      return HttpResponse.json({ enabled: Boolean(body.enabled) })
    }),

    http.get(endpoint(apiBaseUrl, '/health'), async () => {
      await applyScenarioDelay(state)
      if (state.scenario === 'server-error') return errorJson(503, 'health degraded', 'ERR_INTERNAL')
      if (state.scenario === 'partial-service-failure') return HttpResponse.json({ status: 'degraded', db: 'ok', redis: 'error' }, { status: 503 })
      return HttpResponse.json({ status: 'ok', db: 'ok', redis: 'ok' })
    }),

    http.get(endpoint(apiBaseUrl, '/strategies'), async ({ request }) => {
      await applyScenarioDelay(state)
      const authError = authGuard(request, state)
      if (authError) return authError
      const error = scenarioError(state)
      if (error) return error
      const url = new URL(request.url)
      const ticker = url.searchParams.get('ticker')
      const marketType = url.searchParams.get('market_type')
      const status = url.searchParams.get('status')
      const isPaper = url.searchParams.get('is_paper')
      const limit = Number(url.searchParams.get('limit') ?? '20')
      const offset = Number(url.searchParams.get('offset') ?? '0')
      const all = state.scenario === 'empty-data'
        ? []
        : [
          buildStrategy({ id: '00000000-0000-4000-8000-000000000010', name: 'DEV PAPER Mean Reversion', status: state.scenario === 'partial-service-failure' ? 'new_backend_status' : 'active', is_paper: true }),
          buildStrategy({ id: '00000000-0000-4000-8000-000000000011', name: 'DEV LIVE Breakout', ticker: 'LIVE', market_type: 'crypto', status: 'paused', is_paper: false }),
        ]
      const filtered = all.filter((strategy) => {
        if (ticker && strategy.ticker !== ticker) return false
        if (marketType && strategy.market_type !== marketType) return false
        if (status && strategy.status !== status) return false
        if (isPaper === 'true' && !strategy.is_paper) return false
        if (isPaper === 'false' && strategy.is_paper) return false
        return true
      })
      const page = filtered.slice(offset, offset + limit)
      return HttpResponse.json({ data: page, total: filtered.length, limit, offset })
    }),

    http.post(endpoint(apiBaseUrl, '/strategies'), async ({ request }) => {
      await applyScenarioDelay(state)
      const authError = authGuard(request, state)
      if (authError) return authError
      const error = scenarioError(state)
      if (error) return error
      const body = await request.json() as Record<string, unknown>
      if (body.is_paper !== true) return errorJson(400, 'strategy create is paper-only; is_paper must be true when provided', 'ERR_VALIDATION')
      if (typeof body.name !== 'string' || body.name.trim() === '' || typeof body.ticker !== 'string' || body.ticker.trim() === '') {
        return errorJson(400, 'name and ticker are required', 'ERR_VALIDATION')
      }
      return HttpResponse.json(buildStrategy({
        id: '00000000-0000-4000-8000-000000000012',
        name: body.name.trim(),
        ticker: body.ticker.trim().toUpperCase(),
        market_type: String(body.market_type ?? 'stock'),
        description: typeof body.description === 'string' ? body.description : undefined,
        schedule_cron: typeof body.schedule_cron === 'string' ? body.schedule_cron : undefined,
        config: body.config ?? {},
        status: 'active',
        is_paper: true,
        skip_next_run: false,
      }), { status: 201 })
    }),

    http.get(endpoint(apiBaseUrl, '/strategies/:id/reports/latest'), async ({ request, params }) => {
      await applyScenarioDelay(state)
      const authError = authGuard(request, state)
      if (authError) return authError
      const error = scenarioError(state)
      if (error) return error
      if (state.scenario === 'empty-data' || params.id === '00000000-0000-4000-8000-000000000999') {
        return errorJson(404, 'no completed report found', 'ERR_NOT_FOUND')
      }
      if (state.scenario === 'partial-service-failure') {
        return HttpResponse.json(buildLatestReport({
          status: 'new_report_status',
          report_type: 'new_backend_report',
          report_json: { unsafe: '<script>alert(1)</script>', score: 0.44, unknown_metadata: { alpha: true } },
          provider: undefined,
          model: undefined,
        }))
      }
      return HttpResponse.json(buildLatestReport())
    }),

    http.get(endpoint(apiBaseUrl, '/strategies/:id/reports'), async ({ request }) => {
      await applyScenarioDelay(state)
      const authError = authGuard(request, state)
      if (authError) return authError
      const error = scenarioError(state)
      if (error) return error
      const url = new URL(request.url)
      const limit = Number(url.searchParams.get('limit') ?? '5')
      const offset = Number(url.searchParams.get('offset') ?? '0')
      const reportType = url.searchParams.get('report_type') || 'paper_validation'
      const all = state.scenario === 'empty-data'
        ? []
        : [
          buildReportArtifact({ id: '00000000-0000-4000-8000-000000000060', report_type: reportType, status: state.scenario === 'partial-service-failure' ? 'new_report_status' : 'completed' }),
          buildReportArtifact({ id: '00000000-0000-4000-8000-000000000061', report_type: reportType, status: 'failed', error_message: 'fixture report failed', completed_at: undefined }),
          buildReportArtifact({ id: '00000000-0000-4000-8000-000000000062', report_type: reportType, time_bucket: '2026-01-14T12:00:00.000Z' }),
          buildReportArtifact({ id: '00000000-0000-4000-8000-000000000063', report_type: reportType, time_bucket: '2026-01-13T12:00:00.000Z' }),
          buildReportArtifact({ id: '00000000-0000-4000-8000-000000000064', report_type: reportType, time_bucket: '2026-01-12T12:00:00.000Z' }),
          buildReportArtifact({ id: '00000000-0000-4000-8000-000000000065', report_type: reportType, time_bucket: '2026-01-11T12:00:00.000Z' }),
        ]
      const page = all.slice(offset, offset + limit)
      return HttpResponse.json({ data: page, limit, offset })
    }),

    http.get(endpoint(apiBaseUrl, '/strategies/:id'), async ({ request, params }) => {
      await applyScenarioDelay(state)
      const authError = authGuard(request, state)
      if (authError) return authError
      const error = scenarioError(state)
      if (error) return error
      if (params.id === '00000000-0000-4000-8000-000000000999') return errorJson(404, 'strategy not found', 'ERR_NOT_FOUND')
      if (state.scenario === 'empty-data') return HttpResponse.json(buildStrategy({ latest_run_summary: undefined }))
      if (state.scenario === 'partial-service-failure') {
        return HttpResponse.json(buildStrategy({
          status: 'new_backend_status',
          config: { fixture: true, nested: { threshold: 0.42 }, unsafe: '<script>alert(1)</script>' },
          latest_run_summary: {
            id: '00000000-0000-4000-8000-000000000020',
            strategy_id: '00000000-0000-4000-8000-000000000010',
            ticker: 'AUGR',
            status: 'new_run_status',
            signal: 'unknown_signal',
            started_at: '2026-01-15T12:00:00.000Z',
          },
        }))
      }
      return HttpResponse.json(buildStrategy({
        latest_run_summary: {
          id: '00000000-0000-4000-8000-000000000020',
          strategy_id: '00000000-0000-4000-8000-000000000010',
          ticker: 'AUGR',
          status: 'completed',
          signal: 'hold',
          started_at: '2026-01-15T12:00:00.000Z',
          completed_at: '2026-01-15T12:10:00.000Z',
        },
      }))
    }),

    http.put(endpoint(apiBaseUrl, '/strategies/:id'), async ({ request, params }) => {
      await applyScenarioDelay(state)
      const authError = authGuard(request, state)
      if (authError) return authError
      const error = scenarioError(state)
      if (error) return error
      const body = await request.json() as Record<string, unknown>
      if (params.id === '00000000-0000-4000-8000-000000000011') return errorJson(409, 'only paper strategies can be edited', 'ERR_CONFLICT')
      if (body.updated_at === '2020-01-01T00:00:00.000Z') return errorJson(409, 'strategy changed since it was loaded', 'ERR_CONFLICT')
      if (typeof body.name !== 'string' || body.name.trim() === '' || typeof body.ticker !== 'string' || body.ticker.trim() === '') {
        return errorJson(400, 'name and ticker are required', 'ERR_VALIDATION')
      }
      return HttpResponse.json(buildStrategy({
        id: String(params.id),
        name: body.name.trim(),
        ticker: body.ticker.trim().toUpperCase(),
        market_type: String(body.market_type ?? 'stock'),
        description: typeof body.description === 'string' ? body.description : undefined,
        schedule_cron: typeof body.schedule_cron === 'string' ? body.schedule_cron : undefined,
        config: body.config ?? {},
        status: 'active',
        is_paper: true,
        skip_next_run: false,
      }))
    }),

    http.delete(endpoint(apiBaseUrl, '/strategies/:id'), async ({ request, params }) => {
      await applyScenarioDelay(state)
      const authError = authGuard(request, state)
      if (authError) return authError
      const error = scenarioError(state)
      if (error) return error
      if (params.id === '00000000-0000-4000-8000-000000000999') return errorJson(404, 'strategy not found', 'ERR_NOT_FOUND')
      if (params.id === '00000000-0000-4000-8000-000000000011') return errorJson(409, 'delete is only allowed for paper strategies', 'ERR_CONFLICT')
      return new HttpResponse(null, { status: 204 })
    }),

    http.post(endpoint(apiBaseUrl, '/strategies/:id/pause'), async ({ request, params }) => {
      await applyScenarioDelay(state)
      const authError = authGuard(request, state)
      if (authError) return authError
      const error = scenarioError(state)
      if (error) return error
      if (params.id === '00000000-0000-4000-8000-000000000011') return errorJson(409, 'pause is only allowed for paper strategies', 'ERR_CONFLICT')
      return HttpResponse.json(buildStrategy({ id: String(params.id), status: 'paused', is_paper: true }))
    }),

    http.post(endpoint(apiBaseUrl, '/strategies/:id/resume'), async ({ request, params }) => {
      await applyScenarioDelay(state)
      const authError = authGuard(request, state)
      if (authError) return authError
      const error = scenarioError(state)
      if (error) return error
      if (params.id === '00000000-0000-4000-8000-000000000011') return errorJson(409, 'resume is only allowed for paper strategies', 'ERR_CONFLICT')
      return HttpResponse.json(buildStrategy({ id: String(params.id), status: 'active', is_paper: true }))
    }),

    http.post(endpoint(apiBaseUrl, '/strategies/:id/skip-next'), async ({ request, params }) => {
      await applyScenarioDelay(state)
      const authError = authGuard(request, state)
      if (authError) return authError
      const error = scenarioError(state)
      if (error) return error
      if (params.id === '00000000-0000-4000-8000-000000000011') return errorJson(409, 'skip-next is only allowed for paper strategies', 'ERR_CONFLICT')
      return HttpResponse.json(buildStrategy({ id: String(params.id), status: 'active', skip_next_run: true, is_paper: true }))
    }),

    http.post(endpoint(apiBaseUrl, '/strategies/:id/run'), async ({ request, params }) => {
      await applyScenarioDelay(state)
      const authError = authGuard(request, state)
      if (authError) return authError
      const error = scenarioError(state)
      if (error) return error
      if (params.id === '00000000-0000-4000-8000-000000000011') return errorJson(409, 'manual run is only allowed for paper strategies', 'ERR_CONFLICT')
      return HttpResponse.json({ status: 'accepted', strategy_id: String(params.id), message: 'strategy run started' }, { status: 202 })
    }),
  ]
}
