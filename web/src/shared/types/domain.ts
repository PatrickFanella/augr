import type { ISODate, RawJson, UUID } from '@/shared/types/primitives'

export const marketTypes = ['stock', 'crypto', 'polymarket', 'kalshi', 'options'] as const
export const strategyStatuses = ['active', 'paused', 'inactive'] as const
export const reportStatuses = ['completed', 'failed', 'running'] as const
export const pipelineStatuses = ['running', 'completed', 'failed', 'cancelled'] as const
export const pipelineSignals = ['buy', 'sell', 'hold'] as const
export const orderSides = ['buy', 'sell'] as const
export const orderTypes = ['market', 'limit', 'stop', 'stop_limit', 'trailing_stop'] as const
export const orderStatuses = ['pending', 'submitted', 'partial', 'filled', 'cancelled', 'rejected'] as const
export const positionSides = ['long', 'short'] as const
export const riskStatuses = ['normal', 'warning', 'breached'] as const
export const circuitBreakerPhases = ['open', 'tripped', 'cooldown'] as const
export const killSwitchMechanisms = ['api_toggle', 'file_flag', 'env_var', 'unknown'] as const

export type KnownMarketType = (typeof marketTypes)[number]
export type KnownStrategyStatus = (typeof strategyStatuses)[number]
export type KnownReportStatus = (typeof reportStatuses)[number]
export type KnownPipelineStatus = (typeof pipelineStatuses)[number]
export type KnownPipelineSignal = (typeof pipelineSignals)[number]
export type KnownOrderSide = (typeof orderSides)[number]
export type KnownOrderType = (typeof orderTypes)[number]
export type KnownOrderStatus = (typeof orderStatuses)[number]
export type KnownPositionSide = (typeof positionSides)[number]
export type KnownRiskStatus = (typeof riskStatuses)[number]
export type KnownCircuitBreakerPhase = (typeof circuitBreakerPhases)[number]
export type KnownKillSwitchMechanism = (typeof killSwitchMechanisms)[number]

// Keep wire enums forward-compatible. UI components can compare against the known arrays.
export type MarketType = KnownMarketType | (string & {})
export type StrategyStatus = KnownStrategyStatus | (string & {})
export type ReportStatus = KnownReportStatus | (string & {})
export type PipelineStatus = KnownPipelineStatus | (string & {})
export type PipelineSignal = KnownPipelineSignal | (string & {})
export type OrderSide = KnownOrderSide | (string & {})
export type OrderType = KnownOrderType | (string & {})
export type OrderStatus = KnownOrderStatus | (string & {})
export type PositionSide = KnownPositionSide | (string & {})
export type RiskStatus = KnownRiskStatus | (string & {})
export type CircuitBreakerPhase = KnownCircuitBreakerPhase | (string & {})
export type KillSwitchMechanism = KnownKillSwitchMechanism | (string & {})

export type User = {
  id: UUID
  username: string
  created_at: ISODate
  updated_at: ISODate
}

export type StrategyLatestRunSummary = {
  id: UUID
  strategy_id: UUID
  ticker: string
  status: PipelineStatus
  signal?: PipelineSignal
  started_at: ISODate
  completed_at?: ISODate
}

export type Strategy = {
  id: UUID
  name: string
  description?: string
  ticker: string
  market_type: MarketType
  schedule_cron?: string
  config: RawJson
  status: StrategyStatus
  skip_next_run: boolean
  is_paper: boolean
  created_at: ISODate
  updated_at: ISODate
  latest_run_summary?: StrategyLatestRunSummary
}

export type StrategyCreateRequest = {
  name: string
  description?: string
  ticker: string
  market_type: KnownMarketType
  schedule_cron?: string
  config: RawJson
  is_paper: true
}

export type StrategyUpdateRequest = {
  name: string
  description?: string
  ticker: string
  market_type: KnownMarketType
  schedule_cron?: string
  config: RawJson
  updated_at: ISODate
}

