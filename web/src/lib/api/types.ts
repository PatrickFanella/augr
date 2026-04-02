export type UUID = string
export type ISODateString = string

export type MarketType = 'stock' | 'crypto' | 'polymarket'
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

export interface Strategy {
  id: UUID
  name: string
  description?: string
  ticker: string
  market_type: MarketType
  schedule_cron?: string
  config: unknown
  status: StrategyStatus
  skip_next_run: boolean
  is_active?: boolean
  is_paper: boolean
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
  config?: unknown
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
  config?: unknown
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
