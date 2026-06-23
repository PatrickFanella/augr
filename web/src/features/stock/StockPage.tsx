import { useQuery } from '@tanstack/react-query'
import { useMemo, type ReactNode } from 'react'
import { useParams } from 'react-router-dom'
import { AreaChart, Area, ResponsiveContainer, Tooltip } from 'recharts'

import { getOpenPortfolioPositions, getTrades, getRuns, getStrategies } from '@/shared/api/endpoints'
import { PageHeader } from '@/components/ui/page-header'
import { Breadcrumbs, EntityLink } from '@/shared/components/EntityLinks'
import { ErrorState, LastUpdated, LoadingState, EmptyState } from '@/shared/components/QueryStates'
import { getChartColors } from '@/lib/chart-theme'
import { StatusBadge } from '@/components/ui/status-badge'
import { queryKeys } from '@/shared/query/keys'
import { normalizeStatus } from '@/lib/status'

function money(value?: number) {
  if (value === undefined) return '—'
  return new Intl.NumberFormat(undefined, { style: 'currency', currency: 'USD', maximumFractionDigits: 2 }).format(value)
}

function pnlClass(value?: number) {
  if (value === undefined) return 'unknown'
  return value > 0 ? 'active' : value < 0 ? 'unknown' : 'unknown'
}

function SectionState({ title, count, isLoading, error, onRetry, empty, children, updatedAt }: { title: string; count?: number; isLoading: boolean; error: unknown; onRetry: () => void; empty: boolean; children: ReactNode; updatedAt?: string | number | Date }) {
  return (
    <section className="panel" aria-labelledby={title}>
      <div className="panel-header">
        <div>
          <h2 id={title}>{title}</h2>
          {count !== undefined ? <p className="muted">{count} records</p> : null}
        </div>
        {updatedAt ? <LastUpdated date={updatedAt} /> : null}
      </div>
      {isLoading ? <LoadingState label={`Loading ${title.toLowerCase()}…`} /> : null}
      {error ? <ErrorState error={error} onRetry={onRetry} /> : null}
      {!isLoading && !error && empty ? <EmptyState title={`No ${title.toLowerCase()}`} message={`No ${title.toLowerCase()} found for this ticker.`} /> : null}
      {!isLoading && !error && !empty ? children : null}
    </section>
  )
}

