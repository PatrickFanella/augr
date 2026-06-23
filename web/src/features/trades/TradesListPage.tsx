import { useQuery } from '@tanstack/react-query'
import { useEffect, useMemo, useState } from 'react'
import { useSearchParams } from 'react-router-dom'

import { getTrades } from '@/shared/api/endpoints'
import { PageHeader } from '@/components/ui/page-header'
import { Breadcrumbs, EntityId, EntityLink } from '@/shared/components/EntityLinks'
import { EmptyState, ErrorState, LastUpdated, LoadingState, StaleBanner } from '@/shared/components/QueryStates'
import { queryKeys } from '@/shared/query/keys'
import type { Trade } from '@/shared/types/domain'
import { useRealtime } from '@/shared/websocket/RealtimeProvider'

const pageSize = 20

function money(value: number) {
  return new Intl.NumberFormat(undefined, { style: 'currency', currency: 'USD', maximumFractionDigits: 4 }).format(value)
}

function numberValue(value: number) {
  return new Intl.NumberFormat(undefined, { maximumFractionDigits: 4 }).format(value)
}

function dateBound(value: string | null, end = false) {
  if (!value) return undefined
  return `${value}T${end ? '23:59:59.999' : '00:00:00.000'}Z`
}

function TradePill({ value, known }: { value: string; known: string[] }) {
  const normalized = value.replaceAll('_', ' ')
  return <span className={`status-pill ${known.includes(value) ? value : 'unknown'}`}>{known.includes(value) ? normalized : `Unknown: ${normalized}`}</span>
}

function TradesRows({ trades }: { trades: Trade[] }) {
  return (
    <>
      <div className="table-wrap">
        <table aria-label="Trades">
          <thead><tr><th>Trade</th><th>Ticker</th><th>Side</th><th>Quantity</th><th>Price</th><th>Fee</th><th>Order</th><th>Position</th><th>Executed</th></tr></thead>
          <tbody>{trades.map((trade) => (
            <tr key={trade.id}>
              <td><EntityId kind="trade" id={trade.id} />{trade.external_id ? <><br /><span className="muted">{trade.external_id}</span></> : null}</td>
              <td>{trade.ticker}</td>
              <td><TradePill value={trade.side} known={['buy', 'sell']} /></td>
              <td>{numberValue(trade.quantity)}</td>
              <td>{money(trade.price)}</td>
              <td>{money(trade.fee)}</td>
              <td>{trade.order_id ? <EntityLink kind="order" id={trade.order_id} label="Open order" /> : '—'}</td>
              <td>{trade.position_id ? <EntityLink kind="position" id={trade.position_id} label="Position trades" /> : '—'}</td>
              <td>{new Date(trade.executed_at).toLocaleString()}</td>
            </tr>
          ))}</tbody>
        </table>
      </div>
      <div className="card-list" aria-label="Trade cards">
        {trades.map((trade) => (
          <article className="strategy-card" key={trade.id}>
            <h3>{trade.ticker}</h3>
            <p><TradePill value={trade.side} known={['buy', 'sell']} /> · {numberValue(trade.quantity)} @ {money(trade.price)}</p>
            <p>Fee {money(trade.fee)} · {new Date(trade.executed_at).toLocaleString()}</p>
            {trade.order_id ? <EntityLink kind="order" id={trade.order_id} label="Open order" /> : null}
            {trade.position_id ? <p className="muted"><EntityLink kind="position" id={trade.position_id} label="Position trades" /></p> : null}
          </article>
        ))}
      </div>
    </>
  )
}