export type StrategyRunAcceptedResponse = {
  status: 'accepted' | (string & {})
  strategy_id: UUID
  message: string
}

export type ReportArtifact = {
  id: UUID
  strategy_id: UUID
  report_type: string
  time_bucket: ISODate
  status: ReportStatus
  report_json?: RawJson
  provider?: string
  model?: string
  prompt_tokens: number
  completion_tokens: number
  latency_ms: number
  error_message?: string
  created_at: ISODate
  completed_at?: ISODate
}

export type ReportLatestResponse = ReportArtifact & {
  stale_seconds: number
}

export type PipelineRun = {
  id: UUID
  strategy_id: UUID
  ticker: string
  trade_date: ISODate
  status: PipelineStatus
  signal?: PipelineSignal
  started_at: ISODate
  completed_at?: ISODate
  error_message?: string
  config_snapshot?: RawJson
  phase_timings?: RawJson
}

export type AgentDecision = {
  id: UUID
  pipeline_run_id: UUID
  agent_role: string
  phase: string
  round_number?: number
  input_summary?: string
  output_text: string
  output_structured?: RawJson
  llm_provider?: string
  llm_model?: string
  prompt_text?: string
  prompt_tokens?: number
  completion_tokens?: number
  latency_ms?: number
  cost_usd?: number
  created_at: ISODate
}

export type RunSnapshot = Record<string, RawJson>

export type AgentEvent = {
  id: UUID
  pipeline_run_id?: UUID
  strategy_id?: UUID
  agent_role?: string
  event_kind: string
  title: string
  summary?: string
  tags?: string[]
  metadata?: RawJson
  created_at: ISODate
}

export type AllocatorDiagnostics = {
  run_counts_by_signal: Record<string, number>
  run_counts_by_status: Record<string, number>
  decision_counts_by_status: Record<string, number>
  no_action_reasons: Record<string, number>
  active_strategies_by_market: Record<string, number>
  open_positions_by_market: Record<string, number>
  buying_power_utilization_pct: number
  gross_exposure_pct: number
  target_gross_exposure_pct: number
  utilization_gap_pct: number
  warnings: string[]
}

export type AllocatorOpportunity = {
  id: UUID
  strategy_id: UUID
  pipeline_run_id?: UUID
  market_type: MarketType
  ticker: string
  side: string
  prediction_side?: string
  signal: string
  status: string
  score?: number
  confidence: number
  edge_pct: number
  expected_return_pct: number
  max_loss_pct: number
  entry_price: number
  liquidity_usd: number
  market_cap_usd: number
  spread_pct: number
  proposed_notional: number
  selected_notional: number
  reason: string
  reject_reason?: string
  evidence?: RawJson
  expires_at: ISODate
  created_at: ISODate
  updated_at: ISODate
  dedupe_key: string
}

export type AllocationDecision = {
  id: UUID
  opportunity_id?: UUID
  strategy_id?: UUID
  mode: string
  action: string
  score: number
  notional_usd: number
  quantity: number
  reasons: string[]
  created_order_id?: UUID
  created_at: ISODate
}

export type AllocatorSummary = {
  opportunity_counts_by_status: Record<string, number>
  recent_decisions: AllocationDecision[]
  warnings?: string[]
}

export type Position = {
  id: UUID
  strategy_id?: UUID
  market_type?: MarketType
  ticker: string
  side: PositionSide
  quantity: number
  avg_entry: number
  current_price?: number
  unrealized_pnl?: number
  realized_pnl: number
  stop_loss?: number
  take_profit?: number
  opened_at: ISODate
  closed_at?: ISODate
  asset_class?: string
  underlying_ticker?: string
  option_type?: string
  strike?: number
  expiry?: ISODate
  contract_multiplier?: number
  leg_group_id?: UUID
  delta?: number
  gamma?: number
  theta?: number
  vega?: number
}

