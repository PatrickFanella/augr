import { useQuery } from '@tanstack/react-query'
import { useEffect, useMemo, useRef, useState, type KeyboardEvent } from 'react'
import { Link, useNavigate, useSearchParams } from 'react-router-dom'

import { getRuns, type RunListParams } from '@/shared/api/endpoints'
import { EntityLink } from '@/shared/components/EntityLinks'
import { EmptyState, ErrorState, LastUpdated, LoadingState, StaleBanner } from '@/shared/components/QueryStates'
import { queryKeys } from '@/shared/query/keys'
import type { PipelineRun } from '@/shared/types/domain'
import { useRealtime } from '@/shared/websocket/RealtimeProvider'

const pageSize = 20
const runStatuses = ['', 'running', 'completed', 'failed', 'cancelled']
const staleEventTypes = new Set(['pipeline_start', 'signal', 'error', 'pipeline_health'])

function cleanText(value?: string) {
  return value?.trim() ?? ''
}

function titleCase(value?: string) {
  if (!value) return 'Unknown'
  return value.replace(/_/g, ' ')
}

function dateToApi(value: string | null, endOfDay = false) {
  if (!value) return undefined
  return `${value}T${endOfDay ? '23:59:59.999' : '00:00:00.000'}Z`
}

function paramsFromSearch(searchParams: URLSearchParams): RunListParams {
  const offset = Number(searchParams.get('offset') ?? '0')
  return {
    status: cleanText(searchParams.get('status') ?? undefined) || undefined,
    strategy_id: cleanText(searchParams.get('strategy_id') ?? undefined) || undefined,
    ticker: cleanText(searchParams.get('ticker') ?? undefined).toUpperCase() || undefined,
    start_date: dateToApi(searchParams.get('start_date')),
    end_date: dateToApi(searchParams.get('end_date'), true),
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

function RunStatusPill({ value }: { value: string }) {
  const known = ['running', 'completed', 'failed', 'cancelled'].includes(value)
  return <span className={`status-pill ${known ? value : 'unknown'}`}>{known ? value : `Unknown: ${value}`}</span>
}

function SignalValue({ value }: { value?: string }) {
  if (!value) return <span className="muted">No signal</span>
  return <span>{titleCase(value)}</span>
}

function RunCard({ run }: { run: PipelineRun }) {
  return (
    <article className="strategy-card">
      <div className="panel-header">
        <h2><Link to={`/runs/${run.id}`}>{run.ticker}</Link></h2>
        <RunStatusPill value={run.status} />
      </div>
      <p><EntityLink kind="strategy" id={run.strategy_id} label="Open strategy" copy={false} /></p>
      <p>Signal: <SignalValue value={run.signal} /></p>
      <p className="muted">Started {new Date(run.started_at).toLocaleString()}</p>
    </article>
  )
}

export function RunsListPage() {
  const [searchParams, setSearchParams] = useSearchParams()
  const navigate = useNavigate()
  const realtime = useRealtime()
  const params = paramsFromSearch(searchParams)
  const [realtimeStale, setRealtimeStale] = useState(false)
  const lastEventKey = useRef<string | null>(null)
  const query = useQuery({
    queryKey: queryKeys.runsListFiltered(params),
    queryFn: ({ signal }) => getRuns(params, signal),
  })

  const rows = query.data?.data ?? []
  const offset = params.offset ?? 0
  const total = query.data?.total
  const hasNext = total === undefined ? rows.length === pageSize : offset + pageSize < total
  const hasPrevious = offset > 0
  const tableStale = query.isStale || realtimeStale || realtime.status === 'disconnected' || realtime.status === 'degraded'
  const totalLabel = total === undefined ? 'total unavailable' : `of ${total}`
  const visibleRange = rows.length === 0 ? '0' : `${offset + 1}–${offset + rows.length}`
  const filterSummary = useMemo(() => Object.entries(params).filter(([key, value]) => value !== undefined && key !== 'limit' && key !== 'offset').length, [params])

  useEffect(() => {
    const latest = realtime.events[0]
    if (!latest || !staleEventTypes.has(latest.type)) return
    const key = `${latest.timestamp}:${latest.type}:${latest.strategy_id ?? ''}:${latest.run_id ?? ''}`
    if (lastEventKey.current === key) return
    lastEventKey.current = key
    setRealtimeStale(true)
    void query.refetch()
  }, [query, realtime.events])

  function setOffset(nextOffset: number) {
    const next = new URLSearchParams(searchParams)
    if (nextOffset > 0) next.set('offset', String(nextOffset))
    else next.delete('offset')
    setSearchParams(next)
  }

  function onRowKeyDown(event: KeyboardEvent<HTMLTableRowElement>, id: string) {
    if (event.key === 'Enter') navigate(`/runs/${id}`)
  }

  return (
    <div className="detail-stack runs-page">
      <section className="panel hero-panel">
        <div className="panel-header">
          <div>
            <p className="eyebrow">Runs</p>
            <h1>Runs</h1>
            <p className="muted">Browse pipeline runs, preserve filter URLs, and deep-link to run or strategy evidence.</p>
          </div>
          <LastUpdated date={query.dataUpdatedAt || undefined} />
        </div>

        <form className="filter-bar" aria-label="Run filters" onSubmit={(event) => event.preventDefault()}>
          <label>
            Status
            <select value={searchParams.get('status') ?? ''} onChange={(event) => setSearchParams(updateSearch(searchParams, { status: event.target.value }))}>
              {runStatuses.map((status) => <option key={status || 'all'} value={status}>{status || 'All statuses'}</option>)}
            </select>
          </label>
          <label>
            Strategy ID
            <input value={searchParams.get('strategy_id') ?? ''} onChange={(event) => setSearchParams(updateSearch(searchParams, { strategy_id: event.target.value }))} placeholder="UUID" />
          </label>
          <label>
            Ticker
            <input value={searchParams.get('ticker') ?? ''} onChange={(event) => setSearchParams(updateSearch(searchParams, { ticker: event.target.value.toUpperCase() }))} placeholder="AUGR" />
          </label>
          <label>
            Start date
            <input type="date" value={searchParams.get('start_date') ?? ''} onChange={(event) => setSearchParams(updateSearch(searchParams, { start_date: event.target.value }))} />
          </label>
          <label>
            End date
            <input type="date" value={searchParams.get('end_date') ?? ''} onChange={(event) => setSearchParams(updateSearch(searchParams, { end_date: event.target.value }))} />
          </label>
          <button type="button" onClick={() => setSearchParams(new URLSearchParams())}>Clear filters</button>
        </form>
        <p className="muted">{filterSummary} active filters. Date filters are sent as UTC day bounds.</p>

        <StaleBanner show={tableStale && Boolean(query.data)} message="Run rows are read-only and may be stale. Refresh before acting from a future detail page." />
        {realtime.status === 'disconnected' || realtime.status === 'degraded' ? <p role="status" className="warning-box">WebSocket {realtime.status}; run rows may lag realtime changes.</p> : null}
        {query.isLoading ? <LoadingState label="Loading runs…" /> : null}
        {query.error ? <ErrorState error={query.error} onRetry={() => void query.refetch()} /> : null}
        {!query.isLoading && !query.error && rows.length === 0 ? <EmptyState title="No runs found" message="Adjust filters or wait for a pipeline run to start." /> : null}

        {rows.length > 0 ? (
          <>
            <div className="table-wrap" role="region" aria-label="Runs table" tabIndex={0}>
              <table>
                <thead>
                  <tr>
                    <th scope="col">Run</th>
                    <th scope="col">Strategy</th>
                    <th scope="col">Ticker</th>
                    <th scope="col">Status</th>
                    <th scope="col">Signal</th>
                    <th scope="col">Trade date</th>
                    <th scope="col">Started</th>
                    <th scope="col">Completed</th>
                  </tr>
                </thead>
                <tbody>
                  {rows.map((run) => (
                    <tr key={run.id} tabIndex={0} onKeyDown={(event) => onRowKeyDown(event, run.id)}>
                      <th scope="row"><Link to={`/runs/${run.id}`}>{run.id}</Link></th>
                      <td><EntityLink kind="strategy" id={run.strategy_id} copy={false} /></td>
                      <td>{run.ticker}</td>
                      <td><RunStatusPill value={run.status} /></td>
                      <td><SignalValue value={run.signal} /></td>
                      <td>{new Date(run.trade_date).toLocaleDateString()}</td>
                      <td>{new Date(run.started_at).toLocaleString()}</td>
                      <td>{run.completed_at ? new Date(run.completed_at).toLocaleString() : 'Not completed'}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
            <div className="card-list" aria-label="Runs cards">
              {rows.map((run) => <RunCard key={run.id} run={run} />)}
            </div>
            <nav className="pagination-controls" aria-label="Run pagination">
              <button type="button" disabled={!hasPrevious} onClick={() => setOffset(Math.max(0, offset - pageSize))}>Previous</button>
              <span className="muted">Showing {visibleRange} {totalLabel}</span>
              <button type="button" disabled={!hasNext} onClick={() => setOffset(offset + pageSize)}>Next</button>
            </nav>
          </>
        ) : null}
      </section>
    </div>
  )
}
