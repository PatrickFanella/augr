import { useQuery } from '@tanstack/react-query'
import type { KeyboardEvent } from 'react'
import { Link, useNavigate, useSearchParams } from 'react-router-dom'

import { Alert } from '@/components/ui/alert'
import { PageHeader } from '@/components/ui/page-header'
import { StatusBadge } from '@/components/ui/status-badge'
import { normalizeStatus } from '@/lib/status'
import { getStrategies, type StrategyListParams } from '@/shared/api/endpoints'
import { EmptyState, ErrorState, LastUpdated, LoadingState, StaleBanner } from '@/shared/components/QueryStates'
import { queryKeys } from '@/shared/query/keys'
import type { Strategy } from '@/shared/types/domain'
import { useRealtime } from '@/shared/websocket/RealtimeProvider'

const pageSize = 20
const strategyStatuses = ['', 'active', 'paused', 'inactive']
const paperModes = ['', 'true', 'false']

function cleanText(value?: string) {
  return value?.trim() ?? ''
}

function titleCase(value?: string) {
  if (!value) return 'Unknown'
  return value.replace(/_/g, ' ')
}

function paramsFromSearch(searchParams: URLSearchParams): StrategyListParams {
  const isPaper = searchParams.get('is_paper')
  const offset = Number(searchParams.get('offset') ?? '0')
  return {
    ticker: cleanText(searchParams.get('ticker') ?? undefined).toUpperCase() || undefined,
    market_type: cleanText(searchParams.get('market_type') ?? undefined) || undefined,
    status: cleanText(searchParams.get('status') ?? undefined) || undefined,
    is_paper: isPaper === 'true' ? true : isPaper === 'false' ? false : undefined,
    limit: pageSize,
    offset: Number.isFinite(offset) && offset > 0 ? offset : 0,
  }
}

function updateSearch(searchParams: URLSearchParams, updates: Record<string, string>) {
  const next = new URLSearchParams(searchParams)
  for (const [key, value] of Object.entries(updates)) {
    if (value) next.set(key, value)
    else next.delete(key)
  }
  next.delete('offset')
  return next
}

function ModePill({ isPaper }: { isPaper: boolean }) {
  return <span className={`status-pill ${isPaper ? 'paper' : 'live'}`}>{isPaper ? 'PAPER' : 'LIVE'}</span>
}

function LatestRun({ strategy }: { strategy: Strategy }) {
  const latest = strategy.latest_run_summary
  if (!latest) return <span className="muted">No latest run summary</span>
  return (
    <span>
      {titleCase(latest.status)}{latest.signal ? ` · ${titleCase(latest.signal)}` : ''}
      <br />
      <span className="muted">{new Date(latest.started_at).toLocaleString()}</span>
    </span>
  )
}

