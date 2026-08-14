import { useMemo, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { ExternalLink, RefreshCw } from 'lucide-react'

import { PageHeader } from '@/components/ui/page-header'
import {
  addCopySource,
  createCopyLeader,
  createCopySubscription,
  getCopyIntents,
  getCopyLeader,
  getCopyLeaders,
  getCopySubscriptions,
  previewCopySubscription,
  rebalanceCopySubscription,
  refreshCopySource,
  setCopySubscriptionStatus,
  upsertCopyMapping,
} from '@/shared/api/endpoints'
import type { CopyPreview, CopySubscription } from '@/shared/types/domain'
import { Breadcrumbs, EntityLink } from '@/shared/components/EntityLinks'
import { EmptyState, ErrorState, LoadingState } from '@/shared/components/QueryStates'
import { queryKeys } from '@/shared/query/keys'

const money = new Intl.NumberFormat(undefined, { style: 'currency', currency: 'USD', maximumFractionDigits: 0 })
const pct = (value: number) => `${(value * 100).toFixed(1)}%`

export function CopyTradingPage() {
  const queryClient = useQueryClient()
  const [selectedLeaderId, setSelectedLeaderId] = useState('')
  const [selectedSubscriptionId, setSelectedSubscriptionId] = useState('')
  const [preview, setPreview] = useState<CopyPreview | null>(null)
  const [notice, setNotice] = useState('')
  const [leaderName, setLeaderName] = useState('')
  const [cik, setCik] = useState('')
  const [mappingCUSIP, setMappingCUSIP] = useState('')
  const [mappingTicker, setMappingTicker] = useState('')
  const [capitalBudget, setCapitalBudget] = useState(10000)
  const [topN, setTopN] = useState(10)

  const leaders = useQuery({ queryKey: queryKeys.copyLeaders, queryFn: ({ signal }) => getCopyLeaders(signal) })
  const subscriptions = useQuery({ queryKey: queryKeys.copySubscriptions, queryFn: ({ signal }) => getCopySubscriptions(signal) })
  const leaderId = selectedLeaderId || leaders.data?.data[0]?.id || ''
  const selectedLeader = useQuery({ queryKey: queryKeys.copyLeader(leaderId), queryFn: ({ signal }) => getCopyLeader(leaderId, signal), enabled: Boolean(leaderId) })
  const selectedSubscription = useMemo(() => subscriptions.data?.data.find((item) => item.id === selectedSubscriptionId) ?? subscriptions.data?.data[0], [selectedSubscriptionId, subscriptions.data])
  const intents = useQuery({ queryKey: queryKeys.copyIntents(selectedSubscription?.id ?? ''), queryFn: ({ signal }) => getCopyIntents(selectedSubscription!.id, signal), enabled: Boolean(selectedSubscription?.id) })

  const refreshAll = async () => {
    await Promise.all([
      queryClient.invalidateQueries({ queryKey: queryKeys.copyLeaders }),
      queryClient.invalidateQueries({ queryKey: queryKeys.copySubscriptions }),
      leaderId ? queryClient.invalidateQueries({ queryKey: queryKeys.copyLeader(leaderId) }) : Promise.resolve(),
      selectedSubscription?.id ? queryClient.invalidateQueries({ queryKey: queryKeys.copyIntents(selectedSubscription.id) }) : Promise.resolve(),
    ])
  }

  const createLeaderMutation = useMutation({
    mutationFn: async () => {
      const leader = await createCopyLeader({ entity_type: 'institution', display_name: leaderName.trim(), sec_cik: cik.trim() })
      await addCopySource(leader.id, { provider: 'sec', source_type: 'sec_13f', external_key: cik.trim() })
      return leader
    },
    onSuccess: async (leader) => { setLeaderName(''); setCik(''); setSelectedLeaderId(leader.id); setNotice('Institution and SEC 13F source created. Refresh the source to ingest its latest filing.'); await refreshAll() },
  })

  const refreshSourceMutation = useMutation({
    mutationFn: (sourceId: string) => refreshCopySource(sourceId),
    onSuccess: async (result) => { setNotice(result.created ? `Imported ${result.snapshot.holding_count} holdings from the latest filing.` : 'The latest filing was already imported.'); await refreshAll() },
  })

  const mappingMutation = useMutation({
    mutationFn: () => upsertCopyMapping({ provider: 'sec', identifier_type: 'cusip', identifier_value: mappingCUSIP.trim(), ticker: mappingTicker.trim(), confidence: 'manual_verified', mapping_method: 'manual' }),
    onSuccess: () => { setMappingCUSIP(''); setMappingTicker(''); setNotice('Verified CUSIP mapping saved.'); },
  })

  const createSubscriptionMutation = useMutation({
    mutationFn: () => {
      const detail = selectedLeader.data
      const source = detail?.sources.find((item) => item.source_type === 'sec_13f')
      if (!detail || !source) throw new Error('Refresh or add an SEC 13F source first.')
      return createCopySubscription({ leader_id: detail.leader.id, source_id: source.id, is_paper: true, method: 'target_weight', capital_budget: capitalBudget, cash_buffer_pct: 0.1, top_n: topN, min_source_weight: 0.01, max_position_weight: 0.15, max_turnover_pct: 0.25, min_price: 5, min_avg_dollar_volume: 1_000_000, max_spread_bps: 100 })
    },
    onSuccess: async (created) => { setSelectedSubscriptionId(created.id); setNotice('Paper subscription created. Preview it before activation.'); await refreshAll() },
  })

  const previewMutation = useMutation({ mutationFn: (id: string) => previewCopySubscription(id), onSuccess: async (result) => { setPreview(result); setNotice(`Preview generated with ${result.intents.length} trade intents.`); await refreshAll() } })
  const statusMutation = useMutation({ mutationFn: ({ id, action }: { id: string; action: 'activate' | 'pause' | 'resume' | 'stop' }) => setCopySubscriptionStatus(id, action), onSuccess: async (result) => { setNotice(`Subscription is now ${result.status.replaceAll('_', ' ')}.`); await refreshAll() } })
  const rebalanceMutation = useMutation({ mutationFn: (id: string) => rebalanceCopySubscription(id), onSuccess: async (result) => { setPreview(result.preview); setNotice(`Rebalance completed with ${result.intents.length} persisted intents. Open the linked run for execution details.`); await refreshAll() } })

  const mutationError = createLeaderMutation.error ?? refreshSourceMutation.error ?? mappingMutation.error ?? createSubscriptionMutation.error ?? previewMutation.error ?? statusMutation.error ?? rebalanceMutation.error
  const source = selectedLeader.data?.sources.find((item) => item.source_type === 'sec_13f')

  return (
    <div className="detail-stack">
      <Breadcrumbs items={[{ label: 'Cockpit', to: '/cockpit' }, { label: 'Copy trading' }]} />
      <PageHeader eyebrow="Stock replication" title="Copy trading" description="Follow public institutional stock disclosures through deterministic, risk-gated paper portfolios. 13F filings are delayed snapshots—not real-time trades—so every rebalance starts with a reviewable preview." actions={<span className="status-pill warning">Paper only</span>} />
      {notice ? <div className="success-box" role="status">{notice}</div> : null}
      {mutationError ? <div className="error-box" role="alert">{mutationError instanceof Error ? mutationError.message : 'The operation failed.'}</div> : null}

      <div className="grid gap-4 xl:grid-cols-2">
        <section className="panel" aria-labelledby="leaders-heading">
          <div className="panel-header"><h2 id="leaders-heading">Institutions and sources</h2><button className="btn-icon" type="button" aria-label="Reload institutions" onClick={() => void leaders.refetch()}><RefreshCw size={14} /></button></div>
          <form className="filter-bar" onSubmit={(event) => { event.preventDefault(); createLeaderMutation.mutate() }}>
            <label>Name<input required value={leaderName} onChange={(event) => setLeaderName(event.target.value)} placeholder="Berkshire Hathaway" /></label>
            <label>SEC CIK<input required inputMode="numeric" value={cik} onChange={(event) => setCik(event.target.value)} placeholder="1067983" /></label>
            <button className="primary-button" disabled={createLeaderMutation.isPending} type="submit">Add institution</button>
          </form>
          {leaders.isLoading ? <LoadingState label="Loading institutions…" /> : null}
          {leaders.error ? <ErrorState error={leaders.error} onRetry={() => void leaders.refetch()} /> : null}
          {leaders.data?.data.length === 0 ? <EmptyState title="No followed institutions" message="Add an institution by its SEC CIK to begin." /> : null}
          {leaders.data?.data.length ? <div className="table-wrap"><table><thead><tr><th>Institution</th><th>CIK</th><th>Identity</th></tr></thead><tbody>{leaders.data.data.map((leader) => <tr key={leader.id} onClick={() => setSelectedLeaderId(leader.id)} className={leader.id === leaderId ? 'selected' : ''}><th scope="row"><button type="button" className="link-button">{leader.display_name}</button></th><td><code>{leader.sec_cik ?? '—'}</code></td><td>{leader.identity_status.replaceAll('_', ' ')}</td></tr>)}</tbody></table></div> : null}
          {source ? <div className="action-row p-4"><span className={`status-pill ${source.last_observed_at ? 'success' : 'unknown'}`}>{source.last_observed_at ? `Observed ${new Date(source.last_observed_at).toLocaleString()}` : 'Not ingested'}</span><button type="button" disabled={refreshSourceMutation.isPending} onClick={() => refreshSourceMutation.mutate(source.id)}>Refresh latest 13F</button></div> : null}
        </section>

        <section className="panel" aria-labelledby="mapping-heading">
          <div className="panel-header"><h2 id="mapping-heading">Verified instrument mapping</h2></div>
          <p className="muted p-4 pb-0">13F reports identify holdings by CUSIP. Add a reviewed stock ticker mapping before that holding can receive capital.</p>
          <form className="filter-bar" onSubmit={(event) => { event.preventDefault(); mappingMutation.mutate() }}>
            <label>CUSIP<input required value={mappingCUSIP} onChange={(event) => setMappingCUSIP(event.target.value.toUpperCase())} placeholder="084670702" /></label>
            <label>Ticker<input required value={mappingTicker} onChange={(event) => setMappingTicker(event.target.value.toUpperCase())} placeholder="BRK.B" /></label>
            <button className="primary-button" disabled={mappingMutation.isPending} type="submit">Save mapping</button>
          </form>
          <div className="warning-box m-4">Unmapped, derivative, blocked, or illiquid holdings stay as cash. Their weight is never redistributed silently.</div>
        </section>
      </div>

      <section className="panel" aria-labelledby="subscriptions-heading">
        <div className="panel-header"><h2 id="subscriptions-heading">Paper subscriptions</h2></div>
        <form className="filter-bar" onSubmit={(event) => { event.preventDefault(); createSubscriptionMutation.mutate() }}>
          <label>Selected institution<input readOnly value={selectedLeader.data?.leader.display_name ?? ''} placeholder="Select an institution above" /></label>
          <label>Capital budget<input required type="number" min="100" step="100" value={capitalBudget} onChange={(event) => setCapitalBudget(Number(event.target.value))} /></label>
          <label>Top holdings<input required type="number" min="1" max="100" value={topN} onChange={(event) => setTopN(Number(event.target.value))} /></label>
          <button className="primary-button" disabled={!source || createSubscriptionMutation.isPending} type="submit">Create paper subscription</button>
        </form>
        {subscriptions.isLoading ? <LoadingState label="Loading subscriptions…" /> : null}
        {subscriptions.error ? <ErrorState error={subscriptions.error} onRetry={() => void subscriptions.refetch()} /> : null}
        {subscriptions.data?.data.length === 0 ? <EmptyState title="No subscriptions" message="Ingest a source and create a paper allocation policy." /> : null}
        {subscriptions.data?.data.length ? <div className="table-wrap"><table><thead><tr><th>Status</th><th>Budget</th><th>Top N</th><th>Cash buffer</th><th>Turnover cap</th><th>Strategy</th></tr></thead><tbody>{subscriptions.data.data.map((item) => <tr key={item.id} onClick={() => { setSelectedSubscriptionId(item.id); setPreview(null) }} className={item.id === selectedSubscription?.id ? 'selected' : ''}><th scope="row"><button type="button" className="link-button"><span className={`status-pill ${item.status === 'paper_active' ? 'success' : item.status === 'stopped' ? 'danger' : 'unknown'}`}>{item.status.replaceAll('_', ' ')}</span></button></th><td>{money.format(item.capital_budget)}</td><td>{item.top_n}</td><td>{pct(item.cash_buffer_pct)}</td><td>{pct(item.max_turnover_pct)}</td><td><EntityLink kind="strategy" id={item.strategy_id} /></td></tr>)}</tbody></table></div> : null}
      </section>

      {selectedSubscription ? <SubscriptionOperations subscription={selectedSubscription} preview={preview} intents={intents.data?.data ?? []} pending={previewMutation.isPending || statusMutation.isPending || rebalanceMutation.isPending} onPreview={() => previewMutation.mutate(selectedSubscription.id)} onAction={(action) => statusMutation.mutate({ id: selectedSubscription.id, action })} onRebalance={() => rebalanceMutation.mutate(selectedSubscription.id)} /> : null}
    </div>
  )
}

function SubscriptionOperations({ subscription, preview, intents, pending, onPreview, onAction, onRebalance }: { subscription: CopySubscription; preview: CopyPreview | null; intents: CopyPreview['intents']; pending: boolean; onPreview: () => void; onAction: (action: 'activate' | 'pause' | 'resume' | 'stop') => void; onRebalance: () => void }) {
  const visibleIntents = preview?.intents ?? intents
  return <section className="panel" aria-labelledby="operations-heading">
    <div className="panel-header"><h2 id="operations-heading">Review and operate</h2><span className="muted"><code>{subscription.id}</code></span></div>
    <div className="action-row p-4">
      <button type="button" disabled={pending || subscription.status === 'stopped'} onClick={onPreview}>Generate preview</button>
      {subscription.status === 'draft' || subscription.status === 'previewed' ? <button className="primary-button" type="button" disabled={pending} onClick={() => onAction('activate')}>Activate paper</button> : null}
      {subscription.status === 'paper_active' ? <><button type="button" disabled={pending} onClick={onRebalance}>Rebalance now</button><button type="button" disabled={pending} onClick={() => onAction('pause')}>Pause</button></> : null}
      {subscription.status === 'paused' ? <button className="primary-button" type="button" disabled={pending} onClick={() => onAction('resume')}>Resume</button> : null}
      {subscription.status !== 'stopped' ? <button className="danger-button" type="button" disabled={pending} onClick={() => onAction('stop')}>Stop</button> : null}
    </div>
    {preview ? <div className="grid gap-3 p-4 pt-0 sm:grid-cols-2 xl:grid-cols-4"><Metric label="Mapped source weight" value={pct(preview.summary.mapped_weight)} /><Metric label="Unmapped weight → cash" value={pct(preview.summary.unmapped_weight)} /><Metric label="Target invested" value={money.format(preview.summary.target_invested_value)} /><Metric label="Approved turnover" value={money.format(preview.summary.approved_turnover)} /></div> : null}
    {preview?.summary.warnings.length ? <div className="warning-box m-4">{preview.summary.warnings.join(' · ')}</div> : null}
    {visibleIntents.length ? <div className="table-wrap"><table><thead><tr><th>Ticker</th><th>Side</th><th>Target weight</th><th>Requested</th><th>Price</th><th>Policy</th><th>Execution</th></tr></thead><tbody>{visibleIntents.map((intent, index) => <tr key={`${intent.id}-${intent.ticker}-${intent.side}-${index}`}><th scope="row">{intent.ticker}</th><td>{intent.side}</td><td>{pct(intent.target_weight)}</td><td>{money.format(intent.requested_notional)}</td><td>{intent.executable_price ? money.format(intent.executable_price) : '—'}</td><td>{intent.policy_status}{intent.policy_reasons.length ? `: ${intent.policy_reasons.join(', ')}` : ''}</td><td>{intent.order_id ? <EntityLink kind="order" id={intent.order_id} /> : intent.status}{intent.pipeline_run_id ? <span className="ml-2"><EntityLink kind="run" id={intent.pipeline_run_id} /></span> : null}</td></tr>)}</tbody></table></div> : <EmptyState title="No trade intents" message="Generate a preview after importing a filing and mapping at least one disclosed holding." />}
    {preview?.observation.source_url ? <div className="p-4"><a href={preview.observation.source_url} target="_blank" rel="noreferrer">Open source filing <ExternalLink size={13} /></a></div> : null}
  </section>
}

function Metric({ label, value }: { label: string; value: string }) {
  return <div className="rounded border border-[var(--color-border)] p-3"><span className="muted block text-xs">{label}</span><strong className="text-lg">{value}</strong></div>
}
