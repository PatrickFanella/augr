import { useQuery } from '@tanstack/react-query'
import { useEffect, useState } from 'react'
import { useParams } from 'react-router-dom'

import { getOrder } from '@/shared/api/endpoints'
import { PageHeader } from '@/components/ui/page-header'
import { Breadcrumbs, EntityId, EntityLink } from '@/shared/components/EntityLinks'
import { EmptyState, ErrorState, LastUpdated, LoadingState, StaleBanner } from '@/shared/components/QueryStates'
import { queryKeys } from '@/shared/query/keys'
import { StatusBadge } from '@/components/ui/status-badge'
import { normalizeStatus } from '@/lib/status'
import type { Order, Trade } from '@/shared/types/domain'
import { useRealtime } from '@/shared/websocket/RealtimeProvider'

function money(value?: number) {
  if (value === undefined) return '—'
  return new Intl.NumberFormat(undefined, { style: 'currency', currency: 'USD', maximumFractionDigits: 2 }).format(value)
}

function numberValue(value?: number) {
  if (value === undefined) return '—'
  return new Intl.NumberFormat(undefined, { maximumFractionDigits: 4 }).format(value)
}

function timeValue(value?: string) {
  if (!value) return '—'
  return new Date(value).toLocaleString()
}

function displayEnum(value?: string) {
  return value ? value.replaceAll('_', ' ') : '—'
}

function DetailPill({ value, known }: { value: string; known: string[] }) {
  const isKnown = known.includes(value)
  return <StatusBadge status={isKnown ? normalizeStatus(value) : 'unknown'} label={isKnown ? displayEnum(value) : `Unknown: ${displayEnum(value)}`} />
}

function OrderSummary({ order }: { order: Order }) {
  return (
    <section className="panel" aria-labelledby="order-summary-heading">
      <div className="panel-header"><div><h2 id="order-summary-heading">Order summary</h2><p className="muted">Broker order state, execution quantities, and linked operational evidence.</p></div></div>
      <dl className="kv-grid">
        <div><dt>Order ID</dt><dd><EntityId kind="order" id={order.id} /></dd></div>
        <div><dt>External ID</dt><dd>{order.external_id ?? '—'}</dd></div>
        <div><dt>Ticker</dt><dd>{order.ticker}</dd></div>
        <div><dt>Market</dt><dd>{displayEnum(order.market_type)}</dd></div>
        <div><dt>Side</dt><dd><DetailPill value={order.side} known={['buy', 'sell']} /></dd></div>
        <div><dt>Type</dt><dd><DetailPill value={order.order_type} known={['market', 'limit', 'stop', 'stop_limit', 'trailing_stop']} /></dd></div>
        <div><dt>Status</dt><dd><DetailPill value={order.status} known={['pending', 'submitted', 'partial', 'filled', 'cancelled', 'rejected']} /></dd></div>
        <div><dt>Broker</dt><dd>{order.broker}</dd></div>
        <div><dt>Quantity</dt><dd>{numberValue(order.quantity)}</dd></div>
        <div><dt>Filled</dt><dd>{numberValue(order.filled_quantity)} {order.filled_avg_price !== undefined ? `@ ${money(order.filled_avg_price)}` : ''}</dd></div>
        <div><dt>Limit</dt><dd>{money(order.limit_price)}</dd></div>
        <div><dt>Stop</dt><dd>{money(order.stop_price)}</dd></div>
        <div><dt>Created</dt><dd>{timeValue(order.created_at)}</dd></div>
        <div><dt>Submitted</dt><dd>{timeValue(order.submitted_at)}</dd></div>
        <div><dt>Filled at</dt><dd>{timeValue(order.filled_at)}</dd></div>
      </dl>
    </section>
  )
}

function LinkedEvidence({ order, fills }: { order: Order; fills: Trade[] }) {
  const positionIds = Array.from(new Set(fills.map((fill) => fill.position_id).filter(Boolean)))
  return (
    <section className="panel" aria-labelledby="order-evidence-heading">
      <div className="panel-header"><div><h2 id="order-evidence-heading">Linked evidence</h2><p className="muted">Order detail links back to strategy, run, and fill/position evidence when IDs exist.</p></div></div>
      <div className="detail-grid">
        <article className="nested-panel"><h3>Strategy</h3>{order.strategy_id ? <EntityLink kind="strategy" id={order.strategy_id} label="Open strategy" /> : <p className="muted">No strategy ID recorded.</p>}</article>
        <article className="nested-panel"><h3>Run</h3>{order.pipeline_run_id ? <EntityLink kind="run" id={order.pipeline_run_id} label="Open run" /> : <p className="muted">No pipeline run ID recorded.</p>}</article>
        <article className="nested-panel"><h3>Positions</h3>{positionIds.length > 0 ? <ul>{positionIds.map((id) => <li key={id}><EntityLink kind="position" id={id} label="Position trades" /></li>)}</ul> : <p className="muted">No fill position IDs recorded.</p>}</article>
      </div>
    </section>
  )
}

