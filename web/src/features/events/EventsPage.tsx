import { EventTimeline } from '@/features/events/EventTimeline'
import { Breadcrumbs } from '@/shared/components/EntityLinks'

export function EventsPage() {
  return (
    <div className="detail-stack">
      <Breadcrumbs items={[{ label: 'Cockpit', to: '/cockpit' }, { label: 'Events' }]} />
      <section className="panel hero-panel">
        <p className="eyebrow">Timeline</p>
        <h1>Persisted events</h1>
        <p className="muted">Stored agent and pipeline events. Live activity remains in the shell drawer; this page queries persisted evidence.</p>
      </section>
      <EventTimeline />
    </div>
  )
}
