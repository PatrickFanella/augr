import { useQuery } from '@tanstack/react-query'
import { useEffect, useMemo, useState, type KeyboardEvent } from 'react'
import { useParams, useSearchParams } from 'react-router-dom'

import { EventTimeline } from '@/features/events/EventTimeline'
import { getRun, getRunDecisions, getRunSnapshot } from '@/shared/api/endpoints'
import { isApiClientError } from '@/shared/api/errors'
import { Breadcrumbs, CopyButton, EntityId, EntityLink } from '@/shared/components/EntityLinks'
import { EmptyState, ErrorState, LastUpdated, LoadingState, StaleBanner } from '@/shared/components/QueryStates'
import { queryKeys } from '@/shared/query/keys'
import type { AgentDecision, PipelineRun, RunSnapshot } from '@/shared/types/domain'
import { useRealtime } from '@/shared/websocket/RealtimeProvider'

const staleEventTypes = new Set(['agent_decision', 'debate_round', 'signal', 'error', 'pipeline_health'])
const decisionsPageSize = 10
type RunDetailTab = 'overview' | 'decisions' | 'snapshot' | 'timeline'

function titleCase(value?: string) {
  if (!value) return 'Unknown'
  return value.replace(/_/g, ' ')
}

function RunStatusPill({ value }: { value: string }) {
  const known = ['running', 'completed', 'failed', 'cancelled'].includes(value)
  return <span className={`status-pill ${known ? value : 'unknown'}`}>{known ? value : `Unknown: ${value}`}</span>
}

function SignalValue({ value }: { value?: string }) {
  if (!value) return <span className="muted">No signal</span>
  return <span>{titleCase(value)}</span>
}

const secretKeyPattern = /(api[_-]?key|secret|password|token|authorization|credential|private[_-]?key)/i

function redactSnapshotValue(value: unknown): unknown {
  if (Array.isArray(value)) return value.map(redactSnapshotValue)
  if (value && typeof value === 'object') {
    return Object.fromEntries(Object.entries(value as Record<string, unknown>).map(([key, item]) => [key, secretKeyPattern.test(key) ? '[REDACTED]' : redactSnapshotValue(item)]))
  }
  return value
}

function snapshotEntries(snapshot?: RunSnapshot) {
  return Object.entries(snapshot ?? {}).sort(([a], [b]) => a.localeCompare(b))
}

function JsonViewer({ value, title, copyLabel }: { value: unknown; title: string; copyLabel: string }) {
  const json = useMemo(() => JSON.stringify(value ?? {}, null, 2), [value])
  return (
    <div className="json-viewer">
      <div className="panel-header">
        <h2>{title}</h2>
        <CopyButton value={json} label={copyLabel} />
      </div>
      <pre tabIndex={0}>{json}</pre>
    </div>
  )
}

function DetailTabs({ activeTab, onChange }: { activeTab: RunDetailTab; onChange: (tab: RunDetailTab) => void }) {
  const tabs: RunDetailTab[] = ['overview', 'decisions', 'snapshot', 'timeline']
  function onKeyDown(event: KeyboardEvent<HTMLDivElement>) {
    if (event.key !== 'ArrowRight' && event.key !== 'ArrowLeft') return
    event.preventDefault()
    const currentIndex = tabs.indexOf(activeTab)
    const direction = event.key === 'ArrowRight' ? 1 : -1
    onChange(tabs[(currentIndex + direction + tabs.length) % tabs.length])
  }
  return (
    <div className="tabs" role="tablist" aria-label="Run detail tabs" onKeyDown={onKeyDown}>
      {tabs.map((tab) => (
        <button key={tab} type="button" role="tab" aria-selected={activeTab === tab} className={activeTab === tab ? 'active' : ''} onClick={() => onChange(tab)}>
          {tab === 'overview' ? 'Overview' : tab === 'decisions' ? 'Decisions' : tab === 'snapshot' ? 'Snapshot' : 'Timeline'}
        </button>
      ))}
    </div>
  )
}

