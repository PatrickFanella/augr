import { getAccessToken, getRefreshToken, getExpiresAt, clearTokens, setTokens } from '@/lib/auth'
import { getApiBaseUrl } from '@/lib/config'
import type {
  AddWatchTermRequest,
  AgentDecision,
  AgentEvent,
  AgentMemory,
  AnalyzeFilingRequest,
  AuditLogEntry,
  AutomationHealthResponse,
  EarningsEvent,
  EconomicEvent,
  EngineStatus,
  ErrorResponse,
  FilingAnalysis,
  HealthStatus,
  IPOEvent,
  LoginRequest,
  LoginResponse,
  KillSwitchToggleRequest,
  KillSwitchToggleResponse,
  ListResponse,
  MemoryListParams,
  Order,
  OrderDetails,
  OrderListParams,
  PaginationParams,
  PipelineRun,
  Position,
  PositionListParams,
  PortfolioSummary,
  RunListParams,
  ScoredTicker,
  SECFiling,
  Settings,
  SettingsUpdateRequest,
  StoredSignal,
  StoredTrigger,
  Strategy,
  StrategyCreateRequest,
  StrategyListParams,
  StrategyRunResult,
  StrategyUpdateRequest,
  TrackedTicker,
  Trade,
  TradeListParams,
  UUID,
  WatchTerm,
  Conversation,
  ConversationCreateRequest,
  ConversationListParams,
  ConversationMessage,
  ConversationMessageCreateRequest,
  BacktestConfig,
  BacktestConfigCreateRequest,
  BacktestConfigListParams,
  BacktestRun,
  BacktestRunListParams,
  OptionSnapshot,
  DiscoveryRunRequest,
  DiscoveryResult,
  JobStatus,
} from '@/lib/api/types'

interface ApiClientConfig {
  baseUrl?: string
  token?: string
  tokenGetter?: () => string | null
  apiKey?: string
  headers?: HeadersInit
}

type QueryValue = string | number | boolean | undefined
type QueryParams = Record<string, QueryValue>

interface RequestOptions extends Omit<RequestInit, 'body' | 'headers'> {
  body?: unknown
  headers?: HeadersInit
  query?: QueryParams
}

type NullableListResponse<T> = Omit<ListResponse<T>, 'data'> & {
  data?: T[] | null
}

export class ApiClientError extends Error {
  readonly status: number
  readonly code?: string

  constructor(message: string, status: number, code?: string) {
    super(message)
    this.name = 'ApiClientError'
    this.status = status
    this.code = code
  }
}

export class ApiClient {
  private readonly baseUrl: string
  private readonly token?: string
  private readonly tokenGetter?: () => string | null
  private readonly apiKey?: string
  private readonly defaultHeaders?: HeadersInit
  private refreshPromise: Promise<void> | null = null

  constructor(config: ApiClientConfig = {}) {
    this.baseUrl = (config.baseUrl || getApiBaseUrl()).replace(/\/$/, '')
    this.token = config.token
    this.tokenGetter = config.tokenGetter
    this.apiKey = config.apiKey
    this.defaultHeaders = config.headers
  }

  async health() {
    return this.request<HealthStatus>('/health')
  }

  async login(data: LoginRequest) {
    return this.request<LoginResponse>('/api/v1/auth/login', { method: 'POST', body: data })
  }

  async listStrategies(params: StrategyListParams & PaginationParams = {}) {
    return this.requestList<Strategy>('/api/v1/strategies', { query: toQueryParams(params) })
  }

  async getStrategy(id: UUID) {
    return this.request<Strategy>(`/api/v1/strategies/${id}`)
  }

  async createStrategy(data: StrategyCreateRequest) {
    return this.request<Strategy>('/api/v1/strategies', { method: 'POST', body: data })
  }

  async updateStrategy(id: UUID, data: StrategyUpdateRequest) {
    return this.request<Strategy>(`/api/v1/strategies/${id}`, { method: 'PUT', body: data })
  }

  async deleteStrategy(id: UUID) {
    return this.requestNoContent(`/api/v1/strategies/${id}`, { method: 'DELETE' })
  }

  async pauseStrategy(id: UUID) {
    return this.request<Strategy>(`/api/v1/strategies/${id}/pause`, { method: 'POST' })
  }

  async resumeStrategy(id: UUID) {
    return this.request<Strategy>(`/api/v1/strategies/${id}/resume`, { method: 'POST' })
  }

  async skipNextRun(id: UUID) {
    return this.request<Strategy>(`/api/v1/strategies/${id}/skip-next`, { method: 'POST' })
  }

