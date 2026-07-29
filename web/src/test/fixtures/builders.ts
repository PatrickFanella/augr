import type { AuthResponse, AuthSession } from '@/shared/types/auth'
import type {
  AgentDecision,
  AgentEvent,
  AllocationDecision,
  AllocatorDiagnostics,
  AllocatorOpportunity,
  AllocatorSummary,
  AutomationHealthResponse,
  AutomationJobRun,
  AutomationJobStatus,
  Order,
  PipelineRun,
  Position,
  ReportArtifact,
  ReportLatestResponse,
  RiskBreakersResponse,
  RiskCockpitSummary,
  RiskEngineStatus,
  RunSnapshot,
  Strategy,
  Trade,
  User,
} from '@/shared/types/domain'
import type { PortfolioSummary } from '@/shared/types/api'
import type { SettingsResponse } from '@/shared/types/settings'
import type { WebSocketEventEnvelope } from '@/shared/types/websocket'

import { fixtureDate, fixtureId, fixtureLaterDate } from '@/test/fixtures/ids'

export const mockAccessToken = 'dev-paper-access-token'
export const mockRefreshToken = 'dev-paper-refresh-token'
export const expiredAccessToken = 'expired-access-token'

export function buildUser(overrides: Partial<User> = {}): User {
  return {
    id: fixtureId(1),
    username: 'dev-paper-operator',
    created_at: fixtureDate,
    updated_at: fixtureDate,
    ...overrides,
  }
}

export function buildAuthResponse(overrides: Partial<AuthResponse> = {}): AuthResponse {
  return {
    access_token: mockAccessToken,
    refresh_token: mockRefreshToken,
    expires_at: '2026-01-15T13:00:00Z',
    ...overrides,
  }
}

export function buildAuthSession(overrides: Partial<AuthSession> = {}): AuthSession {
  return {
    user: buildUser(),
    access_token: mockAccessToken,
    refresh_token: mockRefreshToken,
    expires_at: '2026-01-15T13:00:00Z',
    ...overrides,
  }
}

export function buildStrategy(overrides: Partial<Strategy> = {}): Strategy {
  return {
    id: fixtureId(10),
    name: 'DEV PAPER Mean Reversion',
    description: 'Development fixture strategy; not live trading data.',
    ticker: 'AUGR',
    market_type: 'stock',
    schedule_cron: '*/15 * * * *',
    config: { fixture: true, mode: 'paper' },
    status: 'active',
    skip_next_run: false,
    is_paper: true,
    created_at: fixtureDate,
    updated_at: fixtureDate,
    ...overrides,
  }
}

export function buildReportArtifact(overrides: Partial<ReportArtifact> = {}): ReportArtifact {
  return {
    id: fixtureId(60),
    strategy_id: fixtureId(10),
    report_type: 'paper_validation',
    time_bucket: fixtureDate,
    status: 'completed',
    report_json: { summary: 'Paper validation passed', score: 0.91, evidence: ['fixture-run'] },
    provider: 'openai',
    model: 'gpt-4.1-mini',
    prompt_tokens: 1200,
    completion_tokens: 350,
    latency_ms: 2400,
    created_at: fixtureDate,
    completed_at: fixtureLaterDate,
    ...overrides,
  }
}

export function buildLatestReport(overrides: Partial<ReportLatestResponse> = {}): ReportLatestResponse {
  return {
    ...buildReportArtifact(overrides),
    stale_seconds: 300,
    ...overrides,
  }
}

export function buildRun(overrides: Partial<PipelineRun> = {}): PipelineRun {
  return {
    id: fixtureId(20),
    strategy_id: fixtureId(10),
    ticker: 'AUGR',
    trade_date: fixtureDate,
    status: 'running',
    signal: 'hold',
    started_at: fixtureDate,
    config_snapshot: { fixture: true, mode: 'paper' },
    phase_timings: { ingest_ms: 42 },
    ...overrides,
  }
}