function RunSummary({ run }: { run: PipelineRun }) {
  const durationMs = run.completed_at ? new Date(run.completed_at).getTime() - new Date(run.started_at).getTime() : undefined
  return (
    <dl className="kv-grid">
      <dt>Run ID</dt><dd><EntityId kind="run" id={run.id} /></dd>
      <dt>Strategy</dt><dd><EntityLink kind="strategy" id={run.strategy_id} /></dd>
      <dt>Ticker</dt><dd>{run.ticker}</dd>
      <dt>Status</dt><dd><RunStatusPill value={run.status} /></dd>
      <dt>Signal</dt><dd><SignalValue value={run.signal} /></dd>
      <dt>Trade date</dt><dd>{new Date(run.trade_date).toLocaleString()}</dd>
      <dt>Started</dt><dd>{new Date(run.started_at).toLocaleString()}</dd>
      <dt>Completed</dt><dd>{run.completed_at ? new Date(run.completed_at).toLocaleString() : 'Not completed'}</dd>
      <dt>Duration</dt><dd>{durationMs !== undefined && durationMs >= 0 ? `${Math.round(durationMs / 1000)} seconds` : 'Still running or unavailable'}</dd>
      {run.error_message ? <><dt>Error</dt><dd className="error-text">{run.error_message}</dd></> : null}
    </dl>
  )
}

function DecisionCard({ decision }: { decision: AgentDecision }) {
  return (
    <article className="panel decision-card">
      <div className="panel-header">
        <div>
          <h3>{titleCase(decision.agent_role)} · {titleCase(decision.phase)}</h3>
          <p className="muted">{new Date(decision.created_at).toLocaleString()} {decision.round_number !== undefined ? `· round ${decision.round_number}` : ''}</p>
        </div>
        <EntityId kind="decision" id={decision.id} />
      </div>
      {decision.input_summary ? <p><strong>Input:</strong> {decision.input_summary}</p> : null}
      <p>{decision.output_text}</p>
      <dl className="kv-grid compact-kv">
        <dt>Provider</dt><dd>{decision.llm_provider || 'Unknown'}</dd>
        <dt>Model</dt><dd>{decision.llm_model || 'Unknown'}</dd>
        <dt>Tokens</dt><dd>{(decision.prompt_tokens ?? 0) + (decision.completion_tokens ?? 0)}</dd>
        <dt>Latency</dt><dd>{decision.latency_ms !== undefined ? `${decision.latency_ms} ms` : 'Unknown'}</dd>
        <dt>Cost</dt><dd>{decision.cost_usd !== undefined ? `$${decision.cost_usd.toFixed(4)}` : 'Unknown'}</dd>
      </dl>
      {decision.output_structured ? <JsonViewer value={decision.output_structured} title="Structured output" copyLabel="Copy structured decision output" /> : null}
      {decision.prompt_text ? <JsonViewer value={{ prompt_text: decision.prompt_text }} title="Prompt text" copyLabel="Copy decision prompt text" /> : null}
    </article>
  )
}

function DecisionsPanel({ runId, realtimeStale }: { runId: string; realtimeStale: boolean }) {
  const [searchParams, setSearchParams] = useSearchParams()
  const decisionOffset = Number(searchParams.get('decision_offset') ?? '0')
  const filters = useMemo(() => ({
    include_prompt: searchParams.get('include_prompt') === 'true' || undefined,
    agent_role: searchParams.get('agent_role') || undefined,
    phase: searchParams.get('phase') || undefined,
    limit: decisionsPageSize,
    offset: Number.isFinite(decisionOffset) && decisionOffset > 0 ? decisionOffset : 0,
  }), [decisionOffset, searchParams])
  const query = useQuery({
    queryKey: queryKeys.runDecisions(runId, filters),
    queryFn: ({ signal }) => getRunDecisions(runId, filters, signal),
  })
  const decisions = query.data?.data ?? []
  const offset = filters.offset ?? 0
  const total = query.data?.total
  const hasNext = total === undefined ? decisions.length === decisionsPageSize : offset + decisionsPageSize < total

  function updateFilters(updates: Record<string, string>) {
    const next = new URLSearchParams(searchParams)
    next.set('tab', 'decisions')
    for (const [key, value] of Object.entries(updates)) {
      if (value) next.set(key, value)
      else next.delete(key)
    }
    next.delete('decision_offset')
    setSearchParams(next)
  }

  function setOffset(nextOffset: number) {
    const next = new URLSearchParams(searchParams)
    next.set('tab', 'decisions')
    if (nextOffset > 0) next.set('decision_offset', String(nextOffset))
    else next.delete('decision_offset')
    setSearchParams(next)
  }

  return (
    <div className="reports-stack" role="tabpanel" aria-label="Run decisions">
      <StaleBanner show={realtimeStale} message="Realtime decision activity was received. Refetch before using this evidence operationally." />
      <section className="panel" aria-labelledby="run-decisions-heading">
        <div className="panel-header">
          <div>
            <h2 id="run-decisions-heading">Agent decisions</h2>
            <p className="muted">Read-only decision and debate evidence for this run.</p>
          </div>
          {query.data ? <LastUpdated date={query.dataUpdatedAt} /> : null}
        </div>
        <form className="filter-bar" aria-label="Decision filters" onSubmit={(event) => event.preventDefault()}>
          <label>Agent role<input value={searchParams.get('agent_role') ?? ''} onChange={(event) => updateFilters({ agent_role: event.target.value })} placeholder="analyst" /></label>
          <label>Phase<input value={searchParams.get('phase') ?? ''} onChange={(event) => updateFilters({ phase: event.target.value })} placeholder="signal_generation" /></label>
          <label>Prompt
            <select value={searchParams.get('include_prompt') ?? ''} onChange={(event) => updateFilters({ include_prompt: event.target.value })}>
              <option value="">Hide prompts</option>
              <option value="true">Include prompts</option>
            </select>
          </label>
          <button type="button" onClick={() => updateFilters({ agent_role: '', phase: '', include_prompt: '' })}>Clear filters</button>
        </form>
        {query.isLoading ? <LoadingState label="Loading run decisions…" /> : null}
        {query.error ? <ErrorState error={query.error} onRetry={() => void query.refetch()} /> : null}
        {query.data && decisions.length === 0 ? <EmptyState title="No decisions found" message="This run has no matching agent decisions yet." /> : null}
        {decisions.length > 0 ? (
          <>
            <div className="decision-list">
              {decisions.map((decision) => <DecisionCard key={decision.id} decision={decision} />)}
            </div>
            <nav className="pagination-controls" aria-label="Decision pagination">
              <button type="button" className="secondary-button" disabled={offset === 0} onClick={() => setOffset(Math.max(0, offset - decisionsPageSize))}>Previous</button>
              <span className="muted">Showing {offset + 1}–{offset + decisions.length} {total === undefined ? 'total unavailable' : `of ${total}`}</span>
              <button type="button" className="secondary-button" disabled={!hasNext} onClick={() => setOffset(offset + decisionsPageSize)}>Next</button>
            </nav>
          </>
        ) : null}
      </section>
    </div>
  )
}

