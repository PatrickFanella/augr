import { api } from '@/shared/api/client'
import { listResponseSchema } from '@/shared/api/schemas'
import {
  authResponseSchema,
  allocationDecisionSchema,
  allocatorDiagnosticsSchema,
  allocatorOpportunitySchema,
  allocatorSummarySchema,
  agentDecisionSchema,
  agentEventSchema,
  automationJobRunSchema,
  automationJobStatusListSchema,
  automationHealthResponseSchema,
  healthStatusResponseSchema,
  orderDetailResponseSchema,
  orderSchema,
  pipelineRunSchema,
  portfolioSummarySchema,
  positionSchema,
  reportArtifactSchema,
  reportLatestResponseSchema,
  riskEngineStatusSchema,
  riskBreakersResponseSchema,
  riskCockpitSummarySchema,
  breakerResetRequestSchema,
  breakerResetResponseSchema,
  killSwitchToggleRequestSchema,
  killSwitchToggleResponseSchema,
  marketKillSwitchRequestSchema,
  marketKillSwitchResponseSchema,
  runSnapshotSchema,
  settingsResponseSchema,
  strategyCreateRequestSchema,
  strategyRunAcceptedResponseSchema,
  strategySchema,
  strategyUpdateRequestSchema,
  tradeSchema,
  userSchema,
  eventMarketsSummarySchema,
  polymarketDataStatusSchema,
} from '@/shared/api/schemas'
import type { ListResponse, PortfolioSummary } from '@/shared/types/api'
import type { AuthResponse, LoginRequest } from '@/shared/types/auth'
import type { AgentDecision, AgentEvent, AllocationDecision, AllocatorDiagnostics, AllocatorOpportunity, AllocatorSummary, AutomationHealthResponse, AutomationJobRun, AutomationJobStatus, BreakerResetRequest, BreakerResetResponse, EventMarketsSummaryResponse, HealthStatusResponse, KillSwitchToggleRequest, KillSwitchToggleResponse, MarketKillSwitchRequest, MarketKillSwitchResponse, Order, OrderDetailResponse, PipelineRun, PolymarketDataStatus, Position, ReportArtifact, ReportLatestResponse, RiskBreakersResponse, RiskCockpitSummary, RiskEngineStatus, RunSnapshot, Strategy, StrategyCreateRequest, StrategyRunAcceptedResponse, StrategyUpdateRequest, Trade, User } from '@/shared/types/domain'
import type { SettingsResponse } from '@/shared/types/settings'

export type StrategyListParams = {
  ticker?: string
  market_type?: string
  status?: string
  is_paper?: boolean
  limit?: number
  offset?: number
}

export type StrategyReportListParams = {
  report_type?: string
  status?: string
  limit?: number
  offset?: number
}

export type RunListParams = {
  status?: string
  strategy_id?: string
  ticker?: string
  start_date?: string
  end_date?: string
  limit?: number
  offset?: number
}

export type RunDecisionListParams = {
  include_prompt?: boolean
  agent_role?: string
  phase?: string
  limit?: number
  offset?: number
}

export type EventListParams = {
  event_kind?: string
  pipeline_run_id?: string
  strategy_id?: string
  agent_role?: string
  after?: string
  before?: string
  limit?: number
  offset?: number
}

export type PortfolioPositionListParams = {
  ticker?: string
  side?: string
  limit?: number
  offset?: number
}

export type AllocatorOpportunityListParams = {
  status?: string
  market_type?: string
  ticker?: string
  strategy_id?: string
  expires_before?: string
  created_after?: string
  limit?: number
  offset?: number
}

export type AllocationDecisionListParams = {
  mode?: string
  action?: string
  strategy_id?: string
  opportunity_id?: string
  created_after?: string
  limit?: number
  offset?: number
}

export type AutomationRunListParams = {
  limit?: number
  offset?: number
}

export type OrderListParams = {
  ticker?: string
  broker?: string
  market_type?: string
  status?: string
  side?: string
  order_type?: string
  limit?: number
  offset?: number
}

export type TradeListParams = {
  order_id?: string
  position_id?: string
  ticker?: string
  side?: string
  start_date?: string
  end_date?: string
  limit?: number
  offset?: number
}

