import { useQuery } from '@tanstack/react-query'
import { useEffect, useMemo, useState } from 'react'
import { useSearchParams } from 'react-router-dom'

import { getOrders } from '@/shared/api/endpoints'
import { Breadcrumbs, EntityLink } from '@/shared/components/EntityLinks'
import { EmptyState, ErrorState, LastUpdated, LoadingState, StaleBanner } from '@/shared/components/QueryStates'
import { queryKeys } from '@/shared/query/keys'
import type { Order } from '@/shared/types/domain'
import { useRealtime } from '@/shared/websocket/RealtimeProvider'

const pageSize = 20

function money(value?: number) {
  if (value === undefined) return '—'
  return new Intl.NumberFormat(undefined, { style: 'currency', currency: 'USD', maximumFractionDigits: 2 }).format(value)
}

function numberValue(value: number) {
  return new Intl.NumberFormat(undefined, { maximumFractionDigits: 4 }).format(value)
}

function OrderPill({ value, known }: { value: string; known: string[] }) {
  const normalized = value.replaceAll('_', ' ')
  return <span className={`status-pill ${known.includes(value) ? value : 'unknown'}`}>{known.includes(value) ? normalized : `Unknown: ${normalized}`}</span>
}

function OrdersRows({ orders }: { orders: Order[] }) {
  return (
    <>
      <div className="table-wrap">
        <table aria-label="Orders">
          <thead><tr><th>Order</th><th>Ticker</th><th>Side</th><th>Type</th><th>Status</th><th>Quantity</th><th>Filled</th><th>Limit</th><th>Broker</th><th>Created</th></tr></thead>
          <tbody>{orders.map((order) => (
            <tr key={order.id}>
              <td><EntityLink kind="order" id={order.id} />{order.strategy_id ? <><br /><EntityLink kind="strategy" id={order.strategy_id} copy={false} /></> : null}{order.pipeline_run_id ? <><br /><EntityLink kind="run" id={order.pipeline_run_id} copy={false} /></> : null}</td>
              <td>{order.ticker}</td>
              <td><OrderPill value={order.side} known={['buy', 'sell']} /></td>
              <td><OrderPill value={order.order_type} known={['market', 'limit', 'stop', 'stop_limit', 'trailing_stop']} /></td>
              <td><OrderPill value={order.status} known={['pending', 'submitted', 'partial', 'filled', 'cancelled', 'rejected']} /></td>
              <td>{numberValue(order.quantity)}</td>
              <td>{numberValue(order.filled_quantity)} {order.filled_avg_price !== undefined ? `@ ${money(order.filled_avg_price)}` : ''}</td>
              <td>{money(order.limit_price)}</td>
              <td>{order.broker}</td>
              <td>{new Date(order.created_at).toLocaleString()}</td>
            </tr>
          ))}</tbody>
        </table>
      </div>
      <div className="card-list" aria-label="Order cards">
        {orders.map((order) => (
          <article className="strategy-card" key={order.id}>
            <h3>{order.ticker}</h3>
            <p><OrderPill value={order.status} known={['pending', 'submitted', 'partial', 'filled', 'cancelled', 'rejected']} /> · {order.side} · {order.order_type}</p>
            <p>{numberValue(order.filled_quantity)} / {numberValue(order.quantity)} filled · {order.broker}</p>
            <EntityLink kind="order" id={order.id} label="Open order" copy={false} />
            {order.strategy_id ? <EntityLink kind="strategy" id={order.strategy_id} label="Open strategy" copy={false} /> : null}
            {order.pipeline_run_id ? <EntityLink kind="run" id={order.pipeline_run_id} label="Open run" copy={false} /> : null}
          </article>
        ))}
      </div>
    </>
  )
}