function SnapshotPanel({ run, realtimeStale }: { run: PipelineRun; realtimeStale: boolean }) {
  const query = useQuery({
    queryKey: queryKeys.runSnapshot(run.id),
    queryFn: ({ signal }) => getRunSnapshot(run.id, signal),
  })
  const entries = snapshotEntries(query.data)
  const showRunningWarning = run.status === 'running'

  return (
    <div className="reports-stack" role="tabpanel" aria-label="Run snapshot">
      <StaleBanner show={realtimeStale || showRunningWarning} message="Snapshot data is read-only evidence and may be stale for running runs. Obvious secret keys are redacted in the UI." />
      <section className="panel" aria-labelledby="run-snapshot-heading">
        <div className="panel-header">
          <div>
            <h2 id="run-snapshot-heading">Run snapshot</h2>
            <p className="muted">Captured market, config, and state payloads grouped by data type.</p>
          </div>
          {query.data ? <LastUpdated date={query.dataUpdatedAt} /> : null}
        </div>
        {query.isLoading ? <LoadingState label="Loading run snapshot…" /> : null}
        {query.error ? <ErrorState error={query.error} onRetry={() => void query.refetch()} /> : null}
        {query.data && entries.length === 0 ? <EmptyState title="Snapshot not recorded" message="No captured snapshot payloads are available for this run." /> : null}
        {entries.length > 0 ? (
          <div className="snapshot-list">
            {entries.map(([dataType, payload]) => (
              <section key={dataType} className="panel nested-panel" aria-labelledby={`snapshot-${dataType.replace(/[^a-z0-9_-]/gi, '-')}`}>
                <h3 id={`snapshot-${dataType.replace(/[^a-z0-9_-]/gi, '-')}`}>{titleCase(dataType)}</h3>
                <JsonViewer value={redactSnapshotValue(payload)} title={`${dataType} JSON`} copyLabel={`Copy ${dataType} snapshot JSON`} />
              </section>
            ))}
          </div>
        ) : null}
      </section>
    </div>
  )
}

