import { useQuery } from '@tanstack/react-query'
import { Link, useParams } from 'react-router-dom'

import { PageHeader } from '@/components/ui/page-header'
import { getDecisionReplay } from '@/shared/api/endpoints'
import { Breadcrumbs } from '@/shared/components/EntityLinks'
import { EmptyState, ErrorState, LoadingState } from '@/shared/components/QueryStates'

function payload(value: unknown) {
  try { return JSON.stringify(value, null, 2) }
  catch { return '[payload cannot be displayed]' }
}

export function ReplayPage() {
  const { id = '' } = useParams()
  const query = useQuery({ queryKey: ['replay', id], queryFn: ({ signal }) => getDecisionReplay(id, signal), enabled: Boolean(id) })
  const replay = query.data
  const events = [...(replay?.events ?? [])].sort((left, right) => Date.parse(left.occurred_at) - Date.parse(right.occurred_at))
  return <div className="detail-stack">
    <Breadcrumbs items={[{ label: 'Decision journal', to: '/journal' }, { label: id || 'Replay' }]} />
    <PageHeader eyebrow="Audit trail" title="Replay workbench" description="A deterministic, read-only timeline reconstructed from the persisted decision and replay events." actions={<Link to="/journal">Back to journal</Link>} />
    {query.isLoading ? <LoadingState label="Loading decision replay…" /> : null}
    {query.error ? <ErrorState error={query.error} onRetry={() => void query.refetch()} /> : null}
    {replay ? <>
      <section className="panel" aria-labelledby="replay-summary-heading"><h2 id="replay-summary-heading">Decision summary</h2><dl className="definition-grid"><dt>Instrument</dt><dd>{replay.source.instrument_key}</dd><dt>Status</dt><dd>{replay.summary.latest_status}</dd><dt>Events</dt><dd>{replay.summary.event_count}</dd><dt>Approved size</dt><dd>{replay.summary.total_approved_size.toLocaleString()}</dd><dt>Net EV</dt><dd>{replay.summary.total_net_ev.toLocaleString()}</dd><dt>Evidence flags</dt><dd>{['paper order', 'live order', 'fill', 'outcome'].filter((_, index) => [replay.summary.has_paper_order, replay.summary.has_live_order, replay.summary.has_fill, replay.summary.has_outcome][index]).join(' · ') || 'None'}</dd></dl></section>
      <section className="panel" aria-labelledby="replay-events-heading"><h2 id="replay-events-heading">Timeline</h2>{events.length === 0 ? <EmptyState title="No replay events" message="The source decision exists, but no lifecycle events have been persisted." /> : <ol className="event-list">{events.map((event) => <li key={event.id}><article className="event-card"><header><strong>{event.event_type}</strong><span className="muted">{new Date(event.occurred_at).toLocaleString()} · {event.source}</span></header><pre>{payload(event.payload)}</pre></article></li>)}</ol>}</section>
    </> : null}
  </div>
}