  async runStrategy(id: UUID) {
    return this.request<StrategyRunResult>(`/api/v1/strategies/${id}/run`, { method: 'POST' })
  }

  async listRuns(params: RunListParams & PaginationParams = {}) {
    return this.requestList<PipelineRun>('/api/v1/runs', { query: toQueryParams(params) })
  }

  async getRun(id: UUID) {
    return this.request<PipelineRun>(`/api/v1/runs/${id}`)
  }

  async getRunDecisions(id: UUID, params: PaginationParams = {}) {
    return this.requestList<AgentDecision>(`/api/v1/runs/${id}/decisions`, {
      query: toQueryParams(params),
    })
  }

  async cancelRun(id: UUID) {
    return this.request<{ status: 'cancelled' }>(`/api/v1/runs/${id}/cancel`, { method: 'POST' })
  }

  async listPositions(params: PositionListParams & PaginationParams = {}) {
    return this.requestList<Position>('/api/v1/portfolio/positions', {
      query: toQueryParams(params),
    })
  }

  async getOpenPositions(params: PaginationParams = {}) {
    return this.requestList<Position>('/api/v1/portfolio/positions/open', {
      query: toQueryParams(params),
    })
  }

  async getPortfolioSummary() {
    return this.request<PortfolioSummary>('/api/v1/portfolio/summary')
  }

  async listOrders(params: OrderListParams & PaginationParams = {}) {
    return this.requestList<Order>('/api/v1/orders', { query: toQueryParams(params) })
  }

  async getOrder(id: UUID) {
    return this.request<OrderDetails>(`/api/v1/orders/${id}`)
  }

  async listTrades(params: TradeListParams & PaginationParams = {}) {
    return this.requestList<Trade>('/api/v1/trades', { query: toQueryParams(params) })
  }

  async listEvents(params: PaginationParams & { run_id?: UUID; event_kind?: string } = {}) {
    return this.requestList<AgentEvent>('/api/v1/events', { query: toQueryParams(params) })
  }

  async listConversations(params: ConversationListParams & PaginationParams = {}) {
    return this.requestList<Conversation>('/api/v1/conversations', { query: toQueryParams(params) })
  }

  async createConversation(payload: ConversationCreateRequest) {
    return this.request<Conversation>('/api/v1/conversations', { method: 'POST', body: payload })
  }

  async getConversationMessages(id: UUID, params: PaginationParams = {}) {
    return this.requestList<ConversationMessage>(`/api/v1/conversations/${id}/messages`, {
      query: toQueryParams(params),
    })
  }

  async createConversationMessage(id: UUID, payload: ConversationMessageCreateRequest) {
    return this.request<ConversationMessage>(`/api/v1/conversations/${id}/messages`, {
      method: 'POST',
      body: payload,
    })
  }

  async listMemories(params: MemoryListParams & PaginationParams = {}) {
    return this.requestList<AgentMemory>('/api/v1/memories', {
      query: toQueryParams(params),
    })
  }

  async searchMemories(query: string, params: PaginationParams = {}) {
    return this.requestList<AgentMemory>('/api/v1/memories/search', {
      method: 'POST',
      body: { query },
      query: toQueryParams(params),
    })
  }

  async deleteMemory(id: UUID) {
    return this.requestNoContent(`/api/v1/memories/${id}`, { method: 'DELETE' })
  }

  async getRiskStatus() {
    return this.request<EngineStatus>('/api/v1/risk/status')
  }

  async toggleKillSwitch(payload: KillSwitchToggleRequest) {
    return this.request<KillSwitchToggleResponse>('/api/v1/risk/killswitch', {
      method: 'POST',
      body: payload,
    })
  }

  async toggleMarketKillSwitch(marketType: string, active: boolean, reason?: string) {
    const action = active ? 'stop' : 'resume'
    return this.requestNoContent(`/api/v1/risk/market/${marketType}/${action}`, {
      method: 'POST',
      body: active ? { reason: reason ?? `${marketType} trading halted from dashboard` } : undefined,
    })
  }

  async getSettings() {
    return this.request<Settings>('/api/v1/settings')
  }

  async updateSettings(payload: SettingsUpdateRequest) {
    return this.request<Settings>('/api/v1/settings', {
      method: 'PUT',
      body: payload,
    })
  }

  async listAuditLog(params: PaginationParams = {}) {
    return this.requestList<AuditLogEntry>('/api/v1/audit-log', { query: toQueryParams(params) })
  }

  private async request<T>(path: string, options: RequestOptions = {}): Promise<T> {
    const url = new URL(`${this.baseUrl}${path}`)

    const response = await this.fetch(url, options)
    return (await response.json()) as T
  }

