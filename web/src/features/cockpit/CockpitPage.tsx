import { useEffect, useId, type ReactNode } from 'react'
import { useQuery, type UseQueryResult } from '@tanstack/react-query'
import { RefreshCw } from 'lucide-react'
import { AreaChart, Area, ResponsiveContainer, Tooltip, PieChart, Pie, Cell } from 'recharts'

import { PageHeader } from '@/components/ui/page-header'
import { getAutomationHealth, getHealth, getOpenPortfolioPositions, getOrders, getPortfolioSummary, getRiskBreakers, getRiskCockpit, getRiskStatus, getRunningRuns, getTrades } from '@/shared/api/endpoints'
import { isApiClientError } from '@/shared/api/errors'
import { Breadcrumbs, EntityLink } from '@/shared/components/EntityLinks'
import { ErrorState, LastUpdated, LoadingState, StaleBanner } from '@/shared/components/QueryStates'
import { getChartColors } from '@/lib/chart-theme'
import { queryKeys } from '@/shared/query/keys'
import type { HealthStatusResponse, Order, PipelineRun, Position, RiskBreakersResponse, RiskCockpitSummary, RiskEngineStatus, Trade } from '@/shared/types/domain'
import { useRealtime } from '@/shared/websocket/RealtimeProvider'

type CockpitClassification = 'safe' | 'degraded' | 'unknown'

function statusClass(status: string) {
  const normalized = status.toLowerCase()
  if (['ok', 'safe', 'normal', 'closed', 'connected', 'healthy'].includes(normalized)) return 'success'
  if (['unknown', 'unavailable', 'not_configured'].includes(normalized)) return 'unknown'
  return 'warning'
}

function pnlClass(value: number) {
  if (value > 0) return 'success'
  if (value < 0) return 'warning'
  return 'unknown'
}

function formatCurrency(value: number) {
  return value.toLocaleString(undefined, { minimumFractionDigits: 2, maximumFractionDigits: 2 })
}

function QueryPanel<T>({ title, query, children, wide = false }: { title: string; query: UseQueryResult<T, Error>; children: (data: T) => ReactNode; wide?: boolean }) {
  const titleId = useId()
  return (
    <section className={`panel ${wide ? 'wide-panel' : ''}`} aria-labelledby={titleId}>
      <div className="panel-header">
        <h2 id={titleId}>{title}</h2>
        {query.isError ? (
          <button type="button" onClick={() => void query.refetch()}><RefreshCw size={14} /> Reload</button>
        ) : (
          <button type="button" className="btn-icon" onClick={() => void query.refetch()} aria-label="Reload"><RefreshCw size={14} /></button>
        )}
      </div>
      {query.isLoading ? <LoadingState label={`Loading ${title.toLowerCase()}…`} /> : null}
      {query.isError ? <ErrorState error={query.error} onRetry={() => void query.refetch()} /> : null}
      {query.data ? children(query.data) : null}
      <LastUpdated date={query.dataUpdatedAt || undefined} />
    </section>
  )
}

function classifyCockpit({
  risk,
  cockpit,
  breakers,
  health,
  automationHealthy,
  realtimeStatus,
  hasWidgetError,
}: {
  risk?: RiskEngineStatus
  cockpit?: RiskCockpitSummary
  breakers?: RiskBreakersResponse
  health?: HealthStatusResponse
  automationHealthy?: boolean
  realtimeStatus: string
  hasWidgetError: boolean
}): CockpitClassification {
  if (!risk || !cockpit || !breakers) return 'unknown'
  if (hasWidgetError) return 'degraded'
  if (realtimeStatus !== 'connected') return 'degraded'
  if (health && health.status !== 'ok') return 'degraded'
  if (automationHealthy === false) return 'degraded'
  if (risk.kill_switch.active || !['closed', 'open'].includes(risk.circuit_breaker.state.toLowerCase()) || risk.risk_status !== 'normal') return 'degraded'
  if (cockpit.kill_switch_active || cockpit.circuit_breaker || cockpit.warnings.length > 0) return 'degraded'
  if (breakers.tripped.some((breaker) => !breaker.reset_at)) return 'degraded'
  return 'safe'
}

