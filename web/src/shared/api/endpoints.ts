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
  optionSnapshotSchema,
  backtestConfigSchema,
  backtestRunSchema,
  tradeDecisionSchema,
  replayDecisionSchema,
  copyLeaderSchema,
  copyLeaderSourceSchema,
  copyLeaderDetailSchema,
  copySubscriptionSchema,
  copyTradeIntentSchema,
  copyPreviewSchema,
  copyRefreshResultSchema,
  copyRebalanceResultSchema,
} from '@/shared/api/schemas'
import type { ListResponse, PortfolioSummary } from '@/shared/types/api'
import type { AuthResponse, LoginRequest } from '@/shared/types/auth'
import type { AgentDecision, AgentEvent, AllocationDecision, AllocatorDiagnostics, AllocatorOpportunity, AllocatorSummary, AutomationHealthResponse, AutomationJobRun, AutomationJobStatus, BacktestConfig, BacktestRun, BreakerResetRequest, BreakerResetResponse, CopyLeader, CopyLeaderDetail, CopyLeaderSource, CopyPreview, CopyRebalanceResult, CopyRefreshResult, CopySubscription, CopyTradeIntent, EventMarketsSummaryResponse, HealthStatusResponse, KillSwitchToggleRequest, KillSwitchToggleResponse, MarketKillSwitchRequest, MarketKillSwitchResponse, OptionSnapshot, Order, OrderDetailResponse, PipelineRun, PolymarketDataStatus, Position, ReplayDecision, ReportArtifact, ReportLatestResponse, RiskBreakersResponse, RiskCockpitSummary, RiskEngineStatus, RunSnapshot, Strategy, StrategyCreateRequest, StrategyRunAcceptedResponse, StrategyUpdateRequest, Trade, TradeDecision, User } from '@/shared/types/domain'
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

export function getOptionsChain(underlying: string, params: { expiry?: string; type?: string } = {}, signal?: AbortSignal): Promise<OptionSnapshot[]> {
  return api.get<OptionSnapshot[]>(`/options/chain/${encodeURIComponent(underlying)}${buildQuery(params)}`, { schema: optionSnapshotSchema.array() as never, signal })
}

export function getBacktestConfigs(params: { strategy_id?: string; limit?: number; offset?: number } = {}, signal?: AbortSignal): Promise<ListResponse<BacktestConfig>> {
  return api.get<ListResponse<BacktestConfig>>(`/backtests/configs${buildQuery(params)}`, { schema: listResponseSchema(backtestConfigSchema) as never, signal })
}

export function getBacktestRuns(params: { backtest_config_id?: string; limit?: number; offset?: number } = {}, signal?: AbortSignal): Promise<ListResponse<BacktestRun>> {
  return api.get<ListResponse<BacktestRun>>(`/backtests/runs${buildQuery(params)}`, { schema: listResponseSchema(backtestRunSchema) as never, signal })
}

export function getTradeDecisions(params: { strategy_id?: string; market_type?: string; status?: string; limit?: number; offset?: number } = {}, signal?: AbortSignal): Promise<ListResponse<TradeDecision>> {
  return api.get<ListResponse<TradeDecision>>(`/journal/decisions${buildQuery(params)}`, { schema: listResponseSchema(tradeDecisionSchema) as never, signal })
}

