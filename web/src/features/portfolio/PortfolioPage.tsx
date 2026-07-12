import { useQuery } from '@tanstack/react-query'
import { useEffect, useMemo, useState } from 'react'
import { useSearchParams } from 'react-router-dom'
import { RefreshCw } from 'lucide-react'

import { Alert } from '@/components/ui/alert'
import { PageHeader } from '@/components/ui/page-header'
import { getAllocationDecisions, getAllocatorDiagnostics, getAllocatorOpportunities, getAllocatorSummary, getOpenPortfolioPositions, getPortfolioSummary } from '@/shared/api/endpoints'
import { Breadcrumbs, EntityId, EntityLink } from '@/shared/components/EntityLinks'
import { EmptyState, ErrorState, LastUpdated, LoadingState, StaleBanner } from '@/shared/components/QueryStates'
import { queryKeys } from '@/shared/query/keys'
import { marketTypes, type AllocationDecision, type AllocatorDiagnostics, type AllocatorOpportunity, type Position } from '@/shared/types/domain'
import { useRealtime } from '@/shared/websocket/RealtimeProvider'

const pageSize = 20

function money(value?: number) {
  if (value === undefined) return 'Unknown'
  return new Intl.NumberFormat(undefined, { style: 'currency', currency: 'USD', maximumFractionDigits: 2 }).format(value)
}

function numberValue(value?: number) {
  if (value === undefined) return 'Unknown'
  return new Intl.NumberFormat(undefined, { maximumFractionDigits: 4 }).format(value)
}

function percent(value?: number) {
  if (value === undefined) return 'Unknown'
  return `${new Intl.NumberFormat(undefined, { maximumFractionDigits: 2 }).format(value)}%`
}

function pnlClass(value?: number) {
  if (value === undefined) return 'unknown'
  if (value > 0) return 'active'
  if (value < 0) return 'unknown'
  return 'unknown'
}

function compactMap(entries: Record<string, number>, emptyLabel = 'No counts reported.') {
  const pairs = Object.entries(entries).sort(([left], [right]) => left.localeCompare(right))
  if (pairs.length === 0) return <p className="muted">{emptyLabel}</p>
  return (
    <dl className="compact-kv">
      {pairs.map(([key, value]) => <div key={key}><dt>{key || 'unknown'}</dt><dd><span className={`status-pill ${value > 0 ? 'active' : 'unknown'}`}>{value}</span></dd></div>)}
    </dl>
  )
}

function StatusPill({ value }: { value: string }) {
  const normalized = value.replaceAll('_', ' ')
  const known = ['queued', 'selected', 'rejected', 'expired', 'executed', 'shadow', 'paper', 'select', 'reject', 'hold', 'buy', 'sell'].includes(value)
  return <span className={`status-pill ${known ? 'active' : 'unknown'}`}>{known ? normalized : `Unknown: ${normalized}`}</span>
}

function SidePill({ value }: { value: string }) {
  const known = ['long', 'short'].includes(value)
  return <span className={`status-pill ${known ? 'active' : 'unknown'}`}>{known ? value : `Unknown: ${value}`}</span>
}

