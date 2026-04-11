export type UUID = string
export type ISODateString = string

export type MarketType = 'stock' | 'crypto' | 'polymarket'
export type StrategyLLMProvider = 'openai' | 'anthropic' | 'google' | 'openrouter' | 'xai' | 'ollama'

export interface StrategyConfigWire {
  llm_config?: {
    provider?: StrategyLLMProvider
    deep_think_model?: string
    quick_think_model?: string
  }
  pipeline_config?: {
    debate_rounds?: number
    analysis_timeout_seconds?: number
    debate_timeout_seconds?: number
  }
  risk_config?: {
    position_size_pct?: number
    stop_loss_multiplier?: number
    take_profit_multiplier?: number
    min_confidence?: number
  }
  analyst_selection?: AgentRole[]
  prompt_overrides?: Record<string, string>
}
export type StrategyStatus = 'active' | 'paused' | 'inactive'
export type PipelineStatus = 'running' | 'completed' | 'failed' | 'cancelled'
export type PipelineSignal = 'buy' | 'sell' | 'hold'
export type OrderSide = 'buy' | 'sell'
export type OrderType = 'market' | 'limit' | 'stop' | 'stop_limit' | 'trailing_stop'
export type OrderStatus = 'pending' | 'submitted' | 'partial' | 'filled' | 'cancelled' | 'rejected'
export type PositionSide = 'long' | 'short'
export type AgentRole =
  | 'market_analyst'
  | 'fundamentals_analyst'
  | 'news_analyst'
  | 'social_media_analyst'
  | 'bull_researcher'
  | 'bear_researcher'
  | 'trader'
  | 'invest_judge'
  | 'risk_manager'
  | 'aggressive_analyst'
  | 'conservative_analyst'
  | 'neutral_analyst'
  | 'aggressive_risk'
  | 'conservative_risk'
  | 'neutral_risk'
export type Phase = 'analysis' | 'research_debate' | 'trading' | 'risk_debate'
export type RiskStatus = 'normal' | 'warning' | 'breached'
export type CircuitBreakerPhase = 'open' | 'tripped' | 'cooldown'
export type KillSwitchMechanism = 'api_toggle' | 'file_flag' | 'env_var' | 'unknown'
export type WebSocketEventType =
  | 'pipeline_start'
  | 'agent_decision'
  | 'debate_round'
  | 'signal'
  | 'order_submitted'
  | 'order_filled'
  | 'position_update'
  | 'circuit_breaker'
  | 'error'

export interface ErrorResponse {
  error: string
  code: string
}

export interface ListResponse<T> {
  data: T[]
  total?: number
  limit: number
  offset: number
}

export interface HealthStatus {
  status: string
}

export interface PredictionMarketData {
  slug: string
  question: string
  description?: string
  resolution_criteria?: string
  end_date?: ISODateString
  resolution_source?: string
  yes_price: number
  no_price: number
  volume_24h: number
  liquidity: number
  open_interest: number
  condition_id?: string
  yes_token_id?: string
  no_token_id?: string
  best_bid_yes?: number
  best_ask_yes?: number
  best_bid_no?: number
  best_ask_no?: number
  spread_yes?: number
}

export interface Strategy {
  id: UUID
  name: string
  description?: string
  ticker: string
  market_type: MarketType
  schedule_cron?: string
  schedule_description?: string
  config: StrategyConfigWire
  status: StrategyStatus
  skip_next_run: boolean
  is_active?: boolean
  is_paper: boolean
  prediction_market?: PredictionMarketData
  created_at: ISODateString
  updated_at: ISODateString
}

export interface PipelineRun {
  id: UUID
  strategy_id: UUID
  ticker: string
  trade_date: ISODateString
  status: PipelineStatus
  signal?: PipelineSignal
  started_at: ISODateString
  completed_at?: ISODateString
  error_message?: string
  config_snapshot?: unknown
  phase_timings?: unknown
}

export interface StrategyRunResult {
  run: PipelineRun
  signal?: PipelineSignal
  orders?: Order[]
  positions?: Position[]
}

export interface AgentDecision {
  id: UUID
  pipeline_run_id: UUID
  agent_role: AgentRole
  phase: Phase
  round_number?: number
  input_summary?: string
  output_text: string
  output_structured?: unknown
  llm_provider?: string
  llm_model?: string
  prompt_tokens?: number
  completion_tokens?: number
  latency_ms?: number
  created_at: ISODateString
}