function FillsTable({ fills }: { fills: Trade[] }) {
  if (fills.length === 0) return <EmptyState title="No fills recorded" message="This order has no recorded trade fills yet." />
  return (
    <>
      <div className="table-wrap">
        <table aria-label="Order fills">
          <thead><tr><th>Fill</th><th>Ticker</th><th>Side</th><th>Quantity</th><th>Price</th><th>Fee</th><th>Position</th><th>Executed</th></tr></thead>
          <tbody>{fills.map((fill) => (
            <tr key={fill.id}>
              <td><EntityId kind="trade" id={fill.id} />{fill.external_id ? <><br /><span className="muted">{fill.external_id}</span></> : null}</td>
              <td>{fill.ticker}</td>
              <td><DetailPill value={fill.side} known={['buy', 'sell']} /></td>
              <td>{numberValue(fill.quantity)}</td>
              <td>{money(fill.price)}</td>
              <td>{money(fill.fee)}</td>
              <td>{fill.position_id ? <EntityLink kind="position" id={fill.position_id} label="Position trades" /> : '—'}</td>
              <td>{timeValue(fill.executed_at)}</td>
            </tr>
          ))}</tbody>
        </table>
      </div>
      <div className="card-list" aria-label="Order fill cards">
        {fills.map((fill) => (
          <article className="strategy-card" key={fill.id}>
            <h3>{fill.ticker}</h3>
            <p><DetailPill value={fill.side} known={['buy', 'sell']} /> · {numberValue(fill.quantity)} @ {money(fill.price)}</p>
            <p>Fee {money(fill.fee)} · Executed {timeValue(fill.executed_at)}</p>
            {fill.position_id ? <p><EntityLink kind="position" id={fill.position_id} label="Position trades" /></p> : null}
          </article>
        ))}
      </div>
    </>
  )
}

export function OrderDetailPage() {
  const { id } = useParams()
  const realtime = useRealtime()
  const [realtimeStale, setRealtimeStale] = useState(false)
  const orderId = id ?? ''
  const query = useQuery({
    queryKey: queryKeys.orderDetail(orderId),
    queryFn: ({ signal }) => getOrder(orderId, signal),
    enabled: Boolean(orderId),
  })
  const detail = query.data

  useEffect(() => {
    const latest = realtime.events[0]
    if (!latest || !detail) return
    if (latest.type === 'order_filled') {
      const payload = latest.data && typeof latest.data === 'object' ? latest.data as Record<string, unknown> : {}
      if (payload.order_id === detail.order.id || latest.run_id === detail.order.pipeline_run_id || latest.strategy_id === detail.order.strategy_id) {
        setRealtimeStale(true)
        void query.refetch()
      }
    }
  }, [detail, query, realtime.events])

  if (query.isLoading) return <LoadingState label="Loading order…" />
  if (query.error) return <ErrorState error={query.error} onRetry={() => void query.refetch()} />
  if (!detail) return <EmptyState title="Order not found" message="No order detail is available for this route." />

  const order = detail.order
  return (
    <div className="detail-stack">
      <Breadcrumbs items={[{ label: 'Cockpit', to: '/cockpit' }, { label: 'Orders', to: '/orders' }, { label: order.ticker }]} />
      <PageHeader eyebrow="Read-only order detail" title={`${order.ticker} order`} description="Broker order state, execution quantities, and linked operational evidence." actions={<div className="header-actions"><DetailPill value={order.status} known={['pending', 'submitted', 'partial', 'filled', 'cancelled', 'rejected']} /><span className="status-pill active">Read-only</span></div>} />
      <p className="muted"><EntityId kind="order" id={order.id} /></p>
      <StaleBanner show={realtimeStale || query.isStale || realtime.status === 'disconnected' || realtime.status === 'degraded'} message="Order detail and fills are read-only and may be stale after realtime fill activity." />
      <LastUpdated date={query.dataUpdatedAt} />
      <OrderSummary order={order} />
      <LinkedEvidence order={order} fills={detail.fills} />
      <section className="panel" aria-labelledby="order-fills-heading">
        <div className="panel-header"><div><h2 id="order-fills-heading">Fills</h2><p className="muted">Fills are trade records returned with the order detail response.</p></div></div>
        <FillsTable fills={detail.fills} />
      </section>
    </div>
  )
}
