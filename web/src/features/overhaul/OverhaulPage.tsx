import { useEffect, useMemo, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import {
  AlertTriangle,
  ArrowDownLeft,
  ArrowUpRight,
  BookOpenCheck,
  Check,
  CircleDollarSign,
  Database,
  FileSearch,
  Fingerprint,
  GitBranch,
  LockKeyhole,
  RefreshCw,
  Search,
  ShieldCheck,
  WalletCards,
} from 'lucide-react'

import { PageHeader } from '@/components/ui/page-header'
import {
  getEconomicAccounts,
  getEconomicCapitalFlows,
  getEconomicCapitalSummary,
  getEconomicLedgerTransaction,
  getMilestoneAssessment,
  getReleaseReadiness,
} from '@/shared/api/endpoints'
import { isApiClientError } from '@/shared/api/errors'
import { Breadcrumbs } from '@/shared/components/EntityLinks'
import { EmptyState, ErrorState, FeatureUnavailable, LastUpdated, LoadingState } from '@/shared/components/QueryStates'
import { queryKeys } from '@/shared/query/keys'
import type { EconomicAccount, ReleaseCapability } from '@/shared/types/domain'

const UUID_PATTERN = /^[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/i

const adoptionStages = [
  { id: 'R0', title: 'Evidence inspection', state: 'available', detail: 'Read-only milestone assessments are runtime-wired.' },
  { id: 'R1', title: 'Accounts & ledger', state: 'available', detail: 'Read-only economic projections are opt-in and fail closed.' },
  { id: 'R2', title: 'Execution & reconciliation', state: 'planned', detail: 'Lifecycle projections and paper execution remain unwired.' },
  { id: 'R3', title: 'Research lifecycle', state: 'planned', detail: 'Immutable manifests and promotion decisions remain unwired.' },
  { id: 'R4', title: 'Copy & prediction', state: 'planned', detail: 'Runtime adoption requires licensed evidence and venue gates.' },
  { id: 'R5', title: 'Unattended control plane', state: 'planned', detail: 'Scheduler, supervisor, costs, and daily brief remain unwired.' },
  { id: 'R6', title: 'Elapsed evidence', state: 'external', detail: 'Real 30–90 day campaigns require separate authorization.' },
] as const

function statusClass(value: string | boolean) {
  const normalized = String(value).toLowerCase()
  if (['true', 'ready', 'qualified', 'active', 'paper_scored', 'available'].includes(normalized)) return 'success'
  if (['false', 'not_ready', 'held', 'paused', 'planned', 'external'].includes(normalized)) return 'warning'
  if (['blocked', 'rejected', 'closed'].includes(normalized)) return 'danger'
  return 'unknown'
}

function formatExactMoney(value: string, currency = 'USD') {
  const [whole = '0', fraction = ''] = value.split('.')
  const negative = whole.startsWith('-')
  const digits = negative ? whole.slice(1) : whole
  const grouped = digits.replace(/\B(?=(\d{3})+(?!\d))/g, ',')
  const cents = fraction.padEnd(2, '0').slice(0, Math.max(2, Math.min(fraction.length, 8)))
  return `${negative ? '-' : ''}${currency} ${grouped}.${cents}`
}

function shortId(id: string) {
  return `${id.slice(0, 8)}…${id.slice(-4)}`
}

function QueryError({ error, retry }: { error: unknown; retry: () => void }) {
  if (isApiClientError(error) && error.kind === 'not_implemented') {
    return <FeatureUnavailable message="This read model is disabled on the current runtime. Enable its server-side inspection gate to expose it." />
  }
  return <ErrorState error={error} onRetry={retry} />
}

function ReadinessPanel() {
  const query = useQuery({
    queryKey: queryKeys.releaseReadiness,
    queryFn: ({ signal }) => getReleaseReadiness(signal),
    refetchInterval: 60_000,
  })
  const required = query.data?.capabilities.filter((item) => item.required) ?? []
  const ready = required.filter((item) => item.ready).length

  return (
    <section className="overhaul-readiness panel" aria-labelledby="release-readiness-title">
      <div className="panel-header">
        <div>
          <p className="eyebrow">Current runtime</p>
          <h2 id="release-readiness-title">Release readiness</h2>
        </div>
        <button type="button" className="btn-icon" onClick={() => void query.refetch()} aria-label="Reload release readiness"><RefreshCw size={15} /></button>
      </div>
      {query.isLoading ? <LoadingState label="Loading release readiness…" /> : null}
      {query.isError ? <QueryError error={query.error} retry={() => void query.refetch()} /> : null}
      {query.data ? (
        <>
          <div className={`readiness-verdict ${query.data.release_ready ? 'ready' : 'blocked'}`}>
            {query.data.release_ready ? <ShieldCheck aria-hidden="true" /> : <AlertTriangle aria-hidden="true" />}
            <div>
              <strong>{query.data.release_ready ? 'Paper release gates are ready' : 'Paper release is blocked'}</strong>
              <span>{ready} of {required.length} required capabilities ready</span>
            </div>
            <span className={`status-pill ${query.data.live_trading_enabled ? 'danger' : 'success'}`}>
              Live {query.data.live_trading_enabled ? 'enabled' : 'disabled'}
            </span>
          </div>
          <div className="capability-grid" aria-label="release capabilities">
            {query.data.capabilities.map((capability) => <CapabilityCard key={capability.name} capability={capability} />)}
          </div>
          <LastUpdated date={query.data.generated_at} />
        </>
      ) : null}
    </section>
  )
}

function CapabilityCard({ capability }: { capability: ReleaseCapability }) {
  return (
    <article className="capability-card">
      <div>
        <strong>{capability.name.replaceAll('_', ' ')}</strong>
        <span>{capability.mode} · {capability.required ? 'required' : 'optional'}</span>
      </div>
      <span className={`status-pill ${capability.ready ? 'success' : 'warning'}`}>{capability.ready ? 'ready' : 'blocked'}</span>
      {capability.blockers?.length ? <p title={capability.blockers.join(', ')}>{capability.blockers.join(' · ')}</p> : null}
    </article>
  )
}

function AccountRail({ accounts, selectedId, onSelect }: { accounts: EconomicAccount[]; selectedId?: string; onSelect: (id: string) => void }) {
  return (
    <div className="account-rail" role="list" aria-label="economic accounts">
      {accounts.map((account) => (
        <button
          type="button"
          role="listitem"
          key={account.id}
          className={selectedId === account.id ? 'selected' : ''}
          onClick={() => onSelect(account.id)}
          aria-pressed={selectedId === account.id}
        >
          <span className="account-mark"><WalletCards aria-hidden="true" /></span>
          <span className="account-rail-copy">
            <strong>{account.name}</strong>
            <small>{account.venue} · {formatExactMoney(account.starting_capital, account.base_currency)}</small>
          </span>
          <span className={`status-pill ${statusClass(account.status)}`}>{account.status}</span>
        </button>
      ))}
    </div>
  )
}

function EconomicWorkspace() {
  const accounts = useQuery({ queryKey: queryKeys.economicAccounts, queryFn: ({ signal }) => getEconomicAccounts(signal) })
  const [selectedId, setSelectedId] = useState<string>()
  const selected = accounts.data?.data.find((account) => account.id === selectedId)

  useEffect(() => {
    if (!selectedId && accounts.data?.data[0]) setSelectedId(accounts.data.data[0].id)
  }, [accounts.data, selectedId])

  const summary = useQuery({
    queryKey: queryKeys.economicCapitalSummary(selectedId ?? 'none'),
    queryFn: ({ signal }) => getEconomicCapitalSummary(selectedId!, signal),
    enabled: Boolean(selectedId),
  })
  const flows = useQuery({
    queryKey: queryKeys.economicCapitalFlows(selectedId ?? 'none'),
    queryFn: ({ signal }) => getEconomicCapitalFlows(selectedId!, signal),
    enabled: Boolean(selectedId),
  })

  return (
    <section className="economic-workspace panel" aria-labelledby="economic-accounts-title">
      <div className="panel-header">
        <div>
          <p className="eyebrow">Schema 64–65 projections</p>
          <h2 id="economic-accounts-title">Economic accounts</h2>
        </div>
        <span className="read-only-chip"><LockKeyhole size={13} /> Read only</span>
      </div>
      {accounts.isLoading ? <LoadingState label="Loading economic accounts…" /> : null}
      {accounts.isError ? <QueryError error={accounts.error} retry={() => void accounts.refetch()} /> : null}
      {accounts.data?.data.length === 0 ? <EmptyState title="No economic accounts" message="The read model is available, but no scored, stress, or shadow accounts have been bootstrapped." /> : null}
      {accounts.data?.data.length ? (
        <div className="economic-layout">
          <AccountRail accounts={accounts.data.data} selectedId={selectedId} onSelect={setSelectedId} />
          <div className="account-detail">
            {selected ? <AccountIdentity account={selected} /> : null}
            {summary.isLoading ? <LoadingState label="Loading exact capital summary…" /> : null}
            {summary.isError ? <QueryError error={summary.error} retry={() => void summary.refetch()} /> : null}
            {summary.data ? (
              <div className="capital-metrics" aria-label="capital summary">
                <div><span>Starting capital</span><strong>{formatExactMoney(summary.data.starting_capital, summary.data.currency)}</strong></div>
                <div><span>Deposits</span><strong className="success">{formatExactMoney(summary.data.deposits, summary.data.currency)}</strong></div>
                <div><span>Withdrawals</span><strong className="warning">{formatExactMoney(summary.data.withdrawals, summary.data.currency)}</strong></div>
                <div><span>Net capital</span><strong>{formatExactMoney(summary.data.net_capital, summary.data.currency)}</strong></div>
              </div>
            ) : null}
            <div className="flow-section">
              <div className="subsection-heading">
                <div><p className="eyebrow">Append-only history</p><h3>Capital flows</h3></div>
                {summary.data ? <span>{summary.data.flow_count} recorded</span> : null}
              </div>
              {flows.isLoading ? <LoadingState label="Loading capital flows…" /> : null}
              {flows.isError ? <QueryError error={flows.error} retry={() => void flows.refetch()} /> : null}
              {flows.data?.data.length === 0 ? <p className="muted">No capital flows recorded.</p> : null}
              {flows.data?.data.length ? (
                <div className="flow-list">
                  {flows.data.data.map((flow) => (
                    <article key={flow.id}>
                      <span className={`flow-icon ${flow.type}`}>{flow.type === 'withdrawal' ? <ArrowUpRight /> : <ArrowDownLeft />}</span>
                      <div><strong>{flow.type}</strong><span>{flow.source} · {new Date(flow.effective_at).toLocaleString()}</span></div>
                      <strong className={flow.type === 'withdrawal' ? 'warning' : 'success'}>{flow.type === 'withdrawal' ? '−' : '+'}{formatExactMoney(flow.amount, flow.currency)}</strong>
                    </article>
                  ))}
                </div>
              ) : null}
            </div>
          </div>
        </div>
      ) : null}
    </section>
  )
}

function AccountIdentity({ account }: { account: EconomicAccount }) {
  return (
    <div className="account-identity">
      <div>
        <p className="eyebrow">{account.environment.replaceAll('_', ' ')}</p>
        <h3>{account.name}</h3>
        <p>{account.evidence_class.replaceAll('_', ' ')} evidence · {account.margin_profile.replaceAll('_', ' ')} margin</p>
      </div>
      <dl>
        <div><dt>Namespace</dt><dd>{account.storage_namespace}</dd></div>
        <div><dt>Account ID</dt><dd title={account.id}>{shortId(account.id)}</dd></div>
        <div><dt>Buying power</dt><dd>{account.buying_power_multiplier}×</dd></div>
        <div><dt>Created by</dt><dd>{account.created_by}</dd></div>
      </dl>
    </div>
  )
}

function EvidenceInspector() {
  const [assessmentInput, setAssessmentInput] = useState('')
  const [assessmentId, setAssessmentId] = useState('')
  const [ledgerInput, setLedgerInput] = useState('')
  const [ledgerId, setLedgerId] = useState('')
  const assessmentValid = assessmentInput === '' || UUID_PATTERN.test(assessmentInput.trim())
  const ledgerValid = ledgerInput === '' || UUID_PATTERN.test(ledgerInput.trim())

  const assessment = useQuery({
    queryKey: queryKeys.milestoneAssessment(assessmentId || 'none'),
    queryFn: ({ signal }) => getMilestoneAssessment(assessmentId, signal),
    enabled: Boolean(assessmentId),
    retry: false,
  })
  const ledger = useQuery({
    queryKey: queryKeys.economicLedgerTransaction(ledgerId || 'none'),
    queryFn: ({ signal }) => getEconomicLedgerTransaction(ledgerId, signal),
    enabled: Boolean(ledgerId),
    retry: false,
  })

  return (
    <div className="inspector-grid">
      <section className="panel inspector-panel" aria-labelledby="assessment-inspector-title">
        <div className="panel-header"><div><p className="eyebrow">Schema 103</p><h2 id="assessment-inspector-title">Milestone evidence</h2></div><FileSearch /></div>
        <form onSubmit={(event) => { event.preventDefault(); if (UUID_PATTERN.test(assessmentInput.trim())) setAssessmentId(assessmentInput.trim()) }}>
          <label htmlFor="assessment-id">Assessment UUID</label>
          <div className="lookup-control"><input id="assessment-id" value={assessmentInput} onChange={(event) => setAssessmentInput(event.target.value)} placeholder="xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx" aria-invalid={!assessmentValid} /><button type="submit" disabled={!UUID_PATTERN.test(assessmentInput.trim())}><Search size={14} /> Inspect</button></div>
          {!assessmentValid ? <span className="field-error">Enter a valid UUID.</span> : null}
        </form>
        {!assessmentId ? <div className="inspector-placeholder"><Fingerprint /><p>Paste a persisted assessment ID to reconstruct its canonical outcome, blockers, parent chain, and digest.</p></div> : null}
        {assessment.isLoading ? <LoadingState label="Reconstructing milestone assessment…" /> : null}
        {assessment.isError ? <QueryError error={assessment.error} retry={() => void assessment.refetch()} /> : null}
        {assessment.data ? (
          <div className="assessment-result">
            <div className="result-verdict"><span className={`status-pill ${statusClass(assessment.data.outcome)}`}>{assessment.data.outcome}</span><strong>{assessment.data.campaign}</strong></div>
            <dl className="compact-evidence"><div><dt>Assessment</dt><dd>{assessment.data.id}</dd></div><div><dt>SHA-256</dt><dd>{assessment.data.sha256}</dd></div><div><dt>Parents</dt><dd>{assessment.data.parents.length}</dd></div></dl>
            {assessment.data.blockers.length ? <div className="blocker-list"><strong>Blocking evidence</strong><ul>{assessment.data.blockers.map((blocker) => <li key={blocker}>{blocker}</li>)}</ul></div> : <p className="qualified-line"><Check /> No blockers recorded by the deterministic assessor.</p>}
            <details><summary>Canonical assessment JSON</summary><pre>{JSON.stringify(assessment.data.canonical, null, 2)}</pre></details>
          </div>
        ) : null}
      </section>

      <section className="panel inspector-panel" aria-labelledby="ledger-inspector-title">
        <div className="panel-header"><div><p className="eyebrow">Balanced transaction</p><h2 id="ledger-inspector-title">Ledger trace</h2></div><BookOpenCheck /></div>
        <form onSubmit={(event) => { event.preventDefault(); if (UUID_PATTERN.test(ledgerInput.trim())) setLedgerId(ledgerInput.trim()) }}>
          <label htmlFor="ledger-id">Transaction UUID</label>
          <div className="lookup-control"><input id="ledger-id" value={ledgerInput} onChange={(event) => setLedgerInput(event.target.value)} placeholder="xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx" aria-invalid={!ledgerValid} /><button type="submit" disabled={!UUID_PATTERN.test(ledgerInput.trim())}><Search size={14} /> Trace</button></div>
          {!ledgerValid ? <span className="field-error">Enter a valid UUID.</span> : null}
        </form>
        {!ledgerId ? <div className="inspector-placeholder"><Database /><p>Trace an immutable ledger transaction to its origin, observation time, and signed posting lines.</p></div> : null}
        {ledger.isLoading ? <LoadingState label="Loading ledger transaction…" /> : null}
        {ledger.isError ? <QueryError error={ledger.error} retry={() => void ledger.refetch()} /> : null}
        {ledger.data ? (
          <div className="ledger-result">
            <div className="result-verdict"><span className="status-pill success">balanced record</span><strong>{ledger.data.event_type}</strong></div>
            <dl className="compact-evidence"><div><dt>Origin</dt><dd>{ledger.data.origin_type} / {ledger.data.origin_id}</dd></div><div><dt>Effective</dt><dd>{new Date(ledger.data.effective_at).toLocaleString()}</dd></div><div><dt>Observed</dt><dd>{new Date(ledger.data.observed_at).toLocaleString()}</dd></div></dl>
            <div className="posting-list">{ledger.data.postings.map((posting) => <div key={posting.id}><span>{posting.ledger_account}<small>{posting.unit_kind} · {posting.unit}</small></span><strong className={posting.amount.startsWith('-') ? 'warning' : 'success'}>{posting.amount}</strong></div>)}</div>
          </div>
        ) : null}
      </section>
    </div>
  )
}

export function OverhaulPage() {
  const stageCounts = useMemo(() => ({ available: adoptionStages.filter((stage) => stage.state === 'available').length, gated: adoptionStages.filter((stage) => stage.state !== 'available').length }), [])

  return (
    <div className="overhaul-page">
      <PageHeader
        eyebrow="Total-overhaul runtime adoption"
        title="Capital & evidence"
        description="Inspect the new exact economic boundary and deterministic evidence chain without creating accounts, moving capital, enabling schedulers, or implying live-trading authority."
        actions={<Breadcrumbs items={[{ label: 'System' }, { label: 'Capital & evidence' }]} />}
      />

      <section className="overhaul-hero" aria-labelledby="adoption-title">
        <div className="overhaul-hero-copy">
          <span className="hero-kicker"><GitBranch /> Runtime adoption R1</span>
          <h2 id="adoption-title">The new foundations are inspectable. Later stages remain fenced.</h2>
          <p>The application now exposes read-only accounts, exact capital history, balanced ledger transactions, release readiness, and persisted milestone assessments. The UI keeps every later mechanism visibly gated until it is wired and proven.</p>
          <div className="hero-facts"><span><CircleDollarSign /> Exact decimal economics</span><span><Fingerprint /> SHA-256 evidence</span><span><LockKeyhole /> No mutation authority</span></div>
        </div>
        <div className="adoption-score">
          <div><strong>{stageCounts.available}</strong><span>runtime stages exposed</span></div>
          <div><strong>{stageCounts.gated}</strong><span>stages still gated</span></div>
        </div>
      </section>

      <section className="adoption-rail" aria-label="overhaul runtime adoption stages">
        {adoptionStages.map((stage) => <article key={stage.id} className={stage.state}><span>{stage.id}</span><div><strong>{stage.title}</strong><p>{stage.detail}</p></div><span className={`status-pill ${statusClass(stage.state)}`}>{stage.state}</span></article>)}
      </section>

      <ReadinessPanel />
      <EconomicWorkspace />
      <EvidenceInspector />

      <section className="authority-boundary" aria-labelledby="authority-title">
        <ShieldCheck />
        <div><p className="eyebrow">Authority boundary</p><h2 id="authority-title">Inspection is not activation</h2><p>This workspace cannot bootstrap accounts, write capital flows, start evidence campaigns, route orders, enable live trading, or change promotion decisions. Those actions remain local/runbook-only or unimplemented and require separate authority.</p></div>
      </section>
    </div>
  )
}