export type Order = {
  id: UUID
  strategy_id?: UUID
  pipeline_run_id?: UUID
  external_id?: string
  ticker: string
  market_type?: MarketType
  side: OrderSide
  order_type: OrderType
  quantity: number
  limit_price?: number
  stop_price?: number
  filled_quantity: number
  filled_avg_price?: number
  status: OrderStatus
  broker: string
  submitted_at?: ISODate
  filled_at?: ISODate
  created_at: ISODate
  asset_class?: string
  underlying_ticker?: string
  option_type?: string
  strike?: number
  expiry?: ISODate
  contract_multiplier?: number
  position_intent?: string
  leg_group_id?: UUID
  prediction_side?: string
  polymarket_intent?: string
}

export type Trade = {
  id: UUID
  order_id?: UUID
  position_id?: UUID
  external_id?: string
  ticker: string
  side: OrderSide
  quantity: number
  price: number
  fee: number
  executed_at: ISODate
  created_at: ISODate
  asset_class?: string
  open_close?: string
  contract_multiplier?: number
  premium?: number
  exit_reason?: string
}

export type OrderDetailResponse = {
  order: Order
  fills: Trade[]
}

export type RiskSettings = {
  max_position_size_pct: number
  max_daily_loss_pct: number
  max_drawdown_pct: number
  max_open_positions: number
  max_total_exposure_pct?: number
  max_per_market_exposure_pct?: number
  circuit_breaker_threshold_pct?: number
  circuit_breaker_cooldown_min?: number
}

export type RiskEngineStatus = {
  risk_status: RiskStatus
  circuit_breaker: {
    state: CircuitBreakerPhase
    reason?: string
    tripped_at?: ISODate
    cooldown_end?: ISODate
  }
  kill_switch: {
    active: boolean
    reason?: string
    mechanisms?: KillSwitchMechanism[]
    activated_at?: ISODate
  }
  market_kill_switches?: Partial<Record<MarketType, RiskEngineStatus['kill_switch']>>
  position_limits: {
    max_per_position_pct: number
    max_total_pct: number
    max_concurrent: number
    max_per_market_pct: number
    current_open_positions?: number
    current_total_exposure_pct?: number
  }
  updated_at: ISODate
}

export type RiskBreakerState = {
  scope: string
  tripped_at: ISODate
  reason: string
  reset_at?: ISODate
}

export type RiskBreakersResponse = {
  tripped: RiskBreakerState[]
}

export type RiskCockpitExposure = {
  market_type: MarketType
  open_positions: number
  approved_decisions: number
  rejected_decisions: number
  gross_exposure: number
  net_expected_value: number
}

export type RiskCockpitSummary = {
  generated_at: ISODate
  kill_switch_active: boolean
  circuit_breaker: boolean
  exposures: RiskCockpitExposure[]
  warnings: string[]
}

export type KillSwitchToggleRequest = {
  active: boolean
  reason: string
}

export type KillSwitchToggleResponse = {
  active: boolean
  reason?: string
  mechanisms?: KillSwitchMechanism[]
  activated_at?: ISODate
  updated_at: ISODate
}

export type MarketKillSwitchRequest = {
  reason: string
}

export type MarketKillSwitchResponse = {
  market_type: MarketType
  active: boolean
}

export type BreakerResetRequest = {
  scope: string
}

export type BreakerResetResponse = {
  scope: string
  reset: boolean
}

export type AutomationJobHealth = {
  name: string
  enabled: boolean
  running: boolean
  last_run?: ISODate
  last_error?: string
  error_count: number
  consecutive_failures: number
  run_count: number
}

export type AutomationHealthResponse = {
  jobs: AutomationJobHealth[]
  healthy: boolean
  total_jobs: number
  failing_jobs: number
  degraded_jobs: number
}

export type HealthStatusResponse = {
  status: string
  db: string
  redis: string
}
