import { EventTimeline } from '@/features/events/EventTimeline'
import { PageHeader } from '@/components/ui/page-header'
import { Breadcrumbs } from '@/shared/components/EntityLinks'

export function EventsPage() {
  return (
    <div className="detail-stack">
      <Breadcrumbs items={[{ label: 'Cockpit', to: '/cockpit' }, { label: 'Events' }]} />
      <PageHeader eyebrow="Timeline" title="Persisted events" description="Stored agent and pipeline events. Live activity remains in the shell drawer; this page queries persisted evidence." />
      <EventTimeline />
    </div>
  )
}
