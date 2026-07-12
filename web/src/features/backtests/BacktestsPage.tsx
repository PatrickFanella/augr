import { useQuery } from '@tanstack/react-query'
import { useSearchParams } from 'react-router-dom'

import { PageHeader } from '@/components/ui/page-header'
import { getBacktestConfigs, getBacktestRuns } from '@/shared/api/endpoints'
import { Breadcrumbs, EntityLink } from '@/shared/components/EntityLinks'
import { EmptyState, ErrorState, LastUpdated, LoadingState } from '@/shared/components/QueryStates'

export function BacktestsPage() {
  const [searchParams, setSearchParams] = useSearchParams()
  const strategyId = searchParams.get('strategy_id') ?? ''
  const configId = searchParams.get('backtest_config_id') ?? ''
  const configs = useQuery({ queryKey: ['backtests', 'configs', strategyId], queryFn: ({ signal }) => getBacktestConfigs({ strategy_id: strategyId || undefined, limit: 20, offset: 0 }, signal) })
  const runs = useQuery({ queryKey: ['backtests', 'runs', configId], queryFn: ({ signal }) => getBacktestRuns({ backtest_config_id: configId || undefined, limit: 20, offset: 0 }, signal) })
  const update = (key: string, value: string) => {
    const next = new URLSearchParams(searchParams)
    if (value) next.set(key, value)
    else next.delete(key)
    setSearchParams(next)
  }
  return (
    <div className="detail-stack">
      <Breadcrumbs items={[{ label: 'Cockpit', to: '/cockpit' }, { label: 'Backtests' }]} />
      <PageHeader eyebrow="Research evidence" title="Backtests" description="Read-only simulation definitions with versioned input fingerprints. Runs use explicit fill assumptions and next-bar execution; paper divergence is available through the strategy-scoped API." actions={<span className="status-pill unknown">Evidence only</span>} />
      <section className="panel" aria-labelledby="backtest-configs-heading">
        <div className="panel-header"><h2 id="backtest-configs-heading">Configurations</h2>{configs.data ? <LastUpdated date={configs.dataUpdatedAt} /> : null}</div>
        <form className="filter-bar" aria-label="Backtest configuration filters" onSubmit={(event) => event.preventDefault()}><label>Strategy ID<input value={strategyId} onChange={(event) => update('strategy_id', event.target.value)} placeholder="UUID" /></label><button type="button" onClick={() => update('strategy_id', '')}>Clear</button></form>
        {configs.isLoading ? <LoadingState label="Loading backtest configurations…" /> : null}{configs.error ? <ErrorState error={configs.error} onRetry={() => void configs.refetch()} /> : null}
        {configs.data && configs.data.data.length === 0 ? <EmptyState title="No backtest configurations" message="No simulation definitions match this filter." /> : null}
        {configs.data?.data.length ? <div className="table-wrap"><table aria-label="Backtest configurations"><thead><tr><th>Name</th><th>Strategy</th><th>Window</th><th>Initial capital</th><th>Volume cap</th><th>Latest run</th></tr></thead><tbody>{configs.data.data.map((config) => <tr key={config.id}><th scope="row">{config.name}</th><td><EntityLink kind="strategy" id={config.strategy_id} /></td><td>{new Date(config.start_date).toLocaleDateString()} – {new Date(config.end_date).toLocaleDateString()}</td><td>{config.simulation.initial_capital.toLocaleString()}</td><td>{config.simulation.max_volume_pct === undefined ? 'Unlimited' : `${(config.simulation.max_volume_pct * 100).toFixed(2)}%`}</td><td>{config.latest_run_summary ? new Date(config.latest_run_summary.run_timestamp).toLocaleString() : 'Never'}</td></tr>)}</tbody></table></div> : null}
      </section>
      <section className="panel" aria-labelledby="backtest-runs-heading">
        <div className="panel-header"><h2 id="backtest-runs-heading">Runs</h2>{runs.data ? <LastUpdated date={runs.dataUpdatedAt} /> : null}</div>
        <form className="filter-bar" aria-label="Backtest run filters" onSubmit={(event) => event.preventDefault()}><label>Configuration ID<input value={configId} onChange={(event) => update('backtest_config_id', event.target.value)} placeholder="UUID" /></label><button type="button" onClick={() => update('backtest_config_id', '')}>Clear</button></form>
        {runs.isLoading ? <LoadingState label="Loading backtest runs…" /> : null}{runs.error ? <ErrorState error={runs.error} onRetry={() => void runs.refetch()} /> : null}
        {runs.data && runs.data.data.length === 0 ? <EmptyState title="No backtest runs" message="No persisted runs match this filter." /> : null}
        {runs.data?.data.length ? <div className="table-wrap"><table aria-label="Backtest runs"><thead><tr><th>Run</th><th>Configuration</th><th>Timestamp</th><th>Duration</th><th>Prompt version</th><th>Prompt hash</th><th>Simulation</th><th>Input hash</th></tr></thead><tbody>{runs.data.data.map((run) => <tr key={run.id}><th scope="row">{run.id}</th><td>{run.backtest_config_id}</td><td>{new Date(run.run_timestamp).toLocaleString()}</td><td>{(run.duration / 1e6).toLocaleString(undefined, { maximumFractionDigits: 2 })} ms</td><td>{run.prompt_version}</td><td><code>{run.prompt_version_hash.slice(0, 12)}…</code></td><td>{run.simulation_version ?? 'Legacy'}</td><td>{run.input_hash ? <code>{run.input_hash.slice(0, 12)}…</code> : 'Unavailable'}</td></tr>)}</tbody></table></div> : null}
      </section>
    </div>
  )
}