function buildQuery(params: Record<string, string | number | boolean | undefined>) {
  const search = new URLSearchParams()
  for (const [key, value] of Object.entries(params)) {
    if (value === undefined || value === '') continue
    search.set(key, String(value))
  }
  const query = search.toString()
  return query ? `?${query}` : ''
}

export function login(request: LoginRequest, signal?: AbortSignal): Promise<AuthResponse> {
  return api.post<AuthResponse>('/auth/login', request, { schema: authResponseSchema as never, auth: false, signal })
}

export function getCurrentUser(signal?: AbortSignal): Promise<User> {
  return api.get<User>('/me', { schema: userSchema as never, signal })
}

export function getSettings(signal?: AbortSignal): Promise<SettingsResponse> {
  return api.get<SettingsResponse>('/settings', { schema: settingsResponseSchema as never, signal })
}

export function getEventMarketsSummary(signal?: AbortSignal): Promise<EventMarketsSummaryResponse> {
  return api.get<EventMarketsSummaryResponse>('/event-markets/summary', { schema: eventMarketsSummarySchema as never, signal })
}

export function getPolymarketDataStatus(signal?: AbortSignal): Promise<PolymarketDataStatus> {
  return api.get<PolymarketDataStatus>('/marketdata/polymarket/status', { schema: polymarketDataStatusSchema as never, signal })
}

export function getRiskStatus(signal?: AbortSignal): Promise<RiskEngineStatus> {
  return api.get<RiskEngineStatus>('/risk/status', { schema: riskEngineStatusSchema as never, signal })
}

export function getRiskCockpit(signal?: AbortSignal): Promise<RiskCockpitSummary> {
  return api.get<RiskCockpitSummary>('/risk/cockpit', { schema: riskCockpitSummarySchema as never, signal })
}

export function getRiskBreakers(signal?: AbortSignal): Promise<RiskBreakersResponse> {
  return api.get<RiskBreakersResponse>('/risk/breakers', { schema: riskBreakersResponseSchema as never, signal })
}

export function toggleKillSwitch(request: KillSwitchToggleRequest, adminKey?: string, signal?: AbortSignal): Promise<KillSwitchToggleResponse> {
  const parsed = killSwitchToggleRequestSchema.parse(request)
  return api.post<KillSwitchToggleResponse>('/risk/killswitch', parsed, {
    schema: killSwitchToggleResponseSchema as never,
    signal,
    retryOnUnauthorized: false,
    headers: adminKey ? { 'X-Admin-Key': adminKey } : undefined,
  })
}

export function stopMarketKillSwitch(marketType: string, request: MarketKillSwitchRequest, signal?: AbortSignal): Promise<MarketKillSwitchResponse> {
  const parsed = marketKillSwitchRequestSchema.parse(request)
  return api.post<MarketKillSwitchResponse>(`/risk/market/${encodeURIComponent(marketType)}/stop`, parsed, {
    schema: marketKillSwitchResponseSchema as never,
    signal,
    retryOnUnauthorized: false,
  })
}

export function resumeMarketKillSwitch(marketType: string, signal?: AbortSignal): Promise<MarketKillSwitchResponse> {
  return api.post<MarketKillSwitchResponse>(`/risk/market/${encodeURIComponent(marketType)}/resume`, undefined, {
    schema: marketKillSwitchResponseSchema as never,
    signal,
    retryOnUnauthorized: false,
  })
}

export function resetRiskBreaker(request: BreakerResetRequest, adminKey: string, signal?: AbortSignal): Promise<BreakerResetResponse> {
  const parsed = breakerResetRequestSchema.parse(request)
  return api.post<BreakerResetResponse>('/risk/breaker/reset', parsed, {
    schema: breakerResetResponseSchema as never,
    signal,
    retryOnUnauthorized: false,
    headers: { 'X-Admin-Key': adminKey },
  })
}

export function getPortfolioSummary(signal?: AbortSignal): Promise<PortfolioSummary> {
  return api.get<PortfolioSummary>('/portfolio/summary', { schema: portfolioSummarySchema as never, signal })
}

export function getPortfolioPositions(params: PortfolioPositionListParams = {}, signal?: AbortSignal): Promise<ListResponse<Position>> {
  return api.get<ListResponse<Position>>(`/portfolio/positions${buildQuery(params)}`, { schema: listResponseSchema(positionSchema) as never, signal })
}

