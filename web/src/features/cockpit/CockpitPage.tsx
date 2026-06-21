import { useEffect, useId, type ReactNode } from 'react'
import { useQuery, type UseQueryResult } from '@tanstack/react-query'

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

function QueryPanel<T>({ title, query, children, wide = false }: { title: string; query: UseQueryResult<T, Error>; children: (data: T) => ReactNode; wide?: boolean }) {
  const titleId = useId()
  return (
    <section className={`panel ${wide ? 'wide-panel' : ''}`} aria-labelledby={titleId}>
      <div className="panel-header">
        <h2 id={titleId}>{title}</h2>
        <button type="button" onClick={() => void query.refetch()}>Retry</button>
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
          <td><EntityLink kind="run" id={run.id} label="Run" /></td>
          <td><EntityLink kind="strategy" id={run.strategy_id} label="Strategy" /></td>
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
      <thead><tr><th>Ticker</th><th>Side</th><th>P&L</th><th>Position</th><th>Strategy</th></tr></thead>
      <tbody>{positions.slice(0, 5).map((position) => (
        <tr key={position.id}>
          <td>{position.ticker}</td><td>{position.side}</td><td>{position.unrealized_pnl?.toFixed(2) ?? 'Unknown'}</td>
          <td><EntityLink kind="position" id={position.id} label="Position trades" /></td>
          <td><EntityLink kind="strategy" id={position.strategy_id} label="Strategy" /></td>
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
          <td><EntityLink kind="order" id={order.id} label="Order" /></td>
          <td><EntityLink kind="run" id={order.pipeline_run_id} label="Run" /></td>
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
          <td><EntityLink kind="order" id={trade.order_id} label="Order" /></td>
          <td><EntityLink kind="position" id={trade.position_id} label="Position trades" /></td>
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

  useEffect(() => {
    send({ action: 'subscribe_all' })
  }, [send])

  return (
    <div className="cockpit-grid">
      <section className="panel hero-panel" aria-labelledby="cockpit-heading">
        <Breadcrumbs items={[{ label: 'Cockpit' }]} />
        <p className="eyebrow">Operator cockpit</p>
        <h1 id="cockpit-heading">System overview</h1>
        <p role="status" className={`status-pill ${classification}`}>Cockpit classification: {classification}</p>
        <p>{classificationCopy(classification)}</p>
        <div className="metric-row">
          <span className={`status-pill ${statusClass(realtime.status)}`}>WebSocket {realtime.status}</span>
          <span>Reconnect failures: {realtime.failedAttempts}</span>
          <span>Buffered events: {realtime.events.length}/250</span>
        </div>
      </section>

      <StaleBanner show={realtime.status !== 'connected'} message={`Realtime is ${realtime.status}; cockpit data may be stale.`} />

      <QueryPanel title="Infrastructure health" query={health}>{(data) => (
        <dl className="kv-grid">
          <dt>Status</dt><dd><span className={`status-pill ${statusClass(data.status)}`}>{data.status}</span></dd>
          <dt>Database</dt><dd>{data.db}</dd>
          <dt>Redis</dt><dd>{data.redis}</dd>
        </dl>
      )}</QueryPanel>

      <QueryPanel title="Risk cockpit" query={riskCockpit}>{(data) => (
        <>
          <dl className="kv-grid">
            <dt>Kill switch</dt><dd>{data.kill_switch_active ? 'Active' : 'Inactive'}</dd>
            <dt>Circuit breaker</dt><dd>{data.circuit_breaker ? 'Tripped' : 'Clear'}</dd>
            <dt>Warnings</dt><dd>{data.warnings.length}</dd>
          </dl>
          {data.warnings.length > 0 ? <ul>{data.warnings.map((warning) => <li key={warning}>{warning}</li>)}</ul> : null}
          {data.exposures.length > 0 ? <table aria-label="cockpit risk exposure"><thead><tr><th>Market</th><th>Open positions</th><th>Gross exposure</th><th>Expected value</th></tr></thead><tbody>{data.exposures.slice(0, 5).map((exposure) => <tr key={exposure.market_type}><td>{exposure.market_type}</td><td>{exposure.open_positions}</td><td>{exposure.gross_exposure.toFixed(2)}</td><td>{exposure.net_expected_value.toFixed(2)}</td></tr>)}</tbody></table> : <p>No risk exposure recorded.</p>}
        </>
      )}</QueryPanel>

      <QueryPanel title="Risk status" query={risk}>{(data) => (
        <dl className="kv-grid">
          <dt>Status</dt><dd><span className={`status-pill ${statusClass(data.risk_status)}`}>{data.risk_status}</span></dd>
          <dt>Circuit breaker</dt><dd>{data.circuit_breaker.state}</dd>
          <dt>Kill switch</dt><dd>{data.kill_switch.active ? 'Active' : 'Inactive'}</dd>
          <dt>Market switches</dt><dd>{Object.entries(data.market_kill_switches ?? {}).filter(([, value]) => value?.active).length} active</dd>
        </dl>
      )}</QueryPanel>

      <QueryPanel title="Circuit breakers" query={breakers}>{(data) => (
        data.tripped.length === 0 ? <p>No tripped breakers.</p> : <ul>{data.tripped.map((breaker) => <li key={`${breaker.scope}-${breaker.tripped_at}`}>{breaker.scope}: {breaker.reason}</li>)}</ul>
      )}</QueryPanel>

      <QueryPanel title="Portfolio summary" query={portfolio}>{(data) => (
        <dl className="kv-grid">
          <dt>Open positions</dt><dd>{data.open_positions}</dd>
          <dt>Unrealized P&L</dt><dd>{data.unrealized_pnl.toFixed(2)}</dd>
          <dt>Realized P&L</dt><dd>{data.realized_pnl.toFixed(2)}</dd>
        </dl>
      )}</QueryPanel>

      <QueryPanel title="Open positions" query={openPositions} wide>{(data) => <OpenPositions positions={data.data} />}</QueryPanel>
      <QueryPanel title="Active runs" query={runs} wide>{(data) => <RecentRuns runs={data.data} />}</QueryPanel>
      <QueryPanel title="Recent orders" query={orders} wide>{(data) => <RecentOrders orders={data.data} />}</QueryPanel>
      <QueryPanel title="Recent trades" query={trades} wide>{(data) => <RecentTrades trades={data.data} />}</QueryPanel>

      <QueryPanel title="Automation health" query={automation}>{(data) => (
        <dl className="kv-grid">
          <dt>Healthy</dt><dd>{data.healthy ? 'Yes' : 'No'}</dd>
          <dt>Total jobs</dt><dd>{data.total_jobs}</dd>
          <dt>Failing jobs</dt><dd>{data.failing_jobs}</dd>
        </dl>
      )}</QueryPanel>

      <section className="panel wide-panel">
        <h2>Recent realtime events</h2>
        {realtime.status !== 'connected' ? <p role="status">Realtime is {realtime.status}; data may be stale.</p> : null}
        {realtime.events.length === 0 ? <p>No events received.</p> : <ul className="event-list">{realtime.events.slice(0, 20).map((event, index) => <li key={`${event.timestamp}-${index}`}><strong>{event.type}</strong><span>{new Date(event.timestamp).toLocaleString()}</span>{event.strategy_id ? <EntityLink kind="strategy" id={event.strategy_id} label="Strategy" copy={false} /> : null}{event.run_id ? <EntityLink kind="run" id={event.run_id} label="Run" copy={false} /> : null}</li>)}</ul>}
      </section>
    </div>
  )
}