export interface AgentEvent {
  id: UUID
  pipeline_run_id?: UUID
  strategy_id?: UUID
  agent_role?: AgentRole
  event_kind: string
  title: string
  summary?: string
  tags?: string[]
  metadata?: unknown
  created_at: ISODateString
}

export interface Conversation {
  id: UUID
  pipeline_run_id: UUID
  agent_role: AgentRole
  title?: string
  created_at: ISODateString
  updated_at: ISODateString
}

export interface ConversationMessage {
  id: UUID
  conversation_id: UUID
  role: 'user' | 'assistant'
  content: string
  created_at: ISODateString
}

export interface Position {
  id: UUID
  strategy_id?: UUID
  ticker: string
  side: PositionSide
  quantity: number
  avg_entry: number
  current_price?: number
  unrealized_pnl?: number
  realized_pnl: number
  stop_loss?: number
  take_profit?: number
  opened_at: ISODateString
  closed_at?: ISODateString
}

export interface PortfolioSummary {
  open_positions: number
  unrealized_pnl: number
  realized_pnl: number
}

export interface Order {
  id: UUID
  strategy_id?: UUID
  pipeline_run_id?: UUID
  external_id?: string
  ticker: string
  side: OrderSide
  order_type: OrderType
  quantity: number
  limit_price?: number
  stop_price?: number
  filled_quantity: number
  filled_avg_price?: number
  status: OrderStatus
  broker: string
  submitted_at?: ISODateString
  filled_at?: ISODateString
  created_at: ISODateString
}

export interface OrderDetails {
  order: Order
  fills: Trade[]
}

export interface Trade {
  id: UUID
  order_id?: UUID
  position_id?: UUID
  ticker: string
  side: OrderSide
  quantity: number
  price: number
  fee: number
  executed_at: ISODateString
  created_at: ISODateString
}

export interface AgentMemory {
  id: UUID
  agent_role: AgentRole
  situation: string
  recommendation: string
  outcome?: string
  pipeline_run_id?: UUID
  relevance_score?: number
  created_at: ISODateString
}

export interface CircuitBreakerStatus {
  state: CircuitBreakerPhase
  reason?: string
  tripped_at?: ISODateString
  cooldown_end?: ISODateString
}

export interface KillSwitchStatus {
  active: boolean
  reason?: string
  mechanisms?: KillSwitchMechanism[]
  activated_at?: ISODateString
}

export interface PositionLimits {
  max_per_position_pct: number
  max_total_pct: number
  max_concurrent: number
  max_per_market_pct: number
  current_open_positions?: number
  current_total_exposure_pct?: number
}

export interface EngineStatus {
  risk_status: RiskStatus
  circuit_breaker: CircuitBreakerStatus
  kill_switch: KillSwitchStatus
  market_kill_switches?: Record<string, KillSwitchStatus>
  position_limits: PositionLimits
  updated_at: ISODateString
}

export interface KillSwitchToggleRequest {
  active: boolean
  reason?: string
}

export interface KillSwitchToggleResponse {
  active: boolean
}

export interface LLMProviderSettings {
  api_key_configured?: boolean
  api_key_last4?: string
  base_url?: string
  model: string
}

export interface OllamaSettings {
  base_url?: string
  model: string
}

export interface LLMProviderSettingsGroup {
  openai: LLMProviderSettings
  anthropic: LLMProviderSettings
  google: LLMProviderSettings
  openrouter: LLMProviderSettings
  xai: LLMProviderSettings
  ollama: OllamaSettings
}

export interface Settings {
  llm: {
    default_provider: string
    deep_think_model: string
    quick_think_model: string
    providers: LLMProviderSettingsGroup
  }
  risk: {
    max_position_size_pct: number
    max_daily_loss_pct: number
    max_drawdown_pct: number
    max_open_positions: number
    max_total_exposure_pct: number
    max_per_market_exposure_pct: number
    circuit_breaker_threshold_pct: number
    circuit_breaker_cooldown_min: number
  }
  system: {
    environment: string
    version: string
    uptime_seconds: number
    connected_brokers: Array<{
      name: string
      paper_mode: boolean
      configured: boolean
    }>
  }
}

export interface LLMProviderUpdateRequest {
  api_key?: string
  base_url?: string
  model: string
}

export interface SettingsUpdateRequest {
  llm: {
    default_provider: string
    deep_think_model: string
    quick_think_model: string
    providers: {
      openai: LLMProviderUpdateRequest
      anthropic: LLMProviderUpdateRequest
      google: LLMProviderUpdateRequest
      openrouter: LLMProviderUpdateRequest
      xai: LLMProviderUpdateRequest
      ollama: OllamaSettings
    }
  }
  risk: Settings['risk']
}