export function TradesListPage() {
  const [searchParams, setSearchParams] = useSearchParams()
  const realtime = useRealtime()
  const [realtimeStale, setRealtimeStale] = useState(false)
  const offset = Number(searchParams.get('offset') ?? '0')
  const scopeConflict = Boolean(searchParams.get('order_id') && searchParams.get('position_id'))
  const filters = useMemo(() => ({
    order_id: searchParams.get('order_id') || undefined,
    position_id: searchParams.get('position_id') || undefined,
    ticker: searchParams.get('ticker') || undefined,
    side: searchParams.get('side') || undefined,
    start_date: dateBound(searchParams.get('start_date')),
    end_date: dateBound(searchParams.get('end_date'), true),
    limit: pageSize,
    offset: Number.isFinite(offset) && offset > 0 ? offset : 0,
  }), [offset, searchParams])
  const query = useQuery({
    queryKey: queryKeys.tradesListFiltered(filters),
    queryFn: ({ signal }) => getTrades(filters, signal),
    enabled: !scopeConflict,
  })
  const trades = query.data?.data ?? []
  const total = query.data?.total
  const currentOffset = filters.offset ?? 0
  const hasNext = total === undefined ? trades.length === pageSize : currentOffset + pageSize < total

  useEffect(() => {
    const latest = realtime.events[0]
    if (!latest) return
    if (latest.type === 'order_filled') {
      setRealtimeStale(true)
      if (!scopeConflict) void query.refetch()
    }
  }, [query, realtime.events, scopeConflict])

  function updateFilters(updates: Record<string, string>) {
    const next = new URLSearchParams(searchParams)
    for (const [key, value] of Object.entries(updates)) {
      if (value) next.set(key, value)
      else next.delete(key)
    }
    next.delete('offset')
    setSearchParams(next)
  }

  function setOffset(nextOffset: number) {
    const next = new URLSearchParams(searchParams)
    if (nextOffset > 0) next.set('offset', String(nextOffset))
    else next.delete('offset')
    setSearchParams(next)
  }

  return (
    <div className="detail-stack">
      <Breadcrumbs items={[{ label: 'Cockpit', to: '/cockpit' }, { label: 'Trades' }]} />
      <PageHeader eyebrow="Read-only execution evidence" title="Trades" description="Browse fills/executions and trace them back to orders and positions. Trade detail and broker actions are excluded." actions={<span className="status-pill active">Read-only</span>} />
      <StaleBanner show={realtimeStale || realtime.status === 'disconnected' || realtime.status === 'degraded'} message="Trade rows are read-only and may be stale after realtime order fills." />
      <section className="panel" aria-labelledby="trades-heading">
        <div className="panel-header"><div><h2 id="trades-heading">Recent trades</h2><p className="muted">Backend filters support either order ID or position ID, plus ticker, side, executed date range, and pagination.</p></div>{query.data ? <LastUpdated date={query.dataUpdatedAt} /> : null}</div>
        <form className="filter-bar" aria-label="Trade filters" onSubmit={(event) => event.preventDefault()}>
          <label>Order ID<input value={searchParams.get('order_id') ?? ''} onChange={(event) => updateFilters({ order_id: event.target.value, position_id: '' })} placeholder="UUID" /></label>
          <label>Position ID<input value={searchParams.get('position_id') ?? ''} onChange={(event) => updateFilters({ position_id: event.target.value, order_id: '' })} placeholder="UUID" /></label>
          <label>Ticker<input value={searchParams.get('ticker') ?? ''} onChange={(event) => updateFilters({ ticker: event.target.value.toUpperCase() })} placeholder="AUGR" /></label>
          <label>Side<select value={searchParams.get('side') ?? ''} onChange={(event) => updateFilters({ side: event.target.value })}><option value="">All</option><option value="buy">Buy</option><option value="sell">Sell</option></select></label>
          <label>Start date<input type="date" value={searchParams.get('start_date') ?? ''} onChange={(event) => updateFilters({ start_date: event.target.value })} /></label>
          <label>End date<input type="date" value={searchParams.get('end_date') ?? ''} onChange={(event) => updateFilters({ end_date: event.target.value })} /></label>
          <button type="button" onClick={() => updateFilters({ order_id: '', position_id: '', ticker: '', side: '', start_date: '', end_date: '' })}>Clear filters</button>
        </form>
        {scopeConflict ? <div role="alert" className="inline-alert error">Use either order ID or position ID, not both.</div> : null}
        {query.isLoading ? <LoadingState label="Loading trades…" /> : null}
        {query.error ? <ErrorState error={query.error} onRetry={() => void query.refetch()} /> : null}
        {query.data && trades.length === 0 ? <EmptyState title="No trades found" message="No trades match these filters." /> : null}
        {trades.length > 0 ? <TradesRows trades={trades} /> : null}
        {trades.length > 0 ? <nav className="pagination-controls" aria-label="Trade pagination"><button type="button" className="secondary-button" disabled={currentOffset === 0} onClick={() => setOffset(Math.max(0, currentOffset - pageSize))}>Previous</button><span className="muted">Showing {currentOffset + 1}–{currentOffset + trades.length} {total === undefined ? 'total unavailable' : `of ${total}`}</span><button type="button" className="secondary-button" disabled={!hasNext} onClick={() => setOffset(currentOffset + pageSize)}>Next</button></nav> : null}
      </section>
    </div>
  )
}
