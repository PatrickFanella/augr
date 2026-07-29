import { useQuery } from '@tanstack/react-query'

import { PageHeader } from '@/components/ui/page-header'
import { getEventMarketsSummary, getPolymarketDataStatus } from '@/shared/api/endpoints'
import { Breadcrumbs } from '@/shared/components/EntityLinks'
import { ErrorState, LastUpdated, LoadingState } from '@/shared/components/QueryStates'
import { normalizeStatus } from '@/lib/status'
import { StatusBadge } from '@/components/ui/status-badge'

export function EventMarketsPage() {
  const summary = useQuery({ queryKey: ['event-markets', 'summary'], queryFn: ({ signal }) => getEventMarketsSummary(signal) })
  const polymarket = useQuery({ queryKey: ['event-markets', 'polymarket-data-status'], queryFn: ({ signal }) => getPolymarketDataStatus(signal), retry: false })
  return (
    <div className="detail-stack">
      <Breadcrumbs items={[{ label: 'Cockpit', to: '/cockpit' }, { label: 'Event markets' }]} />
      <PageHeader eyebrow="Prediction markets" title="Event markets" description="Shared paper-first readiness for Kalshi and Polymarket. Live readiness is reported by the backend and never inferred by this page." actions={<span className="status-pill unknown">Paper-first</span>} />
      <section className="panel" aria-labelledby="event-provider-heading">
        <div className="panel-header"><h2 id="event-provider-heading">Provider summary</h2>{summary.data ? <LastUpdated date={summary.dataUpdatedAt} /> : null}</div>
        {summary.isLoading ? <LoadingState label="Loading event-market summary…" /> : null}
        {summary.error ? <ErrorState error={summary.error} onRetry={() => void summary.refetch()} /> : null}
        {summary.data?.providers.length === 0 ? <p>No event-market providers are configured.</p> : null}
        {summary.data?.providers.length ? <div className="table-wrap"><table aria-label="Event market providers"><thead><tr><th>Provider</th><th>Market data</th><th>Watched</th><th>Active paper</th><th>Discovery</th><th>Live readiness</th></tr></thead><tbody>{summary.data.providers.map((provider) => <tr key={provider.provider}><th scope="row">{provider.provider}</th><td>{provider.data_environment ? <span className={`status-pill ${provider.data_status === 'stale' || provider.data_environment === 'demo' ? 'warning' : 'success'}`} title={provider.data_captured_at ? `Captured ${new Date(provider.data_captured_at).toLocaleString()}` : undefined}>{provider.data_status === 'stale' ? 'stale' : `${provider.data_environment} ${provider.data_status ?? ''}`.trim()}</span> : '—'}</td><td>{provider.watched_markets}</td><td>{provider.active_paper}</td><td><StatusBadge status={normalizeStatus(provider.last_run_status)} label={provider.last_run_status} /></td><td><span className={`status-pill ${provider.live_trading_ready ? 'warning' : 'unknown'}`}>{provider.live_trading_ready ? 'Backend reports ready' : 'Not ready'}</span></td></tr>)}</tbody></table></div> : null}
      </section>
      <section className="panel" aria-labelledby="polymarket-feed-heading">
        <div className="panel-header"><h2 id="polymarket-feed-heading">Polymarket market-data feed</h2>{polymarket.data ? <LastUpdated date={polymarket.data.updated_at} /> : null}</div>
        {polymarket.isLoading ? <LoadingState label="Loading Polymarket feed status…" /> : null}
        {polymarket.error ? <ErrorState error={polymarket.error} onRetry={() => void polymarket.refetch()} /> : null}
        {polymarket.data ? <dl className="kv-grid"><dt>Enabled</dt><dd>{polymarket.data.enabled ? 'Yes' : 'No'}</dd><dt>Connections</dt><dd>{polymarket.data.ws_connections}</dd><dt>Ready markets</dt><dd>{polymarket.data.ready_slugs.length}</dd><dt>Average jitter</dt><dd>{polymarket.data.avg_jitter_ms.toFixed(2)} ms</dd><dt>Recorder lag</dt><dd>{polymarket.data.recorder_lag_seconds.toFixed(2)} s</dd><dt>Dropped updates</dt><dd>{polymarket.data.dropped}</dd></dl> : null}
      </section>
    </div>
  )
}