function classificationCopy(classification: CockpitClassification) {
  if (classification === 'safe') return 'Safe: core risk, infrastructure, and cockpit widgets report normal state.'
  if (classification === 'degraded') return 'Degraded: at least one cockpit signal needs operator review.'
  return 'Unknown: risk cockpit data is unavailable or still loading.'
}

function RecentRuns({ runs }: { runs: PipelineRun[] }) {
  if (runs.length === 0) return <p>No active runs.</p>
  return (
    <table aria-label="active runs">
      <thead><tr><th>Ticker</th><th>Status</th><th>Run</th><th>Strategy</th><th>Started</th></tr></thead>
      <tbody>{runs.slice(0, 5).map((run) => (
        <tr key={run.id}>
          <td>{run.ticker}</td>
          <td><span className={`status-pill ${statusClass(run.status)}`}>{run.status}</span></td>
          <td><EntityLink kind="run" id={run.id} /></td>
          <td><EntityLink kind="strategy" id={run.strategy_id} /></td>
          <td>{new Date(run.started_at).toLocaleString()}</td>
        </tr>
      ))}</tbody>
    </table>
  )
}

function OpenPositions({ positions }: { positions: Position[] }) {
  if (positions.length === 0) return <p>No open positions.</p>
  return (
    <table aria-label="cockpit open positions">
      <thead><tr><th>Ticker</th><th>Side</th><th>P&amp;L</th><th>Position</th><th>Strategy</th></tr></thead>
      <tbody>{positions.slice(0, 5).map((position) => (
        <tr key={position.id}>
          <td>{position.ticker}</td><td>{position.side}</td><td>{position.unrealized_pnl?.toFixed(2) ?? 'Unknown'}</td>
          <td><EntityLink kind="position" id={position.id} /></td>
          <td><EntityLink kind="strategy" id={position.strategy_id} /></td>
        </tr>
      ))}</tbody>
    </table>
  )
}

function RecentOrders({ orders }: { orders: Order[] }) {
  if (orders.length === 0) return <p>No recent orders.</p>
  return (
    <table aria-label="cockpit recent orders">
      <thead><tr><th>Ticker</th><th>Status</th><th>Order</th><th>Run</th></tr></thead>
      <tbody>{orders.slice(0, 5).map((order) => (
        <tr key={order.id}>
          <td>{order.ticker}</td><td><span className={`status-pill ${statusClass(order.status)}`}>{order.status}</span></td>
          <td><EntityLink kind="order" id={order.id} /></td>
          <td><EntityLink kind="run" id={order.pipeline_run_id} /></td>
        </tr>
      ))}</tbody>
    </table>
  )
}

function RecentTrades({ trades }: { trades: Trade[] }) {
  if (trades.length === 0) return <p>No recent trades.</p>
  return (
    <table aria-label="cockpit recent trades">
      <thead><tr><th>Ticker</th><th>Side</th><th>Price</th><th>Order</th><th>Position</th></tr></thead>
      <tbody>{trades.slice(0, 5).map((trade) => (
        <tr key={trade.id}>
          <td>{trade.ticker}</td><td>{trade.side}</td><td>{trade.price.toFixed(2)}</td>
          <td><EntityLink kind="order" id={trade.order_id} /></td>
          <td><EntityLink kind="position" id={trade.position_id} /></td>
        </tr>
      ))}</tbody>
    </table>
  )
}

