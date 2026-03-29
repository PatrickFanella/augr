import { getApiBaseUrl } from '@/lib/config'
import type {
  AgentDecision,
  AgentMemory,
  EngineStatus,
  ErrorResponse,
  HealthStatus,
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
  Strategy,
  StrategyListParams,
  StrategyRunResult,
  Trade,
  TradeListParams,
  UUID,
} from '@/lib/api/types'

interface ApiClientConfig {
  baseUrl?: string
  token?: string
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
  private readonly apiKey?: string
  private readonly defaultHeaders?: HeadersInit

  constructor(config: ApiClientConfig = {}) {
    this.baseUrl = (config.baseUrl || getApiBaseUrl()).replace(/\/$/, '')
    this.token = config.token
    this.apiKey = config.apiKey
    this.defaultHeaders = config.headers
  }

  async health() {
    return this.request<HealthStatus>('/health')
  }

  async listStrategies(params: StrategyListParams & PaginationParams = {}) {
    return this.request<ListResponse<Strategy>>('/api/v1/strategies', { query: toQueryParams(params) })
  }

  async getStrategy(id: UUID) {
    return this.request<Strategy>(`/api/v1/strategies/${id}`)
  }

  async runStrategy(id: UUID) {
    return this.request<StrategyRunResult>(`/api/v1/strategies/${id}/run`, { method: 'POST' })
  }

  async listRuns(params: RunListParams & PaginationParams = {}) {
    return this.request<ListResponse<PipelineRun>>('/api/v1/runs', { query: toQueryParams(params) })
  }

  async getRun(id: UUID) {
    return this.request<PipelineRun>(`/api/v1/runs/${id}`)
  }

  async getRunDecisions(id: UUID, params: PaginationParams = {}) {
    return this.request<ListResponse<AgentDecision>>(`/api/v1/runs/${id}/decisions`, {
      query: toQueryParams(params),
    })
  }

  async cancelRun(id: UUID) {
    return this.request<{ status: 'cancelled' }>(`/api/v1/runs/${id}/cancel`, { method: 'POST' })
  }

  async listPositions(params: PositionListParams & PaginationParams = {}) {
    return this.request<ListResponse<Position>>('/api/v1/portfolio/positions', {
      query: toQueryParams(params),
    })
  }

  async getOpenPositions(params: PaginationParams = {}) {
    return this.request<ListResponse<Position>>('/api/v1/portfolio/positions/open', {
      query: toQueryParams(params),
    })
  }

  async getPortfolioSummary() {
    return this.request<PortfolioSummary>('/api/v1/portfolio/summary')
  }

  async listOrders(params: OrderListParams & PaginationParams = {}) {
    return this.request<ListResponse<Order>>('/api/v1/orders', { query: toQueryParams(params) })
  }

  async getOrder(id: UUID) {
    return this.request<OrderDetails>(`/api/v1/orders/${id}`)
  }

  async listTrades(params: TradeListParams & PaginationParams = {}) {
    return this.request<ListResponse<Trade>>('/api/v1/trades', { query: toQueryParams(params) })
  }

  async listMemories(params: MemoryListParams & PaginationParams = {}) {
    return this.request<ListResponse<AgentMemory>>('/api/v1/memories', {
      query: toQueryParams(params),
    })
  }

  async searchMemories(query: string, params: PaginationParams = {}) {
    return this.request<ListResponse<AgentMemory>>('/api/v1/memories/search', {
      method: 'POST',
      body: { query },
      query: toQueryParams(params),
    })
  }

  async deleteMemory(id: UUID) {
    return this.request<void>(`/api/v1/memories/${id}`, { method: 'DELETE' })
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

  private async request<T>(path: string, options: RequestOptions = {}): Promise<T> {
    const url = new URL(`${this.baseUrl}${path}`)

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
    if (this.token) {
      headers.set('Authorization', `Bearer ${this.token}`)
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

    if (response.status === 204) {
      return undefined as T
    }

    return (await response.json()) as T
  }
}

export const apiClient = new ApiClient()

function toQueryParams(params: object): QueryParams {
  return params as QueryParams
}