export function buildAgentDecision(overrides: Partial<AgentDecision> = {}): AgentDecision {
  return {
    id: fixtureId(70),
    pipeline_run_id: fixtureId(20),
    agent_role: 'analyst',
    phase: 'signal_generation',
    round_number: 1,
    input_summary: 'Fixture market context',
    output_text: 'Hold until confirmation improves.',
    output_structured: { signal: 'hold', confidence: 0.62 },
    llm_provider: 'fixture-provider',
    llm_model: 'fixture-model',
    prompt_tokens: 120,
    completion_tokens: 80,
    latency_ms: 250,
    cost_usd: 0.01,
    created_at: fixtureDate,
    ...overrides,
  }
}

export function buildRunSnapshot(overrides: RunSnapshot = {}): RunSnapshot {
  return {
    market_state: {
      ticker: 'AUGR',
      price: 101.25,
      liquidity: 'paper-fixture',
      api_key: 'fixture-secret-should-redact',
    },
    strategy_config: {
      mode: 'paper',
      threshold: 0.42,
      nested: { access_token: 'fixture-token-should-redact' },
    },
    portfolio_state: {
      open_positions: 1,
      cash: 10000,
    },
    ...overrides,
  }
}

export function buildAgentEvent(overrides: Partial<AgentEvent> = {}): AgentEvent {
  return {
    id: fixtureId(80),
    pipeline_run_id: fixtureId(20),
    strategy_id: fixtureId(10),
    agent_role: 'analyst',
    event_kind: 'agent_decision',
    title: 'Analyst decision recorded',
    summary: 'Read-only persisted event fixture.',
    tags: ['paper', 'fixture'],
    metadata: { signal: 'hold', order_id: fixtureId(40) },
    created_at: fixtureDate,
    ...overrides,
  }
}

export function buildPosition(overrides: Partial<Position> = {}): Position {
  return {
    id: fixtureId(30),
    strategy_id: fixtureId(10),
    market_type: 'stock',
    ticker: 'AUGR',
    side: 'long',
    quantity: 10,
    avg_entry: 100,
    current_price: 101.5,
    unrealized_pnl: 15,
    realized_pnl: 0,
    opened_at: fixtureDate,
    ...overrides,
  }
}

export function buildAllocatorDiagnostics(overrides: Partial<AllocatorDiagnostics> = {}): AllocatorDiagnostics {
  return {
    run_counts_by_signal: { hold: 4, buy: 2, sell: 1 },
    run_counts_by_status: { running: 1, completed: 6, failed: 1 },
    decision_counts_by_status: { approved: 3, rejected: 2, no_action: 1 },
    no_action_reasons: { insufficient_edge: 2, risk_limit: 1 },
    active_strategies_by_market: { stock: 2, polymarket: 1 },
    open_positions_by_market: { stock: 1, unknown: 1 },
    buying_power_utilization_pct: 12.5,
    gross_exposure_pct: 18.25,
    target_gross_exposure_pct: 30,
    utilization_gap_pct: 11.75,
    warnings: ['account_balance_unavailable'],
    ...overrides,
  }
}

export function buildAllocatorOpportunity(overrides: Partial<AllocatorOpportunity> = {}): AllocatorOpportunity {
  return {
    id: fixtureId(90),
    strategy_id: fixtureId(10),
    pipeline_run_id: fixtureId(20),
    market_type: 'stock',
    ticker: 'AUGR',
    side: 'buy',
    signal: 'hold',
    status: 'queued',
    score: 0.72,
    confidence: 0.68,
    edge_pct: 4.2,
    expected_return_pct: 6.1,
    max_loss_pct: 2.4,
    entry_price: 101.25,
    liquidity_usd: 125000,
    market_cap_usd: 5000000,
    spread_pct: 0.4,
    proposed_notional: 250,
    selected_notional: 200,
    reason: 'Paper allocator fixture opportunity.',
    evidence: { model_score: 0.72, notes: ['fixture'] },
    expires_at: fixtureLaterDate,
    created_at: fixtureDate,
    updated_at: fixtureLaterDate,
    dedupe_key: 'fixture-augr-buy',
    ...overrides,
  }
}