export function getDecisionReplay(id: string, signal?: AbortSignal): Promise<ReplayDecision> {
  return api.get<ReplayDecision>(`/replay/decisions/${encodeURIComponent(id)}`, { schema: replayDecisionSchema as never, signal })
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

export function getCopyLeaders(signal?: AbortSignal): Promise<ListResponse<CopyLeader>> {
  return api.get<ListResponse<CopyLeader>>('/copy-trading/leaders?limit=100&offset=0', { schema: listResponseSchema(copyLeaderSchema) as never, signal })
}

export function createCopyLeader(request: Pick<CopyLeader, 'entity_type' | 'display_name'> & { sec_cik?: string }, signal?: AbortSignal): Promise<CopyLeader> {
  return api.post<CopyLeader>('/copy-trading/leaders', request, { schema: copyLeaderSchema as never, signal, retryOnUnauthorized: false })
}

export function getCopyLeader(id: string, signal?: AbortSignal): Promise<CopyLeaderDetail> {
  return api.get<CopyLeaderDetail>(`/copy-trading/leaders/${encodeURIComponent(id)}`, { schema: copyLeaderDetailSchema as never, signal })
}

export function addCopySource(leaderId: string, request: Pick<CopyLeaderSource, 'provider' | 'source_type' | 'external_key'>, signal?: AbortSignal): Promise<CopyLeaderSource> {
  return api.post<CopyLeaderSource>(`/copy-trading/leaders/${encodeURIComponent(leaderId)}/sources`, request, { schema: copyLeaderSourceSchema as never, signal, retryOnUnauthorized: false })
}

export function refreshCopySource(sourceId: string, signal?: AbortSignal): Promise<CopyRefreshResult> {
  return api.post<CopyRefreshResult>(`/copy-trading/sources/${encodeURIComponent(sourceId)}/refresh`, undefined, { schema: copyRefreshResultSchema as never, signal, retryOnUnauthorized: false })
}

export function upsertCopyMapping(request: { provider: string; identifier_type: string; identifier_value: string; instrument_key?: string; ticker: string; confidence?: string; mapping_method?: string }, signal?: AbortSignal) {
  return api.put('/copy-trading/mappings', request, { signal, retryOnUnauthorized: false })
}

export function getCopySubscriptions(signal?: AbortSignal): Promise<ListResponse<CopySubscription>> {
  return api.get<ListResponse<CopySubscription>>('/copy-trading/subscriptions?limit=100&offset=0', { schema: listResponseSchema(copySubscriptionSchema) as never, signal })
}

export type CopySubscriptionCreateRequest = Pick<CopySubscription, 'leader_id' | 'source_id' | 'capital_budget' | 'cash_buffer_pct' | 'top_n' | 'min_source_weight' | 'max_position_weight' | 'max_turnover_pct' | 'min_price' | 'min_avg_dollar_volume' | 'max_spread_bps'> & { is_paper: true; method: 'target_weight'; stock_allowlist?: string[]; stock_blocklist?: string[] }

export function createCopySubscription(request: CopySubscriptionCreateRequest, signal?: AbortSignal): Promise<CopySubscription> {
  return api.post<CopySubscription>('/copy-trading/subscriptions', request, { schema: copySubscriptionSchema as never, signal, retryOnUnauthorized: false })
}

export function getCopySubscription(id: string, signal?: AbortSignal): Promise<CopySubscription> {
  return api.get<CopySubscription>(`/copy-trading/subscriptions/${encodeURIComponent(id)}`, { schema: copySubscriptionSchema as never, signal })
}

export function previewCopySubscription(id: string, signal?: AbortSignal): Promise<CopyPreview> {
  return api.post<CopyPreview>(`/copy-trading/subscriptions/${encodeURIComponent(id)}/preview`, undefined, { schema: copyPreviewSchema as never, signal, retryOnUnauthorized: false })
}

export function setCopySubscriptionStatus(id: string, action: 'activate' | 'pause' | 'resume' | 'stop', signal?: AbortSignal): Promise<CopySubscription> {
  return api.post<CopySubscription>(`/copy-trading/subscriptions/${encodeURIComponent(id)}/${action}`, undefined, { schema: copySubscriptionSchema as never, signal, retryOnUnauthorized: false })
}

export function rebalanceCopySubscription(id: string, signal?: AbortSignal): Promise<CopyRebalanceResult> {
  return api.post<CopyRebalanceResult>(`/copy-trading/subscriptions/${encodeURIComponent(id)}/rebalance`, undefined, { schema: copyRebalanceResultSchema as never, signal, retryOnUnauthorized: false })
}

export function getCopyIntents(id: string, signal?: AbortSignal): Promise<ListResponse<CopyTradeIntent>> {
  return api.get<ListResponse<CopyTradeIntent>>(`/copy-trading/subscriptions/${encodeURIComponent(id)}/intents?limit=100&offset=0`, { schema: listResponseSchema(copyTradeIntentSchema) as never, signal })
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
