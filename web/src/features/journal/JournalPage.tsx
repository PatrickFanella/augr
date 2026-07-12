import { useQuery } from '@tanstack/react-query'
import { Link, useSearchParams } from 'react-router-dom'

import { PageHeader } from '@/components/ui/page-header'
import { getTradeDecisions } from '@/shared/api/endpoints'
import { Breadcrumbs, EntityLink } from '@/shared/components/EntityLinks'
import { EmptyState, ErrorState, LastUpdated, LoadingState } from '@/shared/components/QueryStates'

const marketTypes = ['', 'stock', 'crypto', 'polymarket', 'kalshi', 'options']
const statuses = ['', 'candidate', 'rejected', 'paper_ordered', 'live_ordered', 'closed']

function money(value: number) {
  return new Intl.NumberFormat('en-US', { style: 'currency', currency: 'USD' }).format(value)
}

export function JournalPage() {
  const [params, setParams] = useSearchParams()
  const marketType = params.get('market_type') ?? ''
  const status = params.get('status') ?? ''
  const query = useQuery({ queryKey: ['journal', marketType, status], queryFn: ({ signal }) => getTradeDecisions({ market_type: marketType || undefined, status: status || undefined, limit: 50, offset: 0 }, signal) })
  const update = (key: string, value: string) => {
    const next = new URLSearchParams(params)
    if (value) next.set(key, value)
    else next.delete(key)
    setParams(next)
  }

  return <div className="detail-stack">
    <Breadcrumbs items={[{ label: 'Cockpit', to: '/cockpit' }, { label: 'Decision journal' }]} />
    <PageHeader eyebrow="Audit spine" title="Decision journal" description="Read-only evidence, risk review, LLM provenance, and order references for persisted trading decisions." actions={<span className="status-pill unknown">Read only</span>} />
    <section className="panel" aria-labelledby="journal-heading">
      <div className="panel-header"><h2 id="journal-heading">Recorded decisions</h2>{query.data ? <LastUpdated date={query.dataUpdatedAt} /> : null}</div>
      <form className="filter-bar" aria-label="Decision journal filters" onSubmit={(event) => event.preventDefault()}>
        <label>Market<select value={marketType} onChange={(event) => update('market_type', event.target.value)}>{marketTypes.map((value) => <option key={value} value={value}>{value || 'All markets'}</option>)}</select></label>
        <label>Status<select value={status} onChange={(event) => update('status', event.target.value)}>{statuses.map((value) => <option key={value} value={value}>{value || 'All statuses'}</option>)}</select></label>
      </form>
      {query.isLoading ? <LoadingState label="Loading decision journal…" /> : null}
      {query.error ? <ErrorState error={query.error} onRetry={() => void query.refetch()} /> : null}
      {query.data?.data.length === 0 ? <EmptyState title={marketType || status ? 'No decisions match these filters' : 'No decisions recorded'} message="The journal is available, but no persisted decisions were returned." /> : null}
      {query.data?.data.length ? <div className="table-wrap"><table aria-label="Trade decision journal"><thead><tr><th>Created</th><th>Instrument</th><th>Decision</th><th>Risk</th><th>LLM provenance</th><th>Orders</th><th>Replay</th></tr></thead><tbody>{query.data.data.map((decision) => <tr key={decision.id}>
        <td>{new Date(decision.created_at).toLocaleString()}</td>
        <th scope="row"><span className="status-pill unknown">{decision.market_type}</span><br />{decision.instrument_key}<br /><span className="muted">{decision.side}{decision.outcome ? ` · ${decision.outcome}` : ''}</span></th>
        <td>{decision.status}<br /><span className="muted">Net EV {money(decision.net_ev)} · approved {decision.approved_size.toLocaleString()}</span></td>
        <td>{decision.risk_status}<br /><span className="muted">{decision.risk_reasons.join(' · ') || 'No reasons recorded'}</span></td>
        <td>{decision.llm_provider || 'Not recorded'} / {decision.llm_model || 'Not recorded'}<br /><span className="muted">{decision.prompt_tokens ?? '—'} prompt · {decision.completion_tokens ?? '—'} completion · {decision.latency_ms ?? '—'} ms</span></td>
        <td>{decision.paper_order_id ? <EntityLink kind="order" id={decision.paper_order_id} label="Paper order" /> : 'No paper order'}{decision.live_order_id ? <><br /><EntityLink kind="order" id={decision.live_order_id} label="Live order" /></> : null}</td>
        <td><Link to={`/replay/decisions/${decision.id}`}>Open replay</Link></td>
      </tr>)}</tbody></table></div> : null}
    </section>
  </div>
}