  private async requestList<T>(path: string, options: RequestOptions = {}) {
    return normalizeListResponse(await this.request<NullableListResponse<T>>(path, options))
  }

  private async requestNoContent(path: string, options: RequestOptions = {}) {
    const url = new URL(`${this.baseUrl}${path}`)
    await this.fetch(url, options)
  }

  private async fetch(url: URL, options: RequestOptions) {
    await this.ensureFreshToken()

    if (options.query) {
      for (const [key, value] of Object.entries(options.query)) {
        if (value !== undefined) {
          url.searchParams.set(key, String(value))
        }
      }
    }

    const headers = new Headers(this.defaultHeaders)
    if (options.headers) {
      new Headers(options.headers).forEach((value, key) => headers.set(key, value))
    }
    const token = this.token ?? this.tokenGetter?.()
    if (token) {
      headers.set('Authorization', `Bearer ${token}`)
    }
    if (this.apiKey) {
      headers.set('X-API-Key', this.apiKey)
    }
    if (options.body !== undefined) {
      headers.set('Content-Type', 'application/json')
    }

    const response = await fetch(url, {
      ...options,
      headers,
      body: options.body === undefined ? undefined : JSON.stringify(options.body),
    })

    if (!response.ok) {
      if (response.status === 401) {
        clearTokens()
        this.redirectToLogin()
      }

      let payload: ErrorResponse | undefined
      try {
        payload = (await response.json()) as ErrorResponse
      } catch {
        payload = undefined
      }

      throw new ApiClientError(
        payload?.error ?? `Request failed with status ${response.status}`,
        response.status,
        payload?.code,
      )
    }

    return response
  }