export function StockPage() {
  const params = useParams()
  const ticker = (params.ticker ?? '').toUpperCase()
  const activeTicker = ticker || undefined

  const positionFilters = useMemo(() => ({ ticker: activeTicker, limit: 100, offset: 0 }), [activeTicker])
  const tradesFilters = useMemo(() => ({ ticker: activeTicker, limit: 20, offset: 0 }), [activeTicker])
  const runsFilters = useMemo(() => ({ ticker: activeTicker, limit: 20, offset: 0 }), [activeTicker])
  const strategyFilters = useMemo(() => ({ ticker: activeTicker, limit: 20, offset: 0 }), [activeTicker])

  const positionsQuery = useQuery({ queryKey: queryKeys.portfolioOpenPositions(positionFilters), queryFn: ({ signal }) => getOpenPortfolioPositions(positionFilters, signal), refetchInterval: 30_000, enabled: Boolean(activeTicker) })
  const tradesQuery = useQuery({ queryKey: queryKeys.tradesListFiltered(tradesFilters), queryFn: ({ signal }) => getTrades(tradesFilters, signal), refetchInterval: 30_000, enabled: Boolean(activeTicker) })
  const runsQuery = useQuery({ queryKey: queryKeys.runsListFiltered(runsFilters), queryFn: ({ signal }) => getRuns(runsFilters, signal), refetchInterval: 30_000, enabled: Boolean(activeTicker) })
  const strategiesQuery = useQuery({ queryKey: queryKeys.strategyListFiltered(strategyFilters), queryFn: ({ signal }) => getStrategies(strategyFilters, signal), refetchInterval: 30_000, enabled: Boolean(activeTicker) })

  const positions = positionsQuery.data?.data ?? []
  const trades = tradesQuery.data?.data ?? []
  const runs = runsQuery.data?.data ?? []
  const strategies = strategiesQuery.data?.data ?? []

  const unrealized = positions.reduce((sum, position) => sum + (position.unrealized_pnl ?? 0), 0)
  const realized = positions.reduce((sum, position) => sum + position.realized_pnl, 0)
  const pnlSeries = [...positions].sort((a, b) => new Date(a.opened_at).getTime() - new Date(b.opened_at).getTime()).map((position, index) => ({ name: `${index + 1}`, value: (position.unrealized_pnl ?? 0) + position.realized_pnl }))
  const chartColor = unrealized + realized >= 0 ? getChartColors().success : getChartColors().danger

  if (!activeTicker) {
    return <div className="detail-stack"><section className="panel hero-panel"><h1>Stock</h1><p className="muted">Missing ticker symbol.</p></section></div>
  }

  return (
    <div className="detail-stack">
      <Breadcrumbs items={[{ label: 'Cockpit', to: '/cockpit' }, { label: ticker }]} />
      <PageHeader eyebrow="Ticker detail" title={ticker} description="Aggregated position, trade, run, and strategy activity for this symbol." actions={<span className="status-pill active">Live</span>} />

      <section className="metrics-grid" aria-label={`${ticker} summary`}>
        <article className="panel"><p className="eyebrow">Open positions</p><strong>{positionsQuery.data ? positions.length : '—'}</strong></article>
        <article className="panel"><p className="eyebrow">Unrealized P/L</p><strong><span className={`status-pill ${pnlClass(unrealized)}`}>{positionsQuery.data ? money(unrealized) : '—'}</span></strong></article>
        <article className="panel"><p className="eyebrow">Realized P/L</p><strong><span className={`status-pill ${pnlClass(realized)}`}>{positionsQuery.data ? money(realized) : '—'}</span></strong></article>
        <article className="panel"><p className="eyebrow">Recent trades</p><strong>{tradesQuery.data ? trades.length : '—'}</strong></article>
      </section>

      <section className="chart-container panel" aria-label={`${ticker} P/L chart`}>
        <div className="chart-title">Position P/L</div>
        {positionsQuery.data ? (
          <ResponsiveContainer width="100%" height={88}>
            <AreaChart data={pnlSeries}>
              <Tooltip formatter={(value) => money(Number(value ?? 0))} />
              <Area type="monotone" dataKey="value" stroke={chartColor} fill={chartColor} fillOpacity={0.2} dot={false} strokeWidth={2} />
            </AreaChart>
          </ResponsiveContainer>
        ) : null}
      </section>

      <SectionState title="Open positions" count={positions.length} isLoading={positionsQuery.isLoading} error={positionsQuery.error} onRetry={() => void positionsQuery.refetch()} empty={positions.length === 0} updatedAt={positionsQuery.dataUpdatedAt ? new Date(positionsQuery.dataUpdatedAt) : undefined}>
        <div className="table-wrap">
          <table aria-label="Open positions for ticker">
            <thead><tr><th>Position</th><th>Side</th><th>Quantity</th><th>Avg entry</th><th>Current</th><th>Unrealized</th><th>Realized</th><th>Opened</th></tr></thead>
            <tbody>{positions.map((position) => (<tr key={position.id}><td><EntityLink kind="position" id={position.id} label={position.id} /></td><td><span className={`status-pill ${position.side}`}>{position.side}</span></td><td>{position.quantity}</td><td>{money(position.avg_entry)}</td><td>{money(position.current_price)}</td><td><span className={`status-pill ${pnlClass(position.unrealized_pnl)}`}>{money(position.unrealized_pnl)}</span></td><td><span className={`status-pill ${pnlClass(position.realized_pnl)}`}>{money(position.realized_pnl)}</span></td><td>{new Date(position.opened_at).toLocaleString()}</td></tr>))}</tbody>
          </table>
        </div>
      </SectionState>

      <SectionState title="Recent trades" count={trades.length} isLoading={tradesQuery.isLoading} error={tradesQuery.error} onRetry={() => void tradesQuery.refetch()} empty={trades.length === 0} updatedAt={tradesQuery.dataUpdatedAt ? new Date(tradesQuery.dataUpdatedAt) : undefined}>
        <div className="table-wrap">
          <table aria-label="Recent trades for ticker">
            <thead><tr><th>Trade</th><th>Side</th><th>Quantity</th><th>Price</th><th>Fee</th><th>Order</th><th>Executed</th></tr></thead>
            <tbody>{trades.map((trade) => (<tr key={trade.id}><td><EntityLink kind="trade" id={trade.id} label={trade.id} /></td><td><span className={`status-pill ${trade.side}`}>{trade.side}</span></td><td>{trade.quantity}</td><td>{money(trade.price)}</td><td>{money(trade.fee)}</td><td>{trade.order_id ? <EntityLink kind="order" id={trade.order_id} /> : '—'}</td><td>{new Date(trade.executed_at).toLocaleString()}</td></tr>))}</tbody>
          </table>
        </div>
      </SectionState>

      <SectionState title="Recent runs" count={runs.length} isLoading={runsQuery.isLoading} error={runsQuery.error} onRetry={() => void runsQuery.refetch()} empty={runs.length === 0} updatedAt={runsQuery.dataUpdatedAt ? new Date(runsQuery.dataUpdatedAt) : undefined}>
        <div className="table-wrap">
          <table aria-label="Recent runs for ticker">
            <thead><tr><th>Run</th><th>Status</th><th>Signal</th><th>Started</th><th>Completed</th></tr></thead>
            <tbody>{runs.map((run) => (<tr key={run.id}><td><EntityLink kind="run" id={run.id} label={run.id} /></td><td><StatusBadge status={normalizeStatus(run.status)} label={run.status} /></td><td>{run.signal ?? '—'}</td><td>{new Date(run.started_at).toLocaleString()}</td><td>{run.completed_at ? new Date(run.completed_at).toLocaleString() : '—'}</td></tr>))}</tbody>
          </table>
        </div>
      </SectionState>

      <SectionState title="Active strategies" count={strategies.length} isLoading={strategiesQuery.isLoading} error={strategiesQuery.error} onRetry={() => void strategiesQuery.refetch()} empty={strategies.length === 0} updatedAt={strategiesQuery.dataUpdatedAt ? new Date(strategiesQuery.dataUpdatedAt) : undefined}>
        <div className="table-wrap">
          <table aria-label="Active strategies for ticker">
            <thead><tr><th>Strategy</th><th>Status</th><th>Market</th><th>Paper</th><th>Updated</th></tr></thead>
            <tbody>{strategies.map((strategy) => (<tr key={strategy.id}><td><EntityLink kind="strategy" id={strategy.id} label={strategy.name} /></td><td><StatusBadge status={normalizeStatus(strategy.status)} label={strategy.status} /></td><td>{strategy.market_type}</td><td>{strategy.is_paper ? 'Yes' : 'No'}</td><td>{new Date(strategy.updated_at).toLocaleString()}</td></tr>))}</tbody>
          </table>
        </div>
      </SectionState>
    </div>
  )
}