export function buildAllocationDecision(overrides: Partial<AllocationDecision> = {}): AllocationDecision {
  return {
    id: fixtureId(100),
    opportunity_id: fixtureId(90),
    strategy_id: fixtureId(10),
    mode: 'shadow',
    action: 'select',
    score: 0.74,
    notional_usd: 200,
    quantity: 2,
    reasons: ['paper-only fixture', 'risk within limit'],
    created_order_id: fixtureId(40),
    created_at: fixtureDate,
    ...overrides,
  }
}

export function buildAllocatorSummary(overrides: Partial<AllocatorSummary> = {}): AllocatorSummary {
  return {
    opportunity_counts_by_status: { queued: 2, selected: 1, rejected: 1, expired: 0, executed: 0 },
    recent_decisions: [buildAllocationDecision()],
    warnings: ['opportunities_unavailable'],
    ...overrides,
  }
}

export function buildOrder(overrides: Partial<Order> = {}): Order {
  return {
    id: fixtureId(40),
    strategy_id: fixtureId(10),
    pipeline_run_id: fixtureId(20),
    external_id: 'DEV-PAPER-ORDER-1',
    ticker: 'AUGR',
    market_type: 'stock',
    side: 'buy',
    order_type: 'market',
    quantity: 10,
    filled_quantity: 10,
    filled_avg_price: 100,
    status: 'filled',
    broker: 'paper-broker',
    submitted_at: fixtureDate,
    filled_at: fixtureLaterDate,
    created_at: fixtureDate,
    ...overrides,
  }
}

export function buildTrade(overrides: Partial<Trade> = {}): Trade {
  return {
    id: fixtureId(50),
    order_id: fixtureId(40),
    position_id: fixtureId(30),
    external_id: 'DEV-PAPER-FILL-1',
    ticker: 'AUGR',
    side: 'buy',
    quantity: 10,
    price: 100,
    fee: 0,
    executed_at: fixtureLaterDate,
    created_at: fixtureLaterDate,
    ...overrides,
  }
}

export function buildRiskStatus(overrides: Partial<RiskEngineStatus> = {}): RiskEngineStatus {
  return {
    risk_status: 'normal',
    circuit_breaker: { state: 'open' },
    kill_switch: { active: false, mechanisms: ['api_toggle'] },
    market_kill_switches: {},
    position_limits: {
      max_per_position_pct: 0.05,
      max_total_pct: 0.3,
      max_concurrent: 5,
      max_per_market_pct: 0.2,
      current_open_positions: 1,
      current_total_exposure_pct: 0.04,
    },
    updated_at: fixtureDate,
    ...overrides,
  }
}

export function buildRiskCockpit(overrides: Partial<RiskCockpitSummary> = {}): RiskCockpitSummary {
  return {
    generated_at: fixtureDate,
    kill_switch_active: false,
    circuit_breaker: false,
    exposures: [
      { market_type: 'stock', open_positions: 2, marked_positions: 2, unmarked_positions: 0, approved_decisions: 5, rejected_decisions: 1, gross_exposure: 0.18, gross_marked_value: 0.2, net_expected_value: 120.5 },
      { market_type: 'crypto', open_positions: 1, marked_positions: 1, unmarked_positions: 0, approved_decisions: 2, rejected_decisions: 0, gross_exposure: 0.06, gross_marked_value: 0.07, net_expected_value: 25.25 },
    ],
    open_positions: 3,
    marked_positions: 3,
    unmarked_positions: 0,
    gross_cost_basis: 0.24,
    valuation_status: 'complete',
    reconciliation_status: 'complete',
    warnings: ['paper risk cockpit fixture'],
    ...overrides,
  }
}