export function CockpitPage() {
  const realtime = useRealtime()
  const { send } = realtime
  const risk = useQuery({ queryKey: queryKeys.riskStatus, queryFn: ({ signal }) => getRiskStatus(signal), refetchInterval: 30_000 })
  const riskCockpit = useQuery({ queryKey: queryKeys.riskCockpit, queryFn: ({ signal }) => getRiskCockpit(signal), refetchInterval: 30_000 })
  const breakers = useQuery({ queryKey: queryKeys.riskBreakers, queryFn: ({ signal }) => getRiskBreakers(signal), refetchInterval: 30_000 })
  const health = useQuery({ queryKey: queryKeys.health, queryFn: ({ signal }) => getHealth(signal), refetchInterval: 30_000 })
  const portfolio = useQuery({ queryKey: queryKeys.portfolioSummary, queryFn: ({ signal }) => getPortfolioSummary(signal), refetchInterval: 30_000 })
  const openPositions = useQuery({ queryKey: queryKeys.portfolioOpenPositions({ limit: 5, offset: 0 }), queryFn: ({ signal }) => getOpenPortfolioPositions({ limit: 5, offset: 0 }, signal), refetchInterval: 30_000 })
  const runs = useQuery({ queryKey: queryKeys.runningRuns, queryFn: ({ signal }) => getRunningRuns(signal), refetchInterval: 20_000 })
  const orders = useQuery({ queryKey: queryKeys.ordersListFiltered({ limit: 5, offset: 0 }), queryFn: ({ signal }) => getOrders({ limit: 5, offset: 0 }, signal), refetchInterval: 30_000 })
  const trades = useQuery({ queryKey: queryKeys.tradesListFiltered({ limit: 5, offset: 0 }), queryFn: ({ signal }) => getTrades({ limit: 5, offset: 0 }, signal), refetchInterval: 30_000 })
  const automation = useQuery({ queryKey: queryKeys.automationHealth, queryFn: ({ signal }) => getAutomationHealth(signal), refetchInterval: 30_000 })
  const hasWidgetError = [health, portfolio, openPositions, runs, orders, trades, automation].some((query) => query.isError && !(isApiClientError(query.error) && query.error.kind === 'not_implemented'))
  const classification = classifyCockpit({ risk: risk.data, cockpit: riskCockpit.data, breakers: breakers.data, health: health.data, automationHealthy: automation.data?.healthy, realtimeStatus: realtime.status, hasWidgetError })
  const portfolioPnl = portfolio.data ? portfolio.data.unrealized_pnl + portfolio.data.realized_pnl : 0
  const pnlSparklineData = portfolio.data ? [{ name: 'P/L', unrealized: portfolio.data.unrealized_pnl, realized: portfolio.data.realized_pnl }] : []
  const portfolioDistribution = openPositions.data?.data?.slice(0, 6).map((position, index) => ({ name: position.ticker, value: Math.max(Math.abs(position.unrealized_pnl ?? 0), 1), fill: getChartColors().distribution[index % 6] })) ?? []

  useEffect(() => {
    send({ action: 'subscribe_all' })
  }, [send])

  return (
    <div className="cockpit-grid">
      <PageHeader eyebrow="Operator cockpit" title="System overview" actions={<Breadcrumbs items={[{ label: 'Cockpit' }]} />} />
      <section className="panel">
        <div className="metrics-grid">
          <div>
            <p role="status" className={`status-pill ${classification}`}>Cockpit classification: {classification}</p>
            <p>{classificationCopy(classification)}</p>
            <div className="metric-row">
              <span className={`status-pill ${statusClass(realtime.status)}`}>WebSocket {realtime.status}</span>
              <span>Reconnect failures: {realtime.failedAttempts}</span>
              <span>Buffered events: {realtime.events.length}/250</span>
            </div>
          </div>
          <dl className="kv-grid">
            <dt>Open positions</dt><dd>{portfolio.data?.open_positions ?? '—'}</dd>
            <dt>Unrealized P&amp;L</dt><dd><span className={`status-pill ${pnlClass(portfolio.data?.unrealized_pnl ?? 0)}`}>{formatCurrency(portfolio.data?.unrealized_pnl ?? 0)}</span></dd>
            <dt>Realized P&amp;L</dt><dd><span className={`status-pill ${pnlClass(portfolio.data?.realized_pnl ?? 0)}`}>{formatCurrency(portfolio.data?.realized_pnl ?? 0)}</span></dd>
            <dt>Total P&amp;L</dt><dd><span className={`status-pill ${pnlClass(portfolioPnl)}`}>{formatCurrency(portfolioPnl)}</span></dd>
          </dl>
        </div>
        <div className="metrics-grid">
          <div style={{ minHeight: 120 }}>
            <ResponsiveContainer width="100%" height={120}>
              <AreaChart data={pnlSparklineData}>
                <defs>
                  <linearGradient id="unrealizedPnlGradient" x1="0" y1="0" x2="0" y2="1">
                    <stop offset="5%" stopColor={getChartColors().accent} stopOpacity={0.45} />
                    <stop offset="95%" stopColor={getChartColors().accent} stopOpacity={0} />
                  </linearGradient>
                  <linearGradient id="realizedPnlGradient" x1="0" y1="0" x2="0" y2="1">
                    <stop offset="5%" stopColor={getChartColors().accentSecondary} stopOpacity={0.45} />
                    <stop offset="95%" stopColor={getChartColors().accentSecondary} stopOpacity={0} />
                  </linearGradient>
                </defs>
                <Tooltip formatter={(value) => formatCurrency(Number(value ?? 0))} />
                <Area type="monotone" dataKey="unrealized" stroke={getChartColors().accent} fill="url(#unrealizedPnlGradient)" strokeWidth={2} dot={false} />
                <Area type="monotone" dataKey="realized" stroke={getChartColors().accentSecondary} fill="url(#realizedPnlGradient)" strokeWidth={2} dot={false} />
              </AreaChart>
            </ResponsiveContainer>
          </div>
          <div style={{ minHeight: 120 }}>
            {portfolioDistribution.length > 0 ? (
              <ResponsiveContainer width="100%" height={120}>
                <PieChart>
                  <Pie data={portfolioDistribution} dataKey="value" nameKey="name" innerRadius={26} outerRadius={48} paddingAngle={2}>
                    {portfolioDistribution.map((entry) => <Cell key={entry.name} fill={entry.fill} />)}
                  </Pie>
                  <Tooltip />
                </PieChart>
              </ResponsiveContainer>
            ) : (
              <div className={`status-pill ${pnlClass(portfolioPnl)}`}>P/L {formatCurrency(portfolioPnl)}</div>
            )}
          </div>
        </div>
      </section>

      <StaleBanner show={realtime.status !== 'connected'} message={`Realtime is ${realtime.status}; cockpit data may be stale.`} />

      <QueryPanel title="System health" query={health}>{(data) => (
        <div className="metrics-grid">
          <dl className="kv-grid">
            <dt>Status</dt><dd><span className={`status-pill ${statusClass(data.status)}`}>{data.status}</span></dd>
            <dt>Database</dt><dd>{data.db}</dd>
            <dt>Redis</dt><dd>{data.redis}</dd>
          </dl>
          <dl className="kv-grid">
            <dt>Automation</dt><dd><span className={`status-pill ${statusClass(automation.data?.healthy ? 'healthy' : 'warning')}`}>{automation.data?.healthy ? 'healthy' : automation.isSuccess ? 'failing' : 'loading'}</span></dd>
            <dt>Healthy jobs</dt><dd>{automation.data?.healthy ? automation.data.total_jobs - automation.data.failing_jobs : automation.data?.total_jobs ?? '—'}</dd>
            <dt>Failing jobs</dt><dd>{automation.data?.failing_jobs ?? '—'}</dd>
          </dl>
        </div>
      )}</QueryPanel>

      <QueryPanel title="Risk overview" query={riskCockpit} wide>{(cockpitData) => (
        <>
          <div className="metrics-grid">
            <dl className="kv-grid">
              <dt>Risk status</dt><dd><span className={`status-pill ${statusClass(risk.data?.risk_status ?? 'unknown')}`}>{risk.data?.risk_status ?? 'unknown'}</span></dd>
              <dt>Kill switch</dt><dd>{risk.data?.kill_switch.active ? 'Active' : 'Inactive'}</dd>
              <dt>Circuit breaker</dt><dd>{risk.data?.circuit_breaker.state ?? 'unknown'}</dd>
            </dl>
            <dl className="kv-grid">
              <dt>Cockpit kill switch</dt><dd>{cockpitData.kill_switch_active ? 'Active' : 'Inactive'}</dd>
              <dt>Cockpit breaker</dt><dd>{cockpitData.circuit_breaker ? 'Tripped' : 'Clear'}</dd>
              <dt>Warnings</dt><dd>{cockpitData.warnings.length}</dd>
            </dl>
          </div>
          {cockpitData.warnings.length > 0 ? <ul>{cockpitData.warnings.map((warning) => <li key={warning}>{warning}</li>)}</ul> : null}
          {breakers.data?.tripped.length ? <ul>{breakers.data.tripped.map((breaker) => <li key={`${breaker.scope}-${breaker.tripped_at}`}>{breaker.scope}: {breaker.reason}</li>)}</ul> : <p>No tripped breakers.</p>}
          {cockpitData.exposures.length > 0 ? <table aria-label="cockpit risk exposure"><thead><tr><th>Market</th><th>Open positions</th><th>Gross exposure</th><th>Expected value</th></tr></thead><tbody>{cockpitData.exposures.slice(0, 5).map((exposure) => <tr key={exposure.market_type}><td>{exposure.market_type}</td><td>{exposure.open_positions}</td><td>{exposure.gross_exposure.toFixed(2)}</td><td>{exposure.net_expected_value.toFixed(2)}</td></tr>)}</tbody></table> : <p>No risk exposure recorded.</p>}
        </>
      )}</QueryPanel>

      <QueryPanel title="Portfolio summary" query={portfolio}>{(data) => (
        <div className="metrics-grid">
          <dl className="kv-grid">
            <dt>Open positions</dt><dd>{data.open_positions}</dd>
            <dt>Unrealized P&amp;L</dt><dd><span className={`status-pill ${pnlClass(data.unrealized_pnl)}`}>{formatCurrency(data.unrealized_pnl)}</span></dd>
            <dt>Realized P&amp;L</dt><dd><span className={`status-pill ${pnlClass(data.realized_pnl)}`}>{formatCurrency(data.realized_pnl)}</span></dd>
          </dl>
          <div style={{ minHeight: 72 }}>
            <ResponsiveContainer width="100%" height={72}>
              <AreaChart data={[{ name: 'P/L', unrealized: data.unrealized_pnl, realized: data.realized_pnl }]}>
                <defs>
                  <linearGradient id="portfolioMiniGradient" x1="0" y1="0" x2="0" y2="1">
                    <stop offset="5%" stopColor={data.unrealized_pnl + data.realized_pnl >= 0 ? getChartColors().success : getChartColors().danger} stopOpacity={0.45} />
                    <stop offset="95%" stopColor={data.unrealized_pnl + data.realized_pnl >= 0 ? getChartColors().success : getChartColors().danger} stopOpacity={0} />
                  </linearGradient>
                </defs>
                <Tooltip formatter={(value) => formatCurrency(Number(value ?? 0))} />
                <Area type="monotone" dataKey="unrealized" stroke={data.unrealized_pnl >= 0 ? getChartColors().success : getChartColors().danger} fill="url(#portfolioMiniGradient)" dot={false} strokeWidth={2} />
              </AreaChart>
            </ResponsiveContainer>
          </div>
        </div>
      )}</QueryPanel>

      <QueryPanel title="Open positions" query={openPositions} wide>{(data) => <OpenPositions positions={data.data} />}</QueryPanel>
      <QueryPanel title="Active runs" query={runs} wide>{(data) => <RecentRuns runs={data.data} />}</QueryPanel>
      <QueryPanel title="Recent orders" query={orders} wide>{(data) => <RecentOrders orders={data.data} />}</QueryPanel>
      <QueryPanel title="Recent trades" query={trades} wide>{(data) => <RecentTrades trades={data.data} />}</QueryPanel>
    </div>
  )
}
