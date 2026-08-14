import { useQuery } from '@tanstack/react-query'

import { PageHeader } from '@/components/ui/page-header'
import { getEventMarketsSummary } from '@/shared/api/endpoints'
import { Breadcrumbs } from '@/shared/components/EntityLinks'
import { ErrorState, LastUpdated, LoadingState } from '@/shared/components/QueryStates'
import { normalizeStatus } from '@/lib/status'
import { StatusBadge } from '@/components/ui/status-badge'

export function EventMarketsPage() {
  const summary = useQuery({ queryKey: ['event-markets', 'summary'], queryFn: ({ signal }) => getEventMarketsSummary(signal) })
  const providers = summary.data?.providers.filter((provider) => provider.provider.toLowerCase() === 'kalshi')
  return (
    <div className="detail-stack">
      <Breadcrumbs items={[{ label: 'Cockpit', to: '/cockpit' }, { label: 'Event markets' }]} />
      <PageHeader eyebrow="Prediction markets" title="Event markets" description="Paper-first readiness for Kalshi. Live readiness is reported by the backend and never inferred by this page." actions={<span className="status-pill unknown">Paper-first</span>} />
      <section className="panel" aria-labelledby="event-provider-heading">
        <div className="panel-header"><h2 id="event-provider-heading">Provider summary</h2>{summary.data ? <LastUpdated date={summary.dataUpdatedAt} /> : null}</div>
        {summary.isLoading ? <LoadingState label="Loading event-market summary…" /> : null}
        {summary.error ? <ErrorState error={summary.error} onRetry={() => void summary.refetch()} /> : null}
        {providers?.length === 0 ? <p>No event-market providers are configured.</p> : null}
        {providers?.length ? <div className="table-wrap"><table aria-label="Event market providers"><thead><tr><th>Provider</th><th>Market data</th><th>Watched</th><th>Active paper</th><th>Discovery</th><th>Live readiness</th></tr></thead><tbody>{providers.map((provider) => <tr key={provider.provider}><th scope="row">{provider.provider}</th><td>{provider.data_environment ? <span className={`status-pill ${provider.data_status === 'stale' || provider.data_environment === 'demo' ? 'warning' : 'success'}`} title={provider.data_captured_at ? `Captured ${new Date(provider.data_captured_at).toLocaleString()}` : undefined}>{provider.data_status === 'stale' ? 'stale' : `${provider.data_environment} ${provider.data_status ?? ''}`.trim()}</span> : '—'}</td><td>{provider.watched_markets}</td><td>{provider.active_paper}</td><td><StatusBadge status={normalizeStatus(provider.last_run_status)} label={provider.last_run_status} /></td><td><span className={`status-pill ${provider.live_trading_ready ? 'warning' : 'unknown'}`}>{provider.live_trading_ready ? 'Backend reports ready' : 'Not ready'}</span></td></tr>)}</tbody></table></div> : null}
      </section>
    </div>
  )
}
