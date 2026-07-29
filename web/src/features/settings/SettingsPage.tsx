import { useQuery } from '@tanstack/react-query'

import { PageHeader } from '@/components/ui/page-header'
import { getSettings } from '@/shared/api/endpoints'
import { Breadcrumbs } from '@/shared/components/EntityLinks'
import { ErrorState, LastUpdated, LoadingState } from '@/shared/components/QueryStates'
import { queryKeys } from '@/shared/query/keys'

function percent(value?: number) {
  if (value === undefined) return '—'
  return `${(value * 100).toLocaleString(undefined, { maximumFractionDigits: 2 })}%`
}

export function SettingsPage() {
  const query = useQuery({ queryKey: queryKeys.settings, queryFn: ({ signal }) => getSettings(signal) })
  const settings = query.data
  const providers = settings ? Object.entries(settings.llm.providers) : []

  return (
    <div className="detail-stack">
      <Breadcrumbs items={[{ label: 'Cockpit', to: '/cockpit' }, { label: 'Settings & readiness' }]} />
      <PageHeader eyebrow="Administration" title="Settings & readiness" description="Read-only effective runtime configuration. Secrets are never returned; mutation workflows remain intentionally separate." actions={settings ? <span className={`status-pill ${settings.system.schema_status === 'current' ? 'success' : 'warning'}`}>Schema {settings.system.schema_status}</span> : undefined} />
      {query.isLoading ? <LoadingState label="Loading effective settings…" /> : null}
      {query.error ? <ErrorState error={query.error} onRetry={() => void query.refetch()} /> : null}
      {settings ? <>
        <LastUpdated date={query.dataUpdatedAt} />
        <section className="panel" aria-labelledby="runtime-readiness-heading">
          <h2 id="runtime-readiness-heading">Runtime readiness</h2>
          <dl className="kv-grid">
            <dt>Environment</dt><dd>{settings.system.environment}</dd>
            <dt>Version</dt><dd>{settings.system.version}</dd>
            <dt>Commit</dt><dd>{settings.system.build_commit || 'Unknown'}</dd>
            <dt>Built</dt><dd>{settings.system.build_time ? new Date(settings.system.build_time).toLocaleString() : 'Unknown'}</dd>
            <dt>Schema</dt><dd>{settings.system.current_schema_version} / required {settings.system.required_schema_version}</dd>
            <dt>Uptime</dt><dd>{Math.floor(settings.system.uptime_seconds / 60).toLocaleString()} minutes</dd>
          </dl>
        </section>
        <section className="panel" aria-labelledby="broker-readiness-heading">
          <h2 id="broker-readiness-heading">Broker readiness</h2>
          {settings.system.connected_brokers.length === 0 ? <p>No brokers reported.</p> : <div className="table-wrap"><table aria-label="Broker readiness"><thead><tr><th>Broker</th><th>Configured</th><th>Mode</th><th>Market data</th></tr></thead><tbody>{settings.system.connected_brokers.map((broker) => <tr key={broker.name}><th scope="row">{broker.name}</th><td>{broker.configured ? 'Configured' : 'Not configured'}</td><td><span className={`status-pill ${broker.paper_mode ? 'unknown' : 'warning'}`}>{broker.paper_mode ? 'Paper' : 'Live'}</span></td><td>{broker.data_environment ? <span className={`status-pill ${broker.data_environment === 'live' ? 'success' : 'warning'}`} title={broker.data_source_url}>{broker.data_environment}</span> : '—'}</td></tr>)}</tbody></table></div>}
        </section>
        <section className="panel" aria-labelledby="llm-readiness-heading">
          <h2 id="llm-readiness-heading">LLM readiness</h2>
          <p className="muted">Default: {settings.llm.default_provider} · deep: {settings.llm.deep_think_model} · quick: {settings.llm.quick_think_model}</p>
          <div className="table-wrap"><table aria-label="LLM provider readiness"><thead><tr><th>Provider</th><th>Credential</th><th>Model</th><th>Endpoint</th></tr></thead><tbody>{providers.map(([name, provider]) => <tr key={name}><th scope="row">{name}</th><td>{provider.api_key_configured ? `Configured${provider.api_key_last4 ? ` · …${provider.api_key_last4}` : ''}` : name === 'ollama' ? 'Local/no key' : 'Not configured'}</td><td>{provider.model || '—'}</td><td>{provider.base_url || 'Managed endpoint'}</td></tr>)}</tbody></table></div>
        </section>
        <section className="panel" aria-labelledby="risk-settings-heading">
          <h2 id="risk-settings-heading">Effective risk limits</h2>
          <dl className="kv-grid">
            <dt>Max position</dt><dd>{percent(settings.risk.max_position_size_pct)}</dd>
            <dt>Max daily loss</dt><dd>{percent(settings.risk.max_daily_loss_pct)}</dd>
            <dt>Max drawdown</dt><dd>{percent(settings.risk.max_drawdown_pct)}</dd>
            <dt>Max open positions</dt><dd>{settings.risk.max_open_positions}</dd>
            <dt>Max total exposure</dt><dd>{percent(settings.risk.max_total_exposure_pct)}</dd>
            <dt>Max per market</dt><dd>{percent(settings.risk.max_per_market_exposure_pct)}</dd>
          </dl>
        </section>
      </> : null}
    </div>
  )
}