export function buildRiskBreakers(overrides: Partial<RiskBreakersResponse> = {}): RiskBreakersResponse {
  return {
    tripped: [
      { scope: 'global', tripped_at: fixtureDate, reason: 'paper drawdown guard' },
    ],
    ...overrides,
  }
}

export function buildPortfolioSummary(overrides: Partial<PortfolioSummary> = {}): PortfolioSummary {
  return {
    open_positions: 1,
    marked_positions: 1,
    unmarked_positions: 0,
    unrealized_pnl: 15,
    realized_pnl: 0,
    total_pnl: 15,
    gross_cost_basis: 100,
    gross_marked_value: 115,
    valuation_status: 'complete',
    valuation_generated_at: fixtureDate,
    ...overrides,
  }
}

export function buildAutomationHealth(overrides: Partial<AutomationHealthResponse> = {}): AutomationHealthResponse {
  return {
    jobs: [
      {
        name: 'dev-paper-pipeline',
        enabled: true,
        running: false,
        last_run: fixtureDate,
        error_count: 0,
        consecutive_failures: 0,
        run_count: 3,
      },
    ],
    healthy: true,
    total_jobs: 1,
    failing_jobs: 0,
    degraded_jobs: 0,
    ...overrides,
  }
}

export function buildAutomationJobStatus(overrides: Partial<AutomationJobStatus> = {}): AutomationJobStatus {
  return {
    name: 'deep_scan',
    description: 'Deep strategy scan',
    schedule: 'Every hour (market hours only), skip holidays',
    enabled: true,
    running: false,
    last_run: fixtureDate,
    last_result: 'completed',
    last_summary: { scanned: 12, triggered: 1 },
    error_count: 0,
    consecutive_failures: 0,
    run_count: 8,
    ...overrides,
  }
}

export function buildAutomationJobRun(overrides: Partial<AutomationJobRun> = {}): AutomationJobRun {
  return {
    id: fixtureId(70),
    job_name: 'deep_scan',
    status: 'completed',
    started_at: fixtureDate,
    completed_at: fixtureLaterDate,
    duration_ns: 2_000_000_000,
    consecutive_failures: 0,
    created_at: fixtureDate,
    ...overrides,
  }
}

export function buildSettings(overrides: Partial<SettingsResponse> = {}): SettingsResponse {
  const provider = { api_key_configured: false, model: 'dev-paper-model' }
  return {
    llm: {
      default_provider: 'ollama',
      deep_think_model: 'dev-deep',
      quick_think_model: 'dev-quick',
      providers: {
        openai: provider,
        anthropic: provider,
        google: provider,
        openrouter: provider,
        xai: provider,
        ollama: { ...provider, base_url: 'http://localhost:11434' },
      },
    },
    risk: {
      max_position_size_pct: 0.05,
      max_daily_loss_pct: 0.03,
      max_drawdown_pct: 0.1,
      max_open_positions: 5,
      max_total_exposure_pct: 0.3,
      max_per_market_exposure_pct: 0.2,
      circuit_breaker_threshold_pct: 0.03,
      circuit_breaker_cooldown_min: 15,
    },
    system: {
      environment: 'development-paper',
      version: 'dev-fixture',
      current_schema_version: 1,
      required_schema_version: 1,
      schema_status: 'current',
      uptime_seconds: 3600,
      connected_brokers: [{ name: 'paper-broker', paper_mode: true, configured: true }],
    },
    ...overrides,
  }
}

export function buildWebSocketEvent(overrides: Partial<WebSocketEventEnvelope> = {}): WebSocketEventEnvelope {
  return {
    type: 'pipeline_start',
    strategy_id: fixtureId(10),
    run_id: fixtureId(20),
    data: { fixture: true, mode: 'paper' },
    timestamp: fixtureDate,
    ...overrides,
  }
}
