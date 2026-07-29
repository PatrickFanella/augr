import { useEffect, useId, type ReactNode } from 'react'
import { useQuery, type UseQueryResult } from '@tanstack/react-query'
import { Activity, AlertTriangle, ArrowRight, CircleDollarSign, Radio, RefreshCw, ShieldCheck, WalletCards } from 'lucide-react'
import { Link } from 'react-router-dom'

import { PageHeader } from '@/components/ui/page-header'
import { getAutomationHealth, getHealth, getOpenPortfolioPositions, getOrders, getPortfolioSummary, getRiskBreakers, getRiskCockpit, getRiskStatus, getRunningRuns, getTrades } from '@/shared/api/endpoints'
import { isApiClientError } from '@/shared/api/errors'
import { Breadcrumbs, EntityLink } from '@/shared/components/EntityLinks'
import { ErrorState, LastUpdated, LoadingState, StaleBanner } from '@/shared/components/QueryStates'
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

function pnlClass(value?: number | null) {
  if (value === undefined || value === null) return 'unknown'
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
    <table className="operations-table" aria-label="active runs">
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
    <table className="operations-table" aria-label="cockpit open positions">
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
    <table className="operations-table" aria-label="cockpit recent orders">
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
    <table className="operations-table" aria-label="cockpit recent trades">
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
  const portfolioPnl = portfolio.data?.total_pnl

  useEffect(() => {
    send({ action: 'subscribe_all' })
  }, [send])

  const warnings = [
    ...(riskCockpit.data?.warnings ?? []),
    ...(breakers.data?.tripped.filter((breaker) => !breaker.reset_at).map((breaker) => `${breaker.scope}: ${breaker.reason}`) ?? []),
    ...(automation.data && automation.data.failing_jobs > 0 ? [`${automation.data.failing_jobs} automation job${automation.data.failing_jobs === 1 ? '' : 's'} failing`] : []),
    ...(portfolio.data && portfolio.data.unmarked_positions > 0 ? [`Valuation unavailable for ${portfolio.data.unmarked_positions} of ${portfolio.data.open_positions} open positions`] : []),
    ...(realtime.status !== 'connected' ? [`Realtime feed is ${realtime.status}`] : []),
  ]

  return (
    <div className="cockpit-grid operations-dashboard">
      <PageHeader eyebrow="Paper trading command center" title="System overview" actions={<Breadcrumbs items={[{ label: 'HUD' }]} />} />

      <section className={`ops-hero ${classification}`} aria-labelledby="ops-state-title">
        <div className="ops-state-mark" aria-hidden="true">
          {classification === 'safe' ? <ShieldCheck /> : <AlertTriangle />}
        </div>
        <div className="ops-state-copy">
          <p className="eyebrow">Current operating state</p>
          <h2 id="ops-state-title">{classification === 'safe' ? 'Paper trading is operating normally' : classification === 'degraded' ? 'Paper trading needs attention' : 'Paper trading state is unknown'}</h2>
          <p>{classificationCopy(classification)}</p>
        </div>
        <div className="ops-state-signals">
          <span className={`signal-line ${statusClass(realtime.status)}`}><Radio /> Realtime {realtime.status}</span>
          <span className={`signal-line ${statusClass(automation.data?.healthy ? 'healthy' : 'warning')}`}><Activity /> Automation {automation.data?.healthy ? 'healthy' : automation.isSuccess ? 'failing' : 'loading'}</span>
          <span className={`signal-line ${statusClass(risk.data?.risk_status ?? 'unknown')}`}><ShieldCheck /> Risk <span>{risk.data?.risk_status ?? 'unknown'}</span></span>
        </div>
        <div className="sr-only" aria-live="polite">
          <p>Cockpit classification: {classification}</p>
          <p>WebSocket {realtime.status}</p>
          <p>Buffered events: {realtime.events.length}/250</p>
          <h2>Open notional exposure</h2>
          <p>No historical equity series is available from this endpoint.</p>
        </div>
      </section>

      <StaleBanner show={realtime.status !== 'connected'} message={`Realtime is ${realtime.status}; dashboard data may be stale.`} />

      <section className="ops-metrics" aria-label="Paper account summary">
        <article className="ops-metric">
          <div className="ops-metric-icon"><CircleDollarSign /></div>
          <div><p>Total P&amp;L</p><strong className={pnlClass(portfolioPnl)}>{portfolioPnl == null ? 'Incomplete' : `$${formatCurrency(portfolioPnl)}`}</strong><span>{portfolio.data?.valuation_status === 'complete' ? 'Realized + unrealized' : 'Waiting for position marks'}</span></div>
        </article>
        <article className="ops-metric">
          <div className="ops-metric-icon"><WalletCards /></div>
          <div><p>Open positions</p><strong>{portfolio.data?.open_positions ?? '—'}</strong><span>{portfolio.data ? `${portfolio.data.marked_positions} / ${portfolio.data.open_positions} marked` : 'Valuation loading'}</span></div>
        </article>
        <article className="ops-metric">
          <div className="ops-metric-icon"><Activity /></div>
          <div><p>Active runs</p><strong>{runs.data?.data.length ?? '—'}</strong><span>{realtime.events.length} buffered events</span></div>
        </article>
        <article className="ops-metric">
          <div className="ops-metric-icon"><AlertTriangle /></div>
          <div><p>Needs attention</p><strong className={warnings.length ? 'warning' : 'success'}>{warnings.length}</strong><span>{warnings.length ? 'Review before next cycle' : 'No active warnings'}</span></div>
        </article>
      </section>

      <section className={`attention-panel ${warnings.length ? 'has-warnings' : ''}`} aria-labelledby="attention-title">
        <div>
          <p className="eyebrow">Operator queue</p>
          <h2 id="attention-title">{warnings.length ? `${warnings.length} item${warnings.length === 1 ? '' : 's'} need attention` : 'Nothing needs attention'}</h2>
        </div>
        {warnings.length ? <ul>{warnings.map((warning) => <li key={warning}>{warning}</li>)}</ul> : <p>Risk controls, automation, infrastructure, and realtime data are reporting normally.</p>}
        <Link to="/risk" className="inline-action">Open risk console <ArrowRight /></Link>
      </section>

      <div className="ops-two-column">
        <QueryPanel title="Paper account" query={portfolio}>{(data) => (
          <div className="account-breakdown">
            <div><span>Unrealized</span><strong className={pnlClass(data.unrealized_pnl)}>{data.unrealized_pnl == null ? 'Unavailable' : `$${formatCurrency(data.unrealized_pnl)}`}</strong></div>
            <div><span>Realized</span><strong className={pnlClass(data.realized_pnl)}>${formatCurrency(data.realized_pnl)}</strong></div>
            <div><span>Total</span><strong className={pnlClass(data.total_pnl)}>{data.total_pnl == null ? 'Incomplete' : `$${formatCurrency(data.total_pnl)}`}</strong></div>
          </div>
        )}</QueryPanel>

        <QueryPanel title="System health" query={health}>{(data) => (
          automation.isError ? <ErrorState error={automation.error} onRetry={() => void automation.refetch()} /> : <div className="service-list">
            <div><span>API</span><strong className={statusClass(data.status)}>{data.status}</strong></div>
            <div><span>Database</span><strong className={statusClass(data.db)}>{data.db}</strong></div>
            <div><span>Redis</span><strong className={statusClass(data.redis)}>{data.redis}</strong></div>
            <div><span>Automation</span><strong className={statusClass(automation.data?.healthy ? 'healthy' : 'warning')}>{automation.data?.healthy ? 'healthy' : 'check'}</strong></div>
          </div>
        )}</QueryPanel>
      </div>

      <QueryPanel title="Market exposure" query={riskCockpit} wide>{(cockpitData) => (
        cockpitData.exposures.length > 0 ? <table aria-label="cockpit risk exposure"><thead><tr><th>Market</th><th>Open positions</th><th>Valuation</th><th>Gross cost basis</th></tr></thead><tbody>{cockpitData.exposures.map((exposure) => <tr key={exposure.market_type}><td>{exposure.market_type}</td><td>{exposure.open_positions}</td><td>{exposure.marked_positions} / {exposure.open_positions} marked</td><td>${formatCurrency(exposure.gross_exposure)}</td></tr>)}</tbody></table> : <div className="quiet-state"><ShieldCheck /><div><strong>No market exposure</strong><p>The paper account has no open risk.</p></div></div>
      )}</QueryPanel>

      <div className="ops-activity-grid">
        <QueryPanel title="Open positions" query={openPositions} wide>{(data) => <><OpenPositions positions={data.data} />{data.total !== undefined && data.total > data.data.length ? <p className="muted">Showing {data.data.length} of {data.total}. <Link to="/portfolio">Open the full portfolio.</Link></p> : null}</>}</QueryPanel>
        <QueryPanel title="Active runs" query={runs} wide>{(data) => <RecentRuns runs={data.data} />}</QueryPanel>
        <QueryPanel title="Recent orders" query={orders} wide>{(data) => <RecentOrders orders={data.data} />}</QueryPanel>
        <QueryPanel title="Recent trades" query={trades} wide>{(data) => <RecentTrades trades={data.data} />}</QueryPanel>
      </div>
    </div>
  )
}