export function OrdersListPage() {
  const [searchParams, setSearchParams] = useSearchParams()
  const realtime = useRealtime()
  const [realtimeStale, setRealtimeStale] = useState(false)
  const offset = Number(searchParams.get('offset') ?? '0')
  const filters = useMemo(() => ({
    ticker: searchParams.get('ticker') || undefined,
    broker: searchParams.get('broker') || undefined,
    market_type: searchParams.get('market_type') || undefined,
    status: searchParams.get('status') || undefined,
    side: searchParams.get('side') || undefined,
    order_type: searchParams.get('order_type') || undefined,
    limit: pageSize,
    offset: Number.isFinite(offset) && offset > 0 ? offset : 0,
  }), [offset, searchParams])
  const query = useQuery({ queryKey: queryKeys.ordersListFiltered(filters), queryFn: ({ signal }) => getOrders(filters, signal) })
  const orders = query.data?.data ?? []
  const total = query.data?.total
  const currentOffset = filters.offset ?? 0
  const hasNext = total === undefined ? orders.length === pageSize : currentOffset + pageSize < total

  useEffect(() => {
    const latest = realtime.events[0]
    if (!latest) return
    if (latest.type === 'order_submitted' || latest.type === 'order_filled') {
      setRealtimeStale(true)
      void query.refetch()
    }
  }, [query, realtime.events])

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
      <Breadcrumbs items={[{ label: 'Cockpit', to: '/cockpit' }, { label: 'Orders' }]} />
      <section className="panel hero-panel">
        <p className="eyebrow">Read-only execution evidence</p>
        <div className="panel-header"><div><h1>Orders</h1><p className="muted">Browse recent orders and deep-link to strategy/run evidence. Detail, fills, cancel, and replace are excluded.</p></div><span className="status-pill active">Read-only</span></div>
        <StaleBanner show={realtimeStale || realtime.status === 'disconnected' || realtime.status === 'degraded'} message="Order rows are read-only and may be stale after realtime order activity." />
      </section>
      <section className="panel" aria-labelledby="orders-heading">
        <div className="panel-header"><div><h2 id="orders-heading">Recent orders</h2><p className="muted">Backend-supported filters: ticker, broker, market type, status, side, order type. Strategy/run filters are deferred until backend support exists.</p></div>{query.data ? <LastUpdated date={query.dataUpdatedAt} /> : null}</div>
        <form className="filter-bar" aria-label="Order filters" onSubmit={(event) => event.preventDefault()}>
          <label>Ticker<input value={searchParams.get('ticker') ?? ''} onChange={(event) => updateFilters({ ticker: event.target.value.toUpperCase() })} placeholder="AUGR" /></label>
          <label>Status<select value={searchParams.get('status') ?? ''} onChange={(event) => updateFilters({ status: event.target.value })}><option value="">All</option><option value="pending">Pending</option><option value="submitted">Submitted</option><option value="partial">Partial</option><option value="filled">Filled</option><option value="cancelled">Cancelled</option><option value="rejected">Rejected</option></select></label>
          <label>Side<select value={searchParams.get('side') ?? ''} onChange={(event) => updateFilters({ side: event.target.value })}><option value="">All</option><option value="buy">Buy</option><option value="sell">Sell</option></select></label>
          <label>Order type<select value={searchParams.get('order_type') ?? ''} onChange={(event) => updateFilters({ order_type: event.target.value })}><option value="">All</option><option value="market">Market</option><option value="limit">Limit</option><option value="stop">Stop</option><option value="stop_limit">Stop limit</option><option value="trailing_stop">Trailing stop</option></select></label>
          <label>Broker<input value={searchParams.get('broker') ?? ''} onChange={(event) => updateFilters({ broker: event.target.value })} placeholder="paper-broker" /></label>
          <button type="button" onClick={() => updateFilters({ ticker: '', status: '', side: '', order_type: '', broker: '', market_type: '' })}>Clear filters</button>
        </form>
        {query.isLoading ? <LoadingState label="Loading orders…" /> : null}
        {query.error ? <ErrorState error={query.error} onRetry={() => void query.refetch()} /> : null}
        {query.data && orders.length === 0 ? <EmptyState title="No orders found" message="No orders match these filters." /> : null}
        {orders.length > 0 ? <OrdersRows orders={orders} /> : null}
        {orders.length > 0 ? <nav className="pagination-controls" aria-label="Order pagination"><button type="button" className="secondary-button" disabled={currentOffset === 0} onClick={() => setOffset(Math.max(0, currentOffset - pageSize))}>Previous</button><span className="muted">Showing {currentOffset + 1}–{currentOffset + orders.length} {total === undefined ? 'total unavailable' : `of ${total}`}</span><button type="button" className="secondary-button" disabled={!hasNext} onClick={() => setOffset(currentOffset + pageSize)}>Next</button></nav> : null}
      </section>
    </div>
  )
}