export function RunDetailPage() {
  const { id } = useParams()
  const [searchParams, setSearchParams] = useSearchParams()
  const runId = id ?? ''
  const tabParam = searchParams.get('tab')
  const activeTab: RunDetailTab = tabParam === 'decisions' ? 'decisions' : tabParam === 'snapshot' ? 'snapshot' : tabParam === 'timeline' ? 'timeline' : 'overview'
  const realtime = useRealtime()
  const [realtimeStale, setRealtimeStale] = useState(false)
  const runQuery = useQuery({
    queryKey: queryKeys.runDetail(runId),
    queryFn: ({ signal }) => getRun(runId, signal),
    enabled: Boolean(runId),
  })

  useEffect(() => {
    if (!runId || realtime.events.length === 0) return
    const latest = realtime.events[0]
    if (staleEventTypes.has(latest.type) && (latest.run_id === runId || latest.strategy_id === runQuery.data?.strategy_id)) {
      setRealtimeStale(true)
    }
  }, [realtime.events, runId, runQuery.data?.strategy_id])

  const run = runQuery.data
  const notFound = isApiClientError(runQuery.error) && runQuery.error.kind === 'not_found'
  const showStale = Boolean(run && (runQuery.isStale || realtimeStale || realtime.status === 'disconnected' || realtime.status === 'degraded'))

  function setTab(tab: RunDetailTab) {
    const next = new URLSearchParams(searchParams)
    if (tab === 'overview') next.delete('tab')
    else next.set('tab', tab)
    setSearchParams(next)
  }

  return (
    <div className="detail-stack">
      <Breadcrumbs items={[{ label: 'Cockpit', to: '/cockpit' }, { label: 'Runs', to: '/runs' }, { label: run?.ticker ?? 'Run detail' }]} />

      <section className="panel hero-panel">
        <p className="eyebrow">Run detail</p>
        {runQuery.isLoading ? <LoadingState label="Loading run detail…" /> : null}
        {notFound ? <div role="alert" className="error-box">Run not found. Return to the runs list and verify the link.</div> : null}
        {runQuery.error && !notFound ? <ErrorState error={runQuery.error} onRetry={() => void runQuery.refetch()} /> : null}
        {run ? (
          <>
            <div className="panel-header">
              <div>
                <h1>{run.ticker} run</h1>
                <p className="muted"><code>{run.id}</code></p>
              </div>
              <div className="header-cluster">
                <EntityLink kind="strategy" id={run.strategy_id} label="Open strategy" copy={false} />
                <RunStatusPill value={run.status} />
              </div>
            </div>
            <LastUpdated date={runQuery.dataUpdatedAt || run.started_at} />
            <StaleBanner show={showStale} message="Run detail is read-only and may be stale. Refresh before using this evidence for operational decisions." />
            {realtime.status === 'disconnected' || realtime.status === 'degraded' ? <p role="status" className="warning-box">WebSocket {realtime.status}; run detail may lag realtime changes.</p> : null}

            <DetailTabs activeTab={activeTab} onChange={setTab} />

            {activeTab === 'overview' ? (
              <div role="tabpanel" aria-label="Run overview">
                <div className="detail-grid">
                  <section className="panel" aria-labelledby="run-summary-heading">
                    <h2 id="run-summary-heading">Overview</h2>
                    <RunSummary run={run} />
                  </section>
                  <section className="panel" aria-labelledby="run-evidence-heading">
                    <h2 id="run-evidence-heading">Evidence links</h2>
                    <dl className="kv-grid">
                      <dt>Strategy</dt><dd><EntityLink kind="strategy" id={run.strategy_id} label="Open strategy detail" /></dd>
                      <dt>Decisions</dt><dd><button type="button" className="secondary-button" onClick={() => setTab('decisions')}>Open decisions tab</button></dd>
                      <dt>Snapshot</dt><dd><button type="button" className="secondary-button" onClick={() => setTab('snapshot')}>Open snapshot tab</button></dd>
                      <dt>Timeline</dt><dd><button type="button" className="secondary-button" onClick={() => setTab('timeline')}>Open timeline tab</button></dd>
                    </dl>
                  </section>
                </div>
                <div className="reports-grid">
                  <JsonViewer value={run.config_snapshot ?? {}} title="Config snapshot" copyLabel="Copy run config snapshot JSON" />
                  <JsonViewer value={run.phase_timings ?? {}} title="Phase timings" copyLabel="Copy run phase timings JSON" />
                </div>
              </div>
            ) : activeTab === 'decisions' ? (
              <DecisionsPanel runId={run.id} realtimeStale={realtimeStale} />
            ) : activeTab === 'snapshot' ? (
              <SnapshotPanel run={run} realtimeStale={realtimeStale} />
            ) : (
              <div role="tabpanel" aria-label="Run timeline"><EventTimeline fixedRunId={run.id} fixedStrategyId={run.strategy_id} /></div>
            )}
          </>
        ) : null}
      </section>
    </div>
  )
}