export function getOpenPortfolioPositions(params: PortfolioPositionListParams = {}, signal?: AbortSignal): Promise<ListResponse<Position>> {
  return api.get<ListResponse<Position>>(`/portfolio/positions/open${buildQuery(params)}`, { schema: listResponseSchema(positionSchema) as never, signal })
}

export function getAllocatorDiagnostics(signal?: AbortSignal): Promise<AllocatorDiagnostics> {
  return api.get<AllocatorDiagnostics>('/portfolio/allocator/diagnostics', { schema: allocatorDiagnosticsSchema as never, signal })
}

export function getAllocatorSummary(signal?: AbortSignal): Promise<AllocatorSummary> {
  return api.get<AllocatorSummary>('/portfolio/allocator/summary', { schema: allocatorSummarySchema as never, signal })
}

export function getAllocatorOpportunities(params: AllocatorOpportunityListParams = {}, signal?: AbortSignal): Promise<ListResponse<AllocatorOpportunity>> {
  return api.get<ListResponse<AllocatorOpportunity>>(`/portfolio/allocator/opportunities${buildQuery(params)}`, { schema: listResponseSchema(allocatorOpportunitySchema) as never, signal })
}

export function getAllocationDecisions(params: AllocationDecisionListParams = {}, signal?: AbortSignal): Promise<ListResponse<AllocationDecision>> {
  return api.get<ListResponse<AllocationDecision>>(`/portfolio/allocator/decisions${buildQuery(params)}`, { schema: listResponseSchema(allocationDecisionSchema) as never, signal })
}

export function getRunningRuns(signal?: AbortSignal): Promise<ListResponse<PipelineRun>> {
  return api.get<ListResponse<PipelineRun>>('/runs?status=running', { schema: listResponseSchema(pipelineRunSchema) as never, signal })
}

export function getRuns(params: RunListParams = {}, signal?: AbortSignal): Promise<ListResponse<PipelineRun>> {
  return api.get<ListResponse<PipelineRun>>(`/runs${buildQuery(params)}`, { schema: listResponseSchema(pipelineRunSchema) as never, signal })
}

export function getRun(id: string, signal?: AbortSignal): Promise<PipelineRun> {
  return api.get<PipelineRun>(`/runs/${encodeURIComponent(id)}`, { schema: pipelineRunSchema as never, signal })
}

export function getRunDecisions(id: string, params: RunDecisionListParams = {}, signal?: AbortSignal): Promise<ListResponse<AgentDecision>> {
  return api.get<ListResponse<AgentDecision>>(`/runs/${encodeURIComponent(id)}/decisions${buildQuery(params)}`, { schema: listResponseSchema(agentDecisionSchema) as never, signal })
}

export function getRunSnapshot(id: string, signal?: AbortSignal): Promise<RunSnapshot> {
  return api.get<RunSnapshot>(`/runs/${encodeURIComponent(id)}/snapshot`, { schema: runSnapshotSchema as never, signal })
}

export function getEvents(params: EventListParams = {}, signal?: AbortSignal): Promise<ListResponse<AgentEvent>> {
  return api.get<ListResponse<AgentEvent>>(`/events${buildQuery(params)}`, { schema: listResponseSchema(agentEventSchema) as never, signal })
}

export function getOrders(params: OrderListParams = {}, signal?: AbortSignal): Promise<ListResponse<Order>> {
  return api.get<ListResponse<Order>>(`/orders${buildQuery(params)}`, { schema: listResponseSchema(orderSchema) as never, signal })
}

export function getOrder(id: string, signal?: AbortSignal): Promise<OrderDetailResponse> {
  return api.get<OrderDetailResponse>(`/orders/${encodeURIComponent(id)}`, { schema: orderDetailResponseSchema as never, signal })
}

export function getTrades(params: TradeListParams = {}, signal?: AbortSignal): Promise<ListResponse<Trade>> {
  return api.get<ListResponse<Trade>>(`/trades${buildQuery(params)}`, { schema: listResponseSchema(tradeSchema) as never, signal })
}

export function getAutomationHealth(signal?: AbortSignal): Promise<AutomationHealthResponse> {
  return api.get<AutomationHealthResponse>('/automation/health', { schema: automationHealthResponseSchema as never, signal })
}

export function getAutomationStatus(signal?: AbortSignal): Promise<AutomationJobStatus[]> {
  return api.get<AutomationJobStatus[]>('/automation/status', { schema: automationJobStatusListSchema as never, signal })
}

