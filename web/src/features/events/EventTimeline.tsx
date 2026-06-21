import { useQuery } from '@tanstack/react-query'
import { useSearchParams } from 'react-router-dom'

import { getEvents, type EventListParams } from '@/shared/api/endpoints'
import { EntityId, EntityLink } from '@/shared/components/EntityLinks'
import { EmptyState, ErrorState, LastUpdated, LoadingState } from '@/shared/components/QueryStates'
import { queryKeys } from '@/shared/query/keys'
import type { AgentEvent } from '@/shared/types/domain'

const pageSize = 20

function titleCase(value?: string) {
  if (!value) return 'Unknown'
  return value.replace(/_/g, ' ')
}

function JsonMetadata({ value }: { value: unknown }) {
  return <pre tabIndex={0}>{JSON.stringify(value ?? {}, null, 2)}</pre>
}

function EventLinks({ event }: { event: AgentEvent }) {
  const metadata = event.metadata && typeof event.metadata === 'object' && !Array.isArray(event.metadata) ? event.metadata as Record<string, unknown> : {}
  const orderId = typeof metadata.order_id === 'string' ? metadata.order_id : undefined
  const positionId = typeof metadata.position_id === 'string' ? metadata.position_id : undefined
  const decisionId = typeof metadata.decision_id === 'string' ? metadata.decision_id : undefined
  return (
    <div className="header-cluster">
      {event.strategy_id ? <EntityLink kind="strategy" id={event.strategy_id} label="Strategy" copy={false} /> : null}
      {event.pipeline_run_id ? <EntityLink kind="run" id={event.pipeline_run_id} label="Run" copy={false} /> : null}
      {orderId ? <EntityLink kind="order" id={orderId} label="Order" copy={false} /> : null}
      {positionId ? <EntityLink kind="position" id={positionId} label="Position trades" copy={false} /> : null}
      {decisionId ? <EntityId kind="decision" id={decisionId} /> : null}
    </div>
  )
}

function EventCard({ event }: { event: AgentEvent }) {
  return (
    <article className="panel decision-card">
      <div className="panel-header">
        <div>
          <h3>{event.title}</h3>
          <p className="muted">{titleCase(event.event_kind)} · {new Date(event.created_at).toLocaleString()}</p>
        </div>
        <EventLinks event={event} />
      </div>
      {event.summary ? <p>{event.summary}</p> : null}
      <dl className="kv-grid compact-kv">
        <dt>Event ID</dt><dd><EntityId kind="event" id={event.id} /></dd>
        <dt>Agent role</dt><dd>{event.agent_role || 'Unknown'}</dd>
        <dt>Tags</dt><dd>{event.tags?.join(', ') || 'None'}</dd>
      </dl>
      {event.metadata ? <JsonMetadata value={event.metadata} /> : null}
    </article>
  )
}

export function EventTimeline({ fixedRunId, fixedStrategyId }: { fixedRunId?: string; fixedStrategyId?: string }) {
  const [searchParams, setSearchParams] = useSearchParams()
  const offset = Number(searchParams.get('event_offset') ?? searchParams.get('offset') ?? '0')
  const filters: EventListParams = {
    event_kind: searchParams.get('event_kind') || undefined,
    pipeline_run_id: fixedRunId ?? searchParams.get('pipeline_run_id') ?? searchParams.get('run_id') ?? undefined,
    strategy_id: fixedStrategyId ?? searchParams.get('strategy_id') ?? undefined,
    agent_role: searchParams.get('agent_role') || undefined,
    after: searchParams.get('after') || undefined,
    before: searchParams.get('before') || undefined,
    limit: pageSize,
    offset: Number.isFinite(offset) && offset > 0 ? offset : 0,
  }
  const query = useQuery({ queryKey: queryKeys.eventsListFiltered(filters), queryFn: ({ signal }) => getEvents(filters, signal) })
  const events = query.data?.data ?? []
  const total = query.data?.total
  const currentOffset = filters.offset ?? 0
  const hasNext = total === undefined ? events.length === pageSize : currentOffset + pageSize < total

  function updateFilters(updates: Record<string, string>) {
    const next = new URLSearchParams(searchParams)
    if (fixedRunId || fixedStrategyId) next.set('tab', 'timeline')
    for (const [key, value] of Object.entries(updates)) {
      if (value) next.set(key, value)
      else next.delete(key)
    }
    next.delete(fixedRunId || fixedStrategyId ? 'event_offset' : 'offset')
    setSearchParams(next)
  }

  function setOffset(nextOffset: number) {
    const next = new URLSearchParams(searchParams)
    if (fixedRunId || fixedStrategyId) next.set('tab', 'timeline')
    const key = fixedRunId || fixedStrategyId ? 'event_offset' : 'offset'
    if (nextOffset > 0) next.set(key, String(nextOffset))
    else next.delete(key)
    setSearchParams(next)
  }

  return (
    <section className="panel" aria-labelledby="timeline-heading">
      <div className="panel-header">
        <div>
          <h2 id="timeline-heading">Persisted event timeline</h2>
          <p className="muted">Read-only stored agent and pipeline events. Backend filters use event_kind, after, and before.</p>
        </div>
        {query.data ? <LastUpdated date={query.dataUpdatedAt} /> : null}
      </div>
      <form className="filter-bar" aria-label="Event filters" onSubmit={(event) => event.preventDefault()}>
        <label>Event kind<input value={searchParams.get('event_kind') ?? ''} onChange={(event) => updateFilters({ event_kind: event.target.value })} placeholder="agent_decision" /></label>
        {!fixedRunId ? <label>Run ID<input value={searchParams.get('pipeline_run_id') ?? searchParams.get('run_id') ?? ''} onChange={(event) => updateFilters({ pipeline_run_id: event.target.value, run_id: '' })} /></label> : null}
        {!fixedStrategyId ? <label>Strategy ID<input value={searchParams.get('strategy_id') ?? ''} onChange={(event) => updateFilters({ strategy_id: event.target.value })} /></label> : null}
        <label>Agent role<input value={searchParams.get('agent_role') ?? ''} onChange={(event) => updateFilters({ agent_role: event.target.value })} placeholder="analyst" /></label>
        <label>After<input type="datetime-local" value={searchParams.get('after')?.slice(0, 16) ?? ''} onChange={(event) => updateFilters({ after: event.target.value ? `${event.target.value}:00.000Z` : '' })} /></label>
        <button type="button" onClick={() => updateFilters({ event_kind: '', pipeline_run_id: '', run_id: '', strategy_id: '', agent_role: '', after: '', before: '' })}>Clear filters</button>
      </form>
      {query.isLoading ? <LoadingState label="Loading persisted events…" /> : null}
      {query.error ? <ErrorState error={query.error} onRetry={() => void query.refetch()} /> : null}
      {query.data && events.length === 0 ? <EmptyState title="No persisted events" message="No stored events match these filters." /> : null}
      {events.length > 0 ? (
        <>
          <div className="decision-list">
            {events.map((event) => <EventCard key={event.id} event={event} />)}
          </div>
          <nav className="pagination-controls" aria-label="Event pagination">
            <button type="button" className="secondary-button" disabled={currentOffset === 0} onClick={() => setOffset(Math.max(0, currentOffset - pageSize))}>Previous</button>
            <span className="muted">Showing {currentOffset + 1}–{currentOffset + events.length} {total === undefined ? 'total unavailable' : `of ${total}`}</span>
            <button type="button" className="secondary-button" disabled={!hasNext} onClick={() => setOffset(currentOffset + pageSize)}>Next</button>
          </nav>
        </>
      ) : null}
    </section>
  )
}