export interface WebSocketMessage<TData = unknown> {
  type: WebSocketEventType
  strategy_id?: UUID
  run_id?: UUID
  data?: TData
  timestamp?: ISODateString
}

export interface PipelineErrorData {
  error: string
  timed_out?: boolean
  used_fallback?: boolean
}


export interface LoginRequest {
  username: string
  password: string
}

export interface LoginResponse {
  access_token: string
  refresh_token: string
  expires_at: ISODateString
}
export interface WebSocketAck {
  status: 'ok'
  action: 'subscribe' | 'unsubscribe' | 'subscribe_all' | 'unsubscribe_all'
}

export interface WebSocketCommandError {
  type: 'error'
  error: string
}

export type WebSocketServerMessage = WebSocketMessage | WebSocketAck | WebSocketCommandError

export interface WebSocketSubscriptionCommand {
  action: 'subscribe' | 'unsubscribe'
  strategy_ids?: UUID[]
  run_ids?: UUID[]
}

export interface AuditLogEntry {
  id: UUID
  event_type: string
  entity_type?: string
  entity_id?: UUID
  actor?: string
  details?: unknown
  created_at: ISODateString
}

export interface StrategyListParams {
  ticker?: string
  market_type?: MarketType
  status?: StrategyStatus
  is_active?: boolean
  is_paper?: boolean
}

export interface RunListParams {
  ticker?: string
  status?: PipelineStatus
  strategy_id?: UUID
  start_date?: ISODateString
  end_date?: ISODateString
}

export interface PositionListParams {
  ticker?: string
  side?: PositionSide
}

export interface OrderListParams {
  ticker?: string
  status?: OrderStatus
  side?: OrderSide
  strategy_id?: UUID
}

export interface TradeListParams {
  ticker?: string
  side?: OrderSide
  order_id?: UUID
  position_id?: UUID
}

export interface MemoryListParams {
  q?: string
  agent_role?: AgentRole
}

export interface ConversationListParams {
  pipeline_run_id?: UUID
  agent_role?: AgentRole
}

export interface PaginationParams {
  limit?: number
  offset?: number
}

export interface StrategyCreateRequest {
  name: string
  description?: string
  ticker: string
  market_type: MarketType
  schedule_cron?: string
  config?: StrategyConfigWire
  status?: StrategyStatus
  is_active?: boolean
  is_paper?: boolean
}

export interface StrategyUpdateRequest {
  name: string
  description?: string
  ticker: string
  market_type: MarketType
  schedule_cron?: string
  config?: StrategyConfigWire
  status: StrategyStatus
  is_active?: boolean
  is_paper: boolean
  skip_next_run?: boolean
}

export interface ConversationCreateRequest {
  pipeline_run_id: UUID
  agent_role: AgentRole
}

export interface ConversationMessageCreateRequest {
  content: string
}

// ---------- Backtests ----------

export interface BacktestSimulationParameters {
  initial_capital: number
  slippage_model?: unknown
  transaction_costs?: unknown
  spread_model?: unknown
  max_volume_pct?: number
}

export interface BacktestConfig {
  id: UUID
  strategy_id: UUID
  name: string
  description?: string
  schedule_cron?: string
  start_date: ISODateString
  end_date: ISODateString
  simulation: BacktestSimulationParameters
  created_at: ISODateString
  updated_at: ISODateString
}

export interface BacktestMetrics {
  total_return: number | string
  buy_and_hold_return: number | string
  max_drawdown: number | string
  sharpe_ratio: number | string
  sortino_ratio: number | string
  calmar_ratio: number | string
  alpha: number | string
  beta: number | string
  win_rate: number | string
  profit_factor: number | string
  avg_win_loss_ratio: number | string
  volatility: number | string
  start_equity: number | string
  end_equity: number | string
  total_bars: number
  realized_pnl: number | string
  unrealized_pnl: number | string
}

export interface EquityCurvePoint {
  timestamp: ISODateString
  cash: number
  market_value: number
  portfolio_value: number
  realized_pnl: number
  unrealized_pnl: number
  total_pnl: number
  peak_equity: number
  drawdown_pct: number
}