export function StrategiesListPage() {
  const [searchParams, setSearchParams] = useSearchParams()
  const navigate = useNavigate()
  const realtime = useRealtime()
  const params = paramsFromSearch(searchParams)
  const query = useQuery({
    queryKey: queryKeys.strategyListFiltered(params),
    queryFn: ({ signal }) => getStrategies(params, signal),
  })
  const rows = query.data?.data ?? []
  const offset = params.offset ?? 0
  const total = query.data?.total
  const hasNext = total === undefined ? rows.length === pageSize : offset + pageSize < total
  const hasPrevious = offset > 0
  const tableStale = query.isStale || realtime.status === 'disconnected' || realtime.status === 'degraded'

  function setOffset(nextOffset: number) {
    const next = new URLSearchParams(searchParams)
    if (nextOffset > 0) next.set('offset', String(nextOffset))
    else next.delete('offset')
    setSearchParams(next)
  }

  function onRowKeyDown(event: KeyboardEvent<HTMLTableRowElement>, id: string) {
    if (event.key === 'Enter') navigate(`/strategies/${id}`)
  }

  return (
    <div className="detail-stack strategies-page">
      <PageHeader
        eyebrow="Strategies"
        title="Strategies"
        description="Browse strategies, verify PAPER/LIVE mode, and open deep-linked strategy detail."
        actions={<div className="header-cluster"><Link className="secondary-link" to="/strategies/new">New paper strategy</Link><LastUpdated date={query.dataUpdatedAt || undefined} /></div>}
      />

      <section className="panel hero-panel">
        <form className="filter-bar" aria-label="Strategy filters" onSubmit={(event) => event.preventDefault()}>
          <label>
            Ticker
            <input
              name="ticker"
              value={searchParams.get('ticker') ?? ''}
              onChange={(event) => setSearchParams(updateSearch(searchParams, { ticker: event.target.value.toUpperCase() }))}
              placeholder="AAPL"
            />
          </label>
          <label>
            Market type
            <input
              name="market_type"
              value={searchParams.get('market_type') ?? ''}
              onChange={(event) => setSearchParams(updateSearch(searchParams, { market_type: event.target.value }))}
              placeholder="stock"
            />
          </label>
          <label>
            Status
            <select value={searchParams.get('status') ?? ''} onChange={(event) => setSearchParams(updateSearch(searchParams, { status: event.target.value }))}>
              {strategyStatuses.map((status) => <option key={status || 'all'} value={status}>{status || 'All statuses'}</option>)}
            </select>
          </label>
          <label>
            Mode
            <select value={searchParams.get('is_paper') ?? ''} onChange={(event) => setSearchParams(updateSearch(searchParams, { is_paper: event.target.value }))}>
              {paperModes.map((mode) => <option key={mode || 'all'} value={mode}>{mode === 'true' ? 'Paper only' : mode === 'false' ? 'Live only' : 'All modes'}</option>)}
            </select>
          </label>
          <button type="button" onClick={() => setSearchParams(new URLSearchParams())}>Clear filters</button>
        </form>

        <StaleBanner show={tableStale && Boolean(query.data)} message="Strategy data may be stale. Refresh before taking operational action on a detail page." />
        {realtime.status === 'disconnected' || realtime.status === 'degraded' ? <Alert variant="warning">WebSocket {realtime.status}; rows are read-only and may lag realtime changes.</Alert> : null}
        {query.isLoading ? <LoadingState label="Loading strategies…" /> : null}
        {query.error ? <ErrorState error={query.error} onRetry={() => void query.refetch()} /> : null}
        {!query.isLoading && !query.error && rows.length === 0 ? <EmptyState title="No strategies found" message="Adjust filters or create a paper strategy." /> : null}

        {rows.length > 0 ? (
          <>
            <div className="table-wrap" role="region" aria-label="Strategies table" tabIndex={0}>
              <table>
                <thead>
                  <tr>
                    <th scope="col">Strategy</th>
                    <th scope="col">Ticker</th>
                    <th scope="col">Market</th>
                    <th scope="col">Status</th>
                    <th scope="col">Mode</th>
                    <th scope="col">Latest run</th>
                    <th scope="col">Updated</th>
                  </tr>
                </thead>
                <tbody>
                  {rows.map((strategy) => (
                    <tr key={strategy.id} tabIndex={0} onKeyDown={(event) => onRowKeyDown(event, strategy.id)}>
                      <th scope="row"><Link to={`/strategies/${strategy.id}`}>{strategy.name}</Link></th>
                      <td>{strategy.ticker}</td>
                      <td>{titleCase(strategy.market_type)}</td>
                      <td><StatusBadge status={normalizeStatus(strategy.status)} label={strategy.status} /></td>
                      <td><ModePill isPaper={strategy.is_paper} /></td>
                      <td><LatestRun strategy={strategy} /></td>
                      <td>{new Date(strategy.updated_at).toLocaleString()}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
            <div className="card-list" aria-label="Strategies cards">
              {rows.map((strategy) => (
                <article className="strategy-card" key={strategy.id}>
                  <div className="panel-header">
                    <h2><Link to={`/strategies/${strategy.id}`}>{strategy.name}</Link></h2>
                    <ModePill isPaper={strategy.is_paper} />
                  </div>
                  <p>{strategy.ticker} · {titleCase(strategy.market_type)}</p>
                  <p><StatusBadge status={normalizeStatus(strategy.status)} label={strategy.status} /></p>
                  <p><LatestRun strategy={strategy} /></p>
                </article>
              ))}
            </div>
            <nav className="pagination-controls" aria-label="Strategy pagination">
              <button type="button" disabled={!hasPrevious} onClick={() => setOffset(Math.max(0, offset - pageSize))}>Previous</button>
              <span className="muted">Showing {offset + 1}–{offset + rows.length}{total === undefined ? '' : ` of ${total}`}</span>
              <button type="button" disabled={!hasNext} onClick={() => setOffset(offset + pageSize)}>Next</button>
            </nav>
          </>
        ) : null}
      </section>
    </div>
  )
}