export function getAutomationRuns(params: AutomationRunListParams = {}, signal?: AbortSignal): Promise<ListResponse<AutomationJobRun>> {
  return api.get<ListResponse<AutomationJobRun>>(`/automation/runs${buildQuery(params)}`, { schema: listResponseSchema(automationJobRunSchema) as never, signal })
}

export function runAutomationJob(name: string, signal?: AbortSignal): Promise<{ status: string }> {
  return api.post<{ status: string }>(`/automation/jobs/${encodeURIComponent(name)}/run`, undefined, { signal, retryOnUnauthorized: false })
}

export function setAutomationJobEnabled(name: string, enabled: boolean, signal?: AbortSignal): Promise<{ enabled: boolean }> {
  return api.post<{ enabled: boolean }>(`/automation/jobs/${encodeURIComponent(name)}/enable`, { enabled }, { signal, retryOnUnauthorized: false })
}

export function getHealth(signal?: AbortSignal): Promise<HealthStatusResponse> {
  return api.get<HealthStatusResponse>('/health', { schema: healthStatusResponseSchema as never, signal, auth: false })
}

export function getStrategy(id: string, signal?: AbortSignal): Promise<Strategy> {
  return api.get<Strategy>(`/strategies/${encodeURIComponent(id)}`, { schema: strategySchema as never, signal })
}

export function getStrategies(params: StrategyListParams = {}, signal?: AbortSignal): Promise<ListResponse<Strategy>> {
  return api.get<ListResponse<Strategy>>(`/strategies${buildQuery(params)}`, { schema: listResponseSchema(strategySchema) as never, signal })
}

export function createStrategy(request: StrategyCreateRequest, signal?: AbortSignal): Promise<Strategy> {
  const parsed = strategyCreateRequestSchema.parse(request)
  return api.post<Strategy>('/strategies', parsed, { schema: strategySchema as never, signal, retryOnUnauthorized: false })
}

export function updateStrategy(id: string, request: StrategyUpdateRequest, signal?: AbortSignal): Promise<Strategy> {
  const parsed = strategyUpdateRequestSchema.parse(request)
  return api.put<Strategy>(`/strategies/${encodeURIComponent(id)}`, parsed, { schema: strategySchema as never, signal, retryOnUnauthorized: false })
}

export function deleteStrategy(id: string, signal?: AbortSignal): Promise<void> {
  return api.delete<void>(`/strategies/${encodeURIComponent(id)}`, { signal, retryOnUnauthorized: false })
}

export function getLatestStrategyReport(id: string, reportType = 'paper_validation', signal?: AbortSignal): Promise<ReportLatestResponse> {
  return api.get<ReportLatestResponse>(`/strategies/${encodeURIComponent(id)}/reports/latest${buildQuery({ report_type: reportType })}`, { schema: reportLatestResponseSchema as never, signal })
}

export function getStrategyReports(id: string, params: StrategyReportListParams = {}, signal?: AbortSignal): Promise<ListResponse<ReportArtifact>> {
  return api.get<ListResponse<ReportArtifact>>(`/strategies/${encodeURIComponent(id)}/reports${buildQuery(params)}`, { schema: listResponseSchema(reportArtifactSchema) as never, signal })
}

export function pauseStrategy(id: string, signal?: AbortSignal): Promise<Strategy> {
  return api.post<Strategy>(`/strategies/${encodeURIComponent(id)}/pause`, undefined, { schema: strategySchema as never, signal, retryOnUnauthorized: false })
}

export function resumeStrategy(id: string, signal?: AbortSignal): Promise<Strategy> {
  return api.post<Strategy>(`/strategies/${encodeURIComponent(id)}/resume`, undefined, { schema: strategySchema as never, signal, retryOnUnauthorized: false })
}

export function skipNextStrategy(id: string, signal?: AbortSignal): Promise<Strategy> {
  return api.post<Strategy>(`/strategies/${encodeURIComponent(id)}/skip-next`, undefined, { schema: strategySchema as never, signal, retryOnUnauthorized: false })
}

export function runStrategy(id: string, signal?: AbortSignal): Promise<StrategyRunAcceptedResponse> {
  return api.post<StrategyRunAcceptedResponse>(`/strategies/${encodeURIComponent(id)}/run`, undefined, { schema: strategyRunAcceptedResponseSchema as never, signal, retryOnUnauthorized: false })
}