export interface BacktestRun {
  id: UUID
  backtest_config_id: UUID
  metrics: BacktestMetrics
  trade_log: unknown
  equity_curve: EquityCurvePoint[]
  run_timestamp: ISODateString
  duration: string
  prompt_version: string
  prompt_version_hash: string
  created_at: ISODateString
  updated_at: ISODateString
}

export interface BacktestConfigCreateRequest {
  strategy_id: UUID
  name: string
  description?: string
  start_date: ISODateString
  end_date: ISODateString
  simulation: BacktestSimulationParameters
}

export interface BacktestConfigListParams {
  strategy_id?: UUID
}

export interface BacktestRunListParams {
  backtest_config_id?: UUID
}

// ---------- Options ----------

export interface OptionContract {
  occ_symbol: string
  underlying: string
  option_type: 'call' | 'put'
  strike: number
  expiry: ISODateString
  multiplier: number
  style?: string
}

export interface OptionGreeks {
  delta: number
  gamma: number
  theta: number
  vega: number
  rho?: number
  iv: number
}

export interface OptionSnapshot {
  contract: OptionContract
  greeks: OptionGreeks
  bid: number
  ask: number
  mid: number
  last: number
  volume: number
  open_interest: number
}

// ---------- Discovery ----------

export interface DeployedStrategy {
  strategy_id: UUID
  ticker: string
  config: StrategyConfigWire
  in_sample: BacktestMetrics
  out_of_sample: BacktestMetrics
  score: number
}

export interface DiscoveryResult {
  candidates: number
  generated: number
  swept: number
  validated: number
  deployed: number
  winners: DeployedStrategy[] | null
  duration: number
  errors?: string[]
}

export interface DiscoveryRunRequest {
  tickers: string[]
  market_type?: string
  dry_run?: boolean
  max_winners?: number
}

// ---------- Automation ----------

export interface JobStatus {
  name: string
  description: string
  schedule: string
  last_run?: ISODateString
  last_result: string
  last_error?: string
  run_count: number
  error_count: number
  running: boolean
  enabled: boolean
}

// ---------- Universe ----------

export interface TrackedTicker {
  ticker: string
  name: string
  exchange: string
  index_group: string
  watch_score: number
  last_scanned?: ISODateString
  active: boolean
}

// ---------- Calendar ----------

export interface EarningsEvent {
  symbol: string
  date: ISODateString
  hour: string
  eps_estimate?: number
  eps_actual?: number
  revenue_estimate?: number
  revenue_actual?: number
  quarter: number
  year: number
}

export interface EconomicEvent {
  event: string
  country: string
  time: ISODateString
  impact: string
  estimate?: number
  actual?: number
  previous?: number
  unit: string
}

export interface SECFiling {
  symbol: string
  form: string
  filed_date: ISODateString
  accepted_date: ISODateString
  report_date: ISODateString
  url: string
  access_number: string
}

export interface IPOEvent {
  symbol: string
  date: ISODateString
  exchange: string
  name: string
  price_range: string
  shares_offered: number
  status: string
}

export interface FilingAnalysis {
  symbol: string
  form: string
  filed_date: ISODateString
  sentiment: string
  impact: string
  summary: string
  action: string
  confidence: number
  key_items: string[]
  reasoning: string
}

export interface AnalyzeFilingRequest {
  symbol: string
  form: string
  url: string
}

export interface ScoredTicker {
  ticker: string
  score: number
  reasons: string[]
  day_volume: number
  day_close: number
  change_pct: number
  gap_pct: number
}

// ---------- Signal Intelligence ----------

export interface StoredSignal {
  id: UUID
  received_at: ISODateString
  source: string
  title: string
  body: string
  urgency: number
  summary: string
  recommended_action: string
  affected_strategy_ids: UUID[]
  metadata?: Record<string, unknown>
}

export interface StoredTrigger {
  id: UUID
  fired_at: ISODateString
  strategy_id: UUID
  action: string
  priority: number
  signal_title: string
  signal_summary: string
  source: string
}

export interface WatchTerm {
  term: string
  source: 'auto' | 'manual'
  strategy_ids: UUID[]
}

export interface AddWatchTermRequest {
  term: string
  strategy_id?: UUID
}

// ---------- Automation Health ----------

export interface AutomationJobHealth {
  name: string
  enabled: boolean
  running: boolean
  last_run?: ISODateString
  last_error?: string
  error_count: number
  consecutive_failures: number
  run_count: number
}

export interface AutomationHealthResponse {
  jobs: AutomationJobHealth[]
  healthy: boolean
  total_jobs: number
  failing_jobs: number
}