  private async ensureFreshToken(): Promise<void> {
    // Skip if no tokenGetter (means static token or API key auth)
    if (!this.tokenGetter) return

    const expiresAt = getExpiresAt()
    if (!expiresAt) return

    // If token expires in more than 60 seconds, it's fresh enough
    if (expiresAt - Date.now() > 60_000) return

    // Prevent concurrent refresh attempts
    if (this.refreshPromise) {
      await this.refreshPromise
      return
    }

    const refreshToken = getRefreshToken()
    if (!refreshToken) {
      clearTokens()
      this.redirectToLogin()
      return
    }

    this.refreshPromise = (async () => {
      try {
        const url = new URL(`${this.baseUrl}/api/v1/auth/refresh`)
        const response = await fetch(url, {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ refresh_token: refreshToken }),
        })

        if (!response.ok) {
          clearTokens()
          this.redirectToLogin()
          return
        }

        const data = await response.json() as { access_token: string; refresh_token: string; expires_at: string | number }
        const expiresAt =
          typeof data.expires_at === 'string' ? Date.parse(data.expires_at) : data.expires_at

        if (Number.isNaN(expiresAt)) {
          throw new Error('Invalid expires_at value in refresh response')
        }

        setTokens(data.access_token, data.refresh_token, expiresAt)
      } catch {
        clearTokens()
        this.redirectToLogin()
      }
    })()

    try {
      await this.refreshPromise
    } finally {
      this.refreshPromise = null
    }
  }

  private redirectToLogin(): void {
    if (typeof window !== 'undefined') {
      window.location.href = '/login'
    }
  }

  // Backtest Configs
  async listBacktestConfigs(params: BacktestConfigListParams & PaginationParams = {}) {
    return this.requestList<BacktestConfig>('/api/v1/backtests/configs', {
      query: toQueryParams(params),
    })
  }

  async createBacktestConfig(data: BacktestConfigCreateRequest) {
    return this.request<BacktestConfig>('/api/v1/backtests/configs', {
      method: 'POST',
      body: data,
    })
  }

  async getBacktestConfig(id: UUID) {
    return this.request<BacktestConfig>(`/api/v1/backtests/configs/${id}`)
  }

  async updateBacktestConfig(id: UUID, data: BacktestConfigCreateRequest) {
    return this.request<BacktestConfig>(`/api/v1/backtests/configs/${id}`, {
      method: 'PUT',
      body: data,
    })
  }

  async deleteBacktestConfig(id: UUID) {
    return this.requestNoContent(`/api/v1/backtests/configs/${id}`, { method: 'DELETE' })
  }

  async runBacktestConfig(id: UUID) {
    return this.request<BacktestRun>(`/api/v1/backtests/configs/${id}/run`, { method: 'POST' })
  }

  // Backtest Runs
  async listBacktestRuns(params: BacktestRunListParams & PaginationParams = {}) {
    return this.requestList<BacktestRun>('/api/v1/backtests/runs', {
      query: toQueryParams(params),
    })
  }

  async getBacktestRun(id: UUID) {
    return this.request<BacktestRun>(`/api/v1/backtests/runs/${id}`)
  }

  // Options
  async getOptionsChain(underlying: string, params: { expiry?: string; type?: string } = {}) {
    return this.request<OptionSnapshot[]>(`/api/v1/options/chain/${underlying}`, {
      query: toQueryParams(params),
    })
  }

  // Discovery
  async runDiscovery(data: DiscoveryRunRequest) {
    return this.request<DiscoveryResult>('/api/v1/discovery/run', {
      method: 'POST',
      body: data,
    })
  }

  // Universe
  async listUniverse(params: { index_group?: string; search?: string } & PaginationParams = {}) {
    return this.requestList<TrackedTicker>('/api/v1/universe', { query: toQueryParams(params) })
  }

  async getWatchlist(top: number = 30) {
    return this.request<ScoredTicker[]>('/api/v1/universe/watchlist', { query: { top } })
  }

  async refreshUniverse() {
    return this.request<{ count: number }>('/api/v1/universe/refresh', { method: 'POST' })
  }

  async runPreMarketScan() {
    return this.request<ScoredTicker[]>('/api/v1/universe/scan', { method: 'POST' })
  }

  // Automation
  async getAutomationStatus() {
    return this.request<JobStatus[]>('/api/v1/automation/status')
  }

  async getAutomationHealth() {
    return this.request<AutomationHealthResponse>('/api/v1/automation/health')
  }

  async runAutomationJob(name: string) {
    return this.requestNoContent(`/api/v1/automation/jobs/${name}/run`, { method: 'POST' })
  }

  async setAutomationJobEnabled(name: string, enabled: boolean) {
    return this.requestNoContent(`/api/v1/automation/jobs/${name}/enable`, {
      method: 'POST',
      body: { enabled },
    })
  }

  // Calendar
  async getEarningsCalendar(params: { from?: string; to?: string } = {}) {
    return this.request<EarningsEvent[]>('/api/v1/calendar/earnings', { query: toQueryParams(params) })
  }

  async getEconomicCalendar() {
    return this.request<EconomicEvent[]>('/api/v1/calendar/economic')
  }

  async getFilings(params: { ticker?: string; form?: string } = {}) {
    return this.request<SECFiling[]>('/api/v1/calendar/filings', { query: toQueryParams(params) })
  }

  async analyzeFiling(data: AnalyzeFilingRequest) {
    return this.request<FilingAnalysis>('/api/v1/calendar/filings/analyze', {
      method: 'POST',
      body: data,
    })
  }

  async getIPOCalendar(params: { from?: string; to?: string } = {}) {
    return this.request<IPOEvent[]>('/api/v1/calendar/ipo', { query: toQueryParams(params) })
  }

  // Signal Intelligence
  async listEvaluatedSignals(params: { min_urgency?: number; limit?: number; offset?: number } = {}) {
    return this.request<{ data: StoredSignal[]; total: number }>('/api/v1/signals/evaluated', { query: toQueryParams(params) })
  }

  async listTriggerLog(params: { limit?: number; offset?: number } = {}) {
    return this.request<{ data: StoredTrigger[]; total: number }>('/api/v1/signals/triggers', { query: toQueryParams(params) })
  }

  async listWatchTerms() {
    return this.request<{ data: WatchTerm[] }>('/api/v1/signals/watchlist')
  }

  async addWatchTerm(req: AddWatchTermRequest) {
    return this.request<{ term: string }>('/api/v1/signals/watchlist', { method: 'POST', body: req })
  }

  async deleteWatchTerm(term: string) {
    return this.requestNoContent(`/api/v1/signals/watchlist/${encodeURIComponent(term)}`, { method: 'DELETE' })
  }
}

export const apiClient = new ApiClient({ tokenGetter: getAccessToken })

function normalizeListResponse<T>(response: NullableListResponse<T>): ListResponse<T> {
  const { data, ...rest } = response

  if (data == null) {
    return { ...rest, data: [] }
  }

  return { ...rest, data }
}

function toQueryParams(params: object): QueryParams {
  const queryParams: QueryParams = {}

  for (const [key, value] of Object.entries(params)) {
    if (
      value !== undefined &&
      (typeof value === 'string' || typeof value === 'number' || typeof value === 'boolean')
    ) {
      queryParams[key] = value
    }
  }

  return queryParams
}