function PositionRows({ positions }: { positions: Position[] }) {
  return (
    <>
      <div className="table-wrap">
        <table aria-label="Open positions">
          <thead><tr><th>Position</th><th>Ticker</th><th>Side</th><th>Quantity</th><th>Average entry</th><th>Current</th><th>Unrealized P/L</th><th>Realized P/L</th><th>Opened</th></tr></thead>
          <tbody>
            {positions.map((position) => (
              <tr key={position.id}>
                <td><EntityLink kind="position" id={position.id} />{position.strategy_id ? <><br /><EntityLink kind="strategy" id={position.strategy_id} copy={false} /></> : null}</td>
                <td>{position.ticker}</td>
                <td><SidePill value={position.side} /></td>
                <td>{numberValue(position.quantity)}</td>
                <td>{money(position.avg_entry)}</td>
                <td>{money(position.current_price)}</td>
                <td>{money(position.unrealized_pnl)}</td>
                <td>{money(position.realized_pnl)}</td>
                <td>{new Date(position.opened_at).toLocaleString()}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      <div className="card-list" aria-label="Open position cards">
        {positions.map((position) => (
          <article className="strategy-card" key={position.id}>
            <h3>{position.ticker}</h3>
            <p><SidePill value={position.side} /> · {numberValue(position.quantity)} units</p>
            <p>Unrealized {money(position.unrealized_pnl)} · Realized {money(position.realized_pnl)}</p>
            {position.strategy_id ? <EntityLink kind="strategy" id={position.strategy_id} label="Open strategy" copy={false} /> : null}
          </article>
        ))}
      </div>
    </>
  )
}

function DiagnosticsGrid({ diagnostics }: { diagnostics: AllocatorDiagnostics }) {
  const healthyValue = (value: number | undefined, formatter = percent) => (
    <span className={`status-pill ${value !== undefined && value > 0 ? 'active' : 'unknown'}`}>{formatter(value)}</span>
  )

  return (
    <div className="detail-stack">
      <section className="metrics-grid" aria-label="Allocator diagnostic summary">
        <article className="panel"><p className="eyebrow">Buying power</p><strong>{healthyValue(diagnostics.buying_power_utilization_pct)}</strong></article>
        <article className="panel"><p className="eyebrow">Exposure</p><strong>{healthyValue(diagnostics.gross_exposure_pct)}</strong></article>
        <article className="panel"><p className="eyebrow">Utilization gap</p><strong>{healthyValue(diagnostics.utilization_gap_pct)}</strong></article>
      </section>
      <div className="detail-grid">
        <article className="panel nested-panel"><h3>Exposure</h3><dl className="detail-grid"><div><dt>Buying power utilization</dt><dd><span className={`status-pill ${diagnostics.buying_power_utilization_pct !== undefined && diagnostics.buying_power_utilization_pct <= 80 ? 'active' : 'unknown'}`}>{percent(diagnostics.buying_power_utilization_pct)}</span></dd></div><div><dt>Gross exposure</dt><dd><span className={`status-pill ${diagnostics.gross_exposure_pct !== undefined && diagnostics.gross_exposure_pct <= 100 ? 'active' : 'unknown'}`}>{percent(diagnostics.gross_exposure_pct)}</span></dd></div><div><dt>Target exposure</dt><dd><span className="status-pill active">{percent(diagnostics.target_gross_exposure_pct)}</span></dd></div><div><dt>Utilization gap</dt><dd><span className={`status-pill ${diagnostics.utilization_gap_pct !== undefined && diagnostics.utilization_gap_pct <= 10 ? 'active' : 'unknown'}`}>{percent(diagnostics.utilization_gap_pct)}</span></dd></div></dl></article>
        <article className="panel nested-panel"><h3>Run signals</h3>{compactMap(diagnostics.run_counts_by_signal)}</article>
        <article className="panel nested-panel"><h3>Run statuses</h3>{compactMap(diagnostics.run_counts_by_status)}</article>
        <article className="panel nested-panel"><h3>Decision statuses</h3>{compactMap(diagnostics.decision_counts_by_status)}</article>
        <article className="panel nested-panel"><h3>No-action reasons</h3>{compactMap(diagnostics.no_action_reasons)}</article>
        <article className="panel nested-panel"><h3>Active strategies by market</h3>{compactMap(diagnostics.active_strategies_by_market)}</article>
        <article className="panel nested-panel"><h3>Open positions by market</h3>{compactMap(diagnostics.open_positions_by_market, 'No open positions.')}</article>
      </div>
    </div>
  )
}

function OpportunityRows({ opportunities }: { opportunities: AllocatorOpportunity[] }) {
  return (
    <div className="table-wrap">
      <table aria-label="Allocator opportunities">
        <thead><tr><th>Opportunity</th><th>Ticker</th><th>Market</th><th>Status</th><th>Signal</th><th>Edge</th><th>Selected notional</th><th>Reason</th></tr></thead>
        <tbody>{opportunities.map((opportunity) => (
          <tr key={opportunity.id}>
            <td><EntityId kind="opportunity" id={opportunity.id} label="Opportunity" /><br /><EntityLink kind="strategy" id={opportunity.strategy_id} copy={false} />{opportunity.pipeline_run_id ? <><br /><EntityLink kind="run" id={opportunity.pipeline_run_id} copy={false} /></> : null}</td>
            <td>{opportunity.ticker}</td>
            <td>{opportunity.market_type}</td>
            <td><StatusPill value={opportunity.status} /></td>
            <td>{opportunity.signal}</td>
            <td>{percent(opportunity.edge_pct)}</td>
            <td>{money(opportunity.selected_notional)}</td>
            <td>{opportunity.reason}</td>
          </tr>
        ))}</tbody>
      </table>
    </div>
  )
}

function DecisionRows({ decisions }: { decisions: AllocationDecision[] }) {
  return (
    <div className="table-wrap">
      <table aria-label="Allocator decisions">
        <thead><tr><th>Decision</th><th>Mode</th><th>Action</th><th>Score</th><th>Notional</th><th>Quantity</th><th>Reasons</th></tr></thead>
        <tbody>{decisions.map((decision) => (
          <tr key={decision.id}>
            <td><EntityId kind="decision" id={decision.id} />{decision.strategy_id ? <><br /><EntityLink kind="strategy" id={decision.strategy_id} copy={false} /></> : null}{decision.created_order_id ? <><br /><EntityLink kind="order" id={decision.created_order_id} label="Created order" copy={false} /></> : null}</td>
            <td><StatusPill value={decision.mode} /></td>
            <td><StatusPill value={decision.action} /></td>
            <td>{numberValue(decision.score)}</td>
            <td>{money(decision.notional_usd)}</td>
            <td>{numberValue(decision.quantity)}</td>
            <td>{decision.reasons.join(', ')}</td>
          </tr>
        ))}</tbody>
      </table>
    </div>
  )
}

function AllocatorPanel({ searchParams, setSearchParams }: { searchParams: URLSearchParams; setSearchParams: (next: URLSearchParams) => void }) {
  const opportunityOffset = Number(searchParams.get('opportunity_offset') ?? '0')
  const decisionOffset = Number(searchParams.get('decision_offset') ?? '0')
  const opportunityFilters = useMemo(() => ({
    status: searchParams.get('status') || undefined,
    market_type: searchParams.get('market_type') || undefined,
    ticker: searchParams.get('ticker') || undefined,
    strategy_id: searchParams.get('strategy_id') || undefined,
    limit: 10,
    offset: Number.isFinite(opportunityOffset) && opportunityOffset > 0 ? opportunityOffset : 0,
  }), [opportunityOffset, searchParams])
  const decisionFilters = useMemo(() => ({
    mode: searchParams.get('mode') || undefined,
    action: searchParams.get('action') || undefined,
    strategy_id: searchParams.get('strategy_id') || undefined,
    limit: 10,
    offset: Number.isFinite(decisionOffset) && decisionOffset > 0 ? decisionOffset : 0,
  }), [decisionOffset, searchParams])
  const diagnosticsQuery = useQuery({ queryKey: queryKeys.allocatorDiagnostics, queryFn: ({ signal }) => getAllocatorDiagnostics(signal) })
  const summaryQuery = useQuery({ queryKey: queryKeys.allocatorSummary, queryFn: ({ signal }) => getAllocatorSummary(signal) })
  const opportunitiesQuery = useQuery({ queryKey: queryKeys.allocatorOpportunities(opportunityFilters), queryFn: ({ signal }) => getAllocatorOpportunities(opportunityFilters, signal) })
  const decisionsQuery = useQuery({ queryKey: queryKeys.allocatorDecisions(decisionFilters), queryFn: ({ signal }) => getAllocationDecisions(decisionFilters, signal) })
  const opportunities = opportunitiesQuery.data?.data ?? []
  const decisions = decisionsQuery.data?.data ?? []
  const opportunityTotal = opportunitiesQuery.data?.total
  const decisionTotal = decisionsQuery.data?.total
  const currentOpportunityOffset = opportunityFilters.offset ?? 0
  const currentDecisionOffset = decisionFilters.offset ?? 0

  function updateAllocatorFilters(updates: Record<string, string>) {
    const next = new URLSearchParams(searchParams)
    next.set('tab', 'allocator')
    for (const [key, value] of Object.entries(updates)) {
      if (value) next.set(key, value)
      else next.delete(key)
    }
    next.delete('opportunity_offset')
    next.delete('decision_offset')
    setSearchParams(next)
  }

  function setAllocatorOffset(key: 'opportunity_offset' | 'decision_offset', value: number) {
    const next = new URLSearchParams(searchParams)
    next.set('tab', 'allocator')
    if (value > 0) next.set(key, String(value))
    else next.delete(key)
    setSearchParams(next)
  }

  return (
    <div className="detail-stack" aria-label="Allocator diagnostics">
      <section className="panel" aria-labelledby="allocator-heading">
        <div className="panel-header"><div><h2 id="allocator-heading">Allocator diagnostics</h2><p className="muted">Read-only allocator health, shadow opportunities, and allocation decisions. Rebalance actions are excluded.</p></div>{diagnosticsQuery.data ? <LastUpdated date={diagnosticsQuery.dataUpdatedAt} /> : null}</div>
        {diagnosticsQuery.isLoading ? <LoadingState label="Loading allocator diagnostics…" /> : null}
        {diagnosticsQuery.error ? <ErrorState error={diagnosticsQuery.error} onRetry={() => void diagnosticsQuery.refetch()} /> : null}
        {diagnosticsQuery.data ? <DiagnosticsGrid diagnostics={diagnosticsQuery.data} /> : null}
        {diagnosticsQuery.data?.warnings.length ? <Alert variant="warning">Warnings: {diagnosticsQuery.data.warnings.join(', ')}</Alert> : null}
      </section>

      <section className="metrics-grid" aria-label="Allocator summary">
        <article className="panel"><p className="eyebrow">Queued opportunities</p><strong>{summaryQuery.data?.opportunity_counts_by_status.queued ?? '—'}</strong></article>
        <article className="panel"><p className="eyebrow">Selected opportunities</p><strong>{summaryQuery.data?.opportunity_counts_by_status.selected ?? '—'}</strong></article>
        <article className="panel"><p className="eyebrow">Recent decisions</p><strong>{summaryQuery.data?.recent_decisions.length ?? '—'}</strong></article>
      </section>
      {summaryQuery.error ? <ErrorState error={summaryQuery.error} onRetry={() => void summaryQuery.refetch()} /> : null}

      <section className="panel" aria-labelledby="opportunities-heading">
        <div className="panel-header"><div><h2 id="opportunities-heading">Allocator opportunities</h2><p className="muted">Backend-supported filters: ticker, market type, status, strategy.</p></div>{opportunitiesQuery.data ? <LastUpdated date={opportunitiesQuery.dataUpdatedAt} /> : null}</div>
        <form className="filter-bar" aria-label="Allocator opportunity filters" onSubmit={(event) => event.preventDefault()}>
          <label>Ticker<input value={searchParams.get('ticker') ?? ''} onChange={(event) => updateAllocatorFilters({ ticker: event.target.value.toUpperCase() })} placeholder="AUGR" /></label>
          <label>Status<select value={searchParams.get('status') ?? ''} onChange={(event) => updateAllocatorFilters({ status: event.target.value })}><option value="">All statuses</option>{['queued', 'selected', 'rejected', 'expired', 'executed'].map((status) => <option key={status} value={status}>{status}</option>)}</select></label>
          <label>Market type<select value={searchParams.get('market_type') ?? ''} onChange={(event) => updateAllocatorFilters({ market_type: event.target.value })}><option value="">All markets</option>{marketTypes.map((market) => <option key={market} value={market}>{market}</option>)}</select></label>
          <button type="button" onClick={() => updateAllocatorFilters({ ticker: '', status: '', market_type: '' })}>Clear filters</button>
        </form>
        {opportunitiesQuery.isLoading ? <LoadingState label="Loading allocator opportunities…" /> : null}
        {opportunitiesQuery.error ? <ErrorState error={opportunitiesQuery.error} onRetry={() => void opportunitiesQuery.refetch()} /> : null}
        {opportunitiesQuery.data && opportunities.length === 0 ? <EmptyState title="No allocator opportunities" message="No allocator opportunities match these filters or the backend returned none." /> : null}
        {opportunities.length > 0 ? <OpportunityRows opportunities={opportunities} /> : null}
        {opportunities.length > 0 ? <nav className="pagination-controls" aria-label="Allocator opportunity pagination"><button type="button" className="secondary-button" disabled={currentOpportunityOffset === 0} onClick={() => setAllocatorOffset('opportunity_offset', Math.max(0, currentOpportunityOffset - 10))}>Previous</button><span className="muted">Showing {currentOpportunityOffset + 1}–{currentOpportunityOffset + opportunities.length} {opportunityTotal === undefined ? 'total unavailable' : `of ${opportunityTotal}`}</span><button type="button" className="secondary-button" disabled={opportunityTotal === undefined ? opportunities.length < 10 : currentOpportunityOffset + 10 >= opportunityTotal} onClick={() => setAllocatorOffset('opportunity_offset', currentOpportunityOffset + 10)}>Next</button></nav> : null}
      </section>

      <section className="panel" aria-labelledby="decisions-heading">
        <div className="panel-header"><div><h2 id="decisions-heading">Allocation decisions</h2><p className="muted">Recent allocator decisions are read-only diagnostics. Created orders are linked in later slices.</p></div>{decisionsQuery.data ? <LastUpdated date={decisionsQuery.dataUpdatedAt} /> : null}</div>
        <form className="filter-bar" aria-label="Allocation decision filters" onSubmit={(event) => event.preventDefault()}>
          <label>Mode<select value={searchParams.get('mode') ?? ''} onChange={(event) => updateAllocatorFilters({ mode: event.target.value })}><option value="">All modes</option><option value="shadow">shadow</option><option value="paper">paper</option></select></label>
          <label>Action<select value={searchParams.get('action') ?? ''} onChange={(event) => updateAllocatorFilters({ action: event.target.value })}><option value="">All actions</option>{['shadow_selected', 'shadow_rejected', 'paper_order_intent', 'execution_rejected', 'executed'].map((action) => <option key={action} value={action}>{action}</option>)}</select></label>
          <button type="button" onClick={() => updateAllocatorFilters({ mode: '', action: '' })}>Clear filters</button>
        </form>
        {decisionsQuery.isLoading ? <LoadingState label="Loading allocation decisions…" /> : null}
        {decisionsQuery.error ? <ErrorState error={decisionsQuery.error} onRetry={() => void decisionsQuery.refetch()} /> : null}
        {decisionsQuery.data && decisions.length === 0 ? <EmptyState title="No allocation decisions" message="No allocation decisions match these filters or the backend returned none." /> : null}
        {decisions.length > 0 ? <DecisionRows decisions={decisions} /> : null}
        {decisions.length > 0 ? <nav className="pagination-controls" aria-label="Allocation decision pagination"><button type="button" className="secondary-button" disabled={currentDecisionOffset === 0} onClick={() => setAllocatorOffset('decision_offset', Math.max(0, currentDecisionOffset - 10))}>Previous</button><span className="muted">Showing {currentDecisionOffset + 1}–{currentDecisionOffset + decisions.length} {decisionTotal === undefined ? 'total unavailable' : `of ${decisionTotal}`}</span><button type="button" className="secondary-button" disabled={decisionTotal === undefined ? decisions.length < 10 : currentDecisionOffset + 10 >= decisionTotal} onClick={() => setAllocatorOffset('decision_offset', currentDecisionOffset + 10)}>Next</button></nav> : null}
      </section>
    </div>
  )
}

export function PortfolioPage() {
  const [searchParams, setSearchParams] = useSearchParams()
  const realtime = useRealtime()
  const [realtimeStale, setRealtimeStale] = useState(false)
  const offset = Number(searchParams.get('offset') ?? '0')
  const activeTab = searchParams.get('tab') === 'allocator' ? 'allocator' : 'positions'
  const filters = useMemo(() => ({
    ticker: searchParams.get('ticker') || undefined,
    side: searchParams.get('side') || undefined,
    limit: pageSize,
    offset: Number.isFinite(offset) && offset > 0 ? offset : 0,
  }), [offset, searchParams])
  const summaryQuery = useQuery({ queryKey: queryKeys.portfolioSummary, queryFn: ({ signal }) => getPortfolioSummary(signal) })
  const positionsQuery = useQuery({ queryKey: queryKeys.portfolioOpenPositions(filters), queryFn: ({ signal }) => getOpenPortfolioPositions(filters, signal) })
  const positions = positionsQuery.data?.data ?? []
  const total = positionsQuery.data?.total
  const currentOffset = filters.offset ?? 0
  const hasNext = total === undefined ? positions.length === pageSize : currentOffset + pageSize < total

  useEffect(() => {
    const latest = realtime.events[0]
    if (!latest) return
    if (latest.type === 'position_update' || latest.type === 'order_filled') {
      setRealtimeStale(true)
      void summaryQuery.refetch()
      void positionsQuery.refetch()
    }
  }, [realtime.events, positionsQuery, summaryQuery])

  function updateFilters(updates: Record<string, string>) {
    const next = new URLSearchParams(searchParams)
    for (const [key, value] of Object.entries(updates)) {
      if (value) next.set(key, value)
      else next.delete(key)
    }
    next.delete('offset')
    setSearchParams(next)
  }

  function setOffset(nextOffset: number) {
    const next = new URLSearchParams(searchParams)
    if (nextOffset > 0) next.set('offset', String(nextOffset))
    else next.delete('offset')
    setSearchParams(next)
  }

  function setTab(tab: 'positions' | 'allocator') {
    const next = new URLSearchParams(searchParams)
    if (tab === 'allocator') next.set('tab', 'allocator')
    else next.delete('tab')
    setSearchParams(next)
  }

  return (
    <div className="detail-stack">
      <PageHeader eyebrow="Paper/live clarity" title="Portfolio" description="Read-only exposure, P/L, and open positions. Broker reconciliation and actions are excluded." actions={<span className="status-pill active">Read-only</span>} />
      <Breadcrumbs items={[{ label: 'Cockpit', to: '/cockpit' }, { label: 'Portfolio' }]} />
      <section className="panel">
        <StaleBanner show={realtimeStale || realtime.status === 'disconnected' || realtime.status === 'degraded'} message="Portfolio data may be stale after realtime position/order activity. Values are display-only." />
      </section>

      <div role="tablist" aria-label="Portfolio sections" className="tabs">
        <button type="button" role="tab" aria-selected={activeTab === 'positions'} onClick={() => setTab('positions')}>Positions</button>
        <button type="button" role="tab" aria-selected={activeTab === 'allocator'} onClick={() => setTab('allocator')}>Allocator diagnostics</button>
      </div>

      {activeTab === 'allocator' ? <AllocatorPanel searchParams={searchParams} setSearchParams={setSearchParams} /> : null}

      {activeTab === 'positions' ? <>

      <section className="metrics-grid" aria-label="Portfolio summary">
        <article className="panel"><p className="eyebrow">Open positions</p><strong>{summaryQuery.data?.open_positions ?? '—'}</strong></article>
        <article className="panel"><p className="eyebrow">Unrealized P/L</p><strong><span className={`status-pill ${pnlClass(summaryQuery.data?.unrealized_pnl)}`}>{summaryQuery.data ? money(summaryQuery.data.unrealized_pnl) : '—'}</span></strong></article>
        <article className="panel"><p className="eyebrow">Realized P/L</p><strong><span className={`status-pill ${pnlClass(summaryQuery.data?.realized_pnl)}`}>{summaryQuery.data ? money(summaryQuery.data.realized_pnl) : '—'}</span></strong></article>
      </section>
      {summaryQuery.isLoading ? <LoadingState label="Loading portfolio summary…" /> : null}
      {summaryQuery.error ? <ErrorState error={summaryQuery.error} onRetry={() => void summaryQuery.refetch()} /> : null}

      {summaryQuery.data ? (
        <section className="panel" aria-labelledby="portfolio-pnl-composition">
          <h2 id="portfolio-pnl-composition">Current P/L composition</h2>
          <p className="muted">This API provides a current snapshot, not a historical equity curve. Values below are not presented as a time series.</p>
          <dl className="kv-grid">
            <dt>Unrealized</dt><dd>{money(summaryQuery.data.unrealized_pnl)}</dd>
            <dt>Realized</dt><dd>{money(summaryQuery.data.realized_pnl)}</dd>
            <dt>Total</dt><dd>{money(summaryQuery.data.unrealized_pnl + summaryQuery.data.realized_pnl)}</dd>
          </dl>
        </section>
      ) : null}

      <section className="panel" aria-labelledby="open-positions-heading">
        <div className="panel-header">
          <div><h2 id="open-positions-heading">Open positions</h2><p className="muted">Backend supports ticker and side filters for this slice.</p>{realtimeStale ? <Alert variant="warning">Data may be stale</Alert> : null}</div>
          <div className="panel-actions">{positionsQuery.data ? <LastUpdated date={positionsQuery.dataUpdatedAt} /> : null}<button type="button" className="secondary-button" onClick={() => { void summaryQuery.refetch(); void positionsQuery.refetch(); setRealtimeStale(false) }} aria-label="Refresh portfolio data"><RefreshCw size={16} /> Refresh</button></div>
        </div>
        <form className="filter-bar" aria-label="Position filters" onSubmit={(event) => event.preventDefault()} style={{ gridTemplateColumns: 'repeat(3, minmax(0, 1fr))' }}>
          <label>Ticker<input value={searchParams.get('ticker') ?? ''} onChange={(event) => updateFilters({ ticker: event.target.value.toUpperCase() })} placeholder="AUGR" /></label>
          <label>Side<select value={searchParams.get('side') ?? ''} onChange={(event) => updateFilters({ side: event.target.value })}><option value="">All</option><option value="long">Long</option><option value="short">Short</option></select></label>
          <button type="button" onClick={() => updateFilters({ ticker: '', side: '' })}>Clear filters</button>
        </form>
        {positionsQuery.isLoading ? <LoadingState label="Loading open positions…" /> : null}
        {positionsQuery.error ? <ErrorState error={positionsQuery.error} onRetry={() => void positionsQuery.refetch()} /> : null}
        {positionsQuery.data && positions.length === 0 ? <EmptyState title="No open positions" message="No open positions match these filters." /> : null}
        {positions.length > 0 ? <PositionRows positions={positions} /> : null}
        {positions.length > 0 ? (
          <nav className="pagination-controls" aria-label="Position pagination">
            <button type="button" className="secondary-button" disabled={currentOffset === 0} onClick={() => setOffset(Math.max(0, currentOffset - pageSize))}>Previous</button>
            <span className="muted">Showing {currentOffset + 1}–{currentOffset + positions.length} {total === undefined ? 'total unavailable' : `of ${total}`}</span>
            <button type="button" className="secondary-button" disabled={!hasNext} onClick={() => setOffset(currentOffset + pageSize)}>Next</button>
          </nav>
        ) : null}
      </section>
      </> : null}
    </div>
  )
}
