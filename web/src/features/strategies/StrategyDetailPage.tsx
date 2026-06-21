import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useEffect, useMemo, useState, type KeyboardEvent } from 'react'
import { Link, useNavigate, useParams, useSearchParams } from 'react-router-dom'

import { deleteStrategy, getLatestStrategyReport, getRuns, getStrategy, getStrategyReports, pauseStrategy, resumeStrategy, runStrategy, skipNextStrategy } from '@/shared/api/endpoints'
import { isApiClientError } from '@/shared/api/errors'
import { refreshAccessToken } from '@/shared/auth/refresh'
import { isAccessTokenExpiringSoon } from '@/shared/auth/tokenStore'
import { ConfirmationDialog } from '@/shared/components/ConfirmationDialog'
import { Breadcrumbs, CopyButton, EntityId, EntityLink } from '@/shared/components/EntityLinks'
import { EmptyState, ErrorState, LastUpdated, LoadingState, StaleBanner } from '@/shared/components/QueryStates'
import { queryKeys } from '@/shared/query/keys'
import type { ReportArtifact, ReportLatestResponse, Strategy, StrategyLatestRunSummary } from '@/shared/types/domain'
import { useRealtime } from '@/shared/websocket/RealtimeProvider'

type StrategyAction = 'pause' | 'resume' | 'skip-next' | 'run'
type ActionDialogError = { message: string; unknownCompletion?: boolean } | null
type DetailTab = 'overview' | 'config' | 'reports'
const reportsPageSize = 5
const deleteTypedToken = 'DELETE'

const actionLabels: Record<StrategyAction, { verb: string; button: string; title: string; confirm: string; success: string }> = {
  pause: { verb: 'pause', button: 'Pause paper strategy', title: 'Pause paper strategy?', confirm: 'Pause paper strategy', success: 'Pause confirmed' },
  resume: { verb: 'resume', button: 'Resume paper strategy', title: 'Resume paper strategy?', confirm: 'Resume paper strategy', success: 'Resume confirmed' },
  'skip-next': { verb: 'skip next run for', button: 'Skip next paper run', title: 'Skip next paper run?', confirm: 'Skip next run', success: 'Skip-next confirmed' },
  run: { verb: 'start a manual run for', button: 'Run paper strategy now', title: 'Run paper strategy now?', confirm: 'Start paper run', success: 'Manual run accepted' },
}

function mutationErrorMessage(error: unknown, action: StrategyAction): ActionDialogError {
  const label = actionLabels[action]
  if (!isApiClientError(error)) return { message: `The ${label.verb} request failed. The outcome is unknown; verify the server state before retrying.`, unknownCompletion: true }
  switch (error.kind) {
    case 'unauthorized':
      return { message: `Your session is no longer authorized. Sign in again before trying to ${label.verb} this strategy.` }
    case 'conflict':
      return { message: `The strategy state changed before ${label.verb} could complete. Review the refreshed status before trying again.` }
    case 'validation':
    case 'bad_request':
      return { message: `The ${label.verb} request was rejected. Review the strategy state before trying again.` }
    case 'rate_limited':
      return { message: `${label.button} is temporarily rate limited. Wait before trying again.` }
    case 'network':
      return { message: `Network failed while submitting ${label.verb}. Completion is unknown; verify the server state before retrying.`, unknownCompletion: true }
    case 'server':
      return { message: `The server could not complete ${label.verb} safely. Verify the strategy state before retrying.` }
    case 'not_implemented':
      return { message: `${label.button} is not available on this server.` }
    default:
      return { message: `${label.button} did not complete. Review the confirmed server state before retrying.` }
  }
}

function deleteErrorMessage(error: unknown): ActionDialogError {
  if (!isApiClientError(error)) {
    if (error instanceof Error) return { message: error.message }
    return { message: 'Delete request failed. Completion is unknown; verify the server state before retrying.', unknownCompletion: true }
  }
  switch (error.kind) {
    case 'unauthorized':
      return { message: 'Your session is no longer authorized. Sign in again before deleting this strategy.' }
    case 'conflict':
      return { message: 'Delete was rejected because the strategy state changed, is live, or has active work. Review the refreshed detail before retrying.' }
    case 'validation':
    case 'bad_request':
      return { message: 'Delete was rejected. Review the strategy state before trying again.' }
    case 'rate_limited':
      return { message: 'Strategy delete is temporarily rate limited. Wait before trying again.' }
    case 'network':
      return { message: 'Network failed while submitting delete. Completion is unknown; verify the server state before retrying.', unknownCompletion: true }
    case 'server':
      return { message: 'The server could not complete delete safely. Verify the strategy state before retrying.' }
    case 'not_implemented':
      return { message: 'Strategy delete is not available on this server.' }
    case 'not_found':
      return null
    default:
      return { message: 'Strategy delete did not complete. Review the confirmed server state before retrying.' }
  }
}

function assertActionAllowed(strategy: Strategy | undefined, action: StrategyAction, realtimeStale: boolean) {
  if (!strategy) throw new Error('Strategy is not loaded.')
  if (!strategy.is_paper) throw new Error('Live strategies cannot use this paper-only action workflow.')
  if (realtimeStale) throw new Error('Strategy detail is stale. Refresh before using paper action controls.')
  if (action === 'pause' && strategy.status !== 'active') throw new Error('Only active paper strategies can be paused.')
  if (action === 'resume' && strategy.status !== 'paused') throw new Error('Only paused paper strategies can be resumed.')
  if (action === 'skip-next' && strategy.status !== 'active') throw new Error('Only active paper strategies can skip the next run.')
  if (action === 'skip-next' && strategy.skip_next_run) throw new Error('This strategy is already marked to skip the next run.')
  if (action === 'run' && strategy.status !== 'active') throw new Error('Only active paper strategies can be run manually.')
}

async function verifiedRefetch(queryClient: ReturnType<typeof useQueryClient>, strategyId: string): Promise<Strategy> {
  await queryClient.invalidateQueries({ queryKey: queryKeys.strategyList })
  await queryClient.invalidateQueries({ queryKey: queryKeys.strategyDetail(strategyId) })
  await queryClient.invalidateQueries({ queryKey: queryKeys.runningRuns })
  await queryClient.invalidateQueries({ queryKey: queryKeys.strategyRuns(strategyId) })
  return queryClient.fetchQuery({ queryKey: queryKeys.strategyDetail(strategyId), queryFn: ({ signal }) => getStrategy(strategyId, signal), retry: false })
}

async function verifyDeleteAbsence(queryClient: ReturnType<typeof useQueryClient>, strategyId: string): Promise<void> {
  await queryClient.invalidateQueries({ queryKey: queryKeys.strategyList })
  await queryClient.invalidateQueries({ queryKey: queryKeys.strategyDetail(strategyId) })
  await queryClient.invalidateQueries({ queryKey: queryKeys.runningRuns })
  await queryClient.invalidateQueries({ queryKey: queryKeys.strategyRuns(strategyId) })
  await queryClient.invalidateQueries({ queryKey: queryKeys.strategyReports(strategyId, {}) })
  try {
    await queryClient.fetchQuery({ queryKey: queryKeys.strategyDetail(strategyId), queryFn: ({ signal }) => getStrategy(strategyId, signal), retry: false })
  } catch (error) {
    if (isApiClientError(error) && error.kind === 'not_found') return
    throw error
  }
  throw new Error('Strategy still exists after delete verification.')
}

function titleCase(value?: string) {
  if (!value) return 'Unknown'
  return value.replace(/_/g, ' ')
}

function StatusPill({ value }: { value: string }) {
  const known = ['active', 'paused', 'inactive'].includes(value)
  return <span className={`status-pill ${known ? value : 'unknown'}`}>{known ? value : `Unknown: ${value}`}</span>
}

function ModePill({ isPaper }: { isPaper: boolean }) {
  return <span className={`status-pill ${isPaper ? 'paper' : 'live'}`}>{isPaper ? 'PAPER' : 'LIVE'}</span>
}

function JsonViewer({ value, title = 'Config JSON', copyLabel = 'Copy strategy config JSON' }: { value: unknown; title?: string; copyLabel?: string }) {
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

function ReportStatusPill({ value }: { value: string }) {
  const known = ['completed', 'failed', 'running'].includes(value)
  return <span className={`status-pill ${known ? value : 'unknown'}`}>{known ? value : `Unknown: ${value}`}</span>
}

function ReportSummary({ report }: { report: ReportArtifact | ReportLatestResponse }) {
  return (
    <dl className="kv-grid">
      <dt>Report ID</dt><dd><code>{report.id}</code></dd>
      <dt>Type</dt><dd>{titleCase(report.report_type)}</dd>
      <dt>Status</dt><dd><ReportStatusPill value={report.status} /></dd>
      <dt>Bucket</dt><dd>{new Date(report.time_bucket).toLocaleString()}</dd>
      <dt>Provider</dt><dd>{report.provider || 'Unknown'}</dd>
      <dt>Model</dt><dd>{report.model || 'Unknown'}</dd>
      <dt>Tokens</dt><dd>{report.prompt_tokens + report.completion_tokens} total</dd>
      <dt>Latency</dt><dd>{report.latency_ms} ms</dd>
      <dt>Completed</dt><dd>{report.completed_at ? new Date(report.completed_at).toLocaleString() : 'Not completed'}</dd>
      {'stale_seconds' in report ? <><dt>Age</dt><dd>{Math.round(report.stale_seconds)} seconds stale</dd></> : null}
      {report.error_message ? <><dt>Error</dt><dd>{report.error_message}</dd></> : null}
    </dl>
  )
}

function ReportsPanel({ strategyId, realtimeStale }: { strategyId: string; realtimeStale: boolean }) {
  const [searchParams, setSearchParams] = useSearchParams()
  const reportOffset = Number(searchParams.get('report_offset') ?? '0')
  const reportType = searchParams.get('report_type') || 'paper_validation'
  const filters = useMemo(() => ({ report_type: reportType, limit: reportsPageSize, offset: reportOffset }), [reportOffset, reportType])

  const latestQuery = useQuery({
    queryKey: queryKeys.strategyReportLatest(strategyId, reportType),
    queryFn: ({ signal }) => getLatestStrategyReport(strategyId, reportType, signal),
    retry: false,
  })
  const historyQuery = useQuery({
    queryKey: queryKeys.strategyReports(strategyId, filters),
    queryFn: ({ signal }) => getStrategyReports(strategyId, filters, signal),
  })

  const latestNotFound = isApiClientError(latestQuery.error) && latestQuery.error.kind === 'not_found'
  const history = historyQuery.data?.data ?? []
  const hasNext = history.length === reportsPageSize

  function setReportOffset(offset: number) {
    const next = new URLSearchParams(searchParams)
    next.set('tab', 'reports')
    if (offset <= 0) next.delete('report_offset')
    else next.set('report_offset', String(offset))
    setSearchParams(next)
  }

  return (
    <div className="reports-stack" role="tabpanel" aria-label="Strategy reports">
      <StaleBanner show={realtimeStale} message="Realtime activity may make reports stale. Do not infer trading safety from stale reports." />
      <section className="panel" aria-labelledby="latest-report-heading">
        <div className="panel-header">
          <div>
            <h2 id="latest-report-heading">Latest report</h2>
            <p className="muted">Type: {titleCase(reportType)}</p>
          </div>
          {latestQuery.data ? <LastUpdated date={latestQuery.data.completed_at ?? latestQuery.data.created_at} /> : null}
        </div>
        {latestQuery.isLoading ? <LoadingState label="Loading latest report…" /> : null}
        {latestNotFound ? <EmptyState title="No completed latest report" message="This strategy does not have a completed report for the selected type yet." /> : null}
        {latestQuery.error && !latestNotFound ? <ErrorState error={latestQuery.error} onRetry={() => void latestQuery.refetch()} /> : null}
        {latestQuery.data ? (
          <div className="reports-grid">
            <ReportSummary report={latestQuery.data} />
            <JsonViewer value={latestQuery.data.report_json ?? {}} title="Report JSON" copyLabel="Copy latest report JSON" />
          </div>
        ) : null}
      </section>

      <section className="panel" aria-labelledby="history-report-heading">
        <div className="panel-header">
          <h2 id="history-report-heading">Historical reports</h2>
          {historyQuery.data ? <span className="muted">Showing {history.length} from offset {historyQuery.data.offset}</span> : null}
        </div>
        {historyQuery.isLoading ? <LoadingState label="Loading report history…" /> : null}
        {historyQuery.error ? <ErrorState error={historyQuery.error} onRetry={() => void historyQuery.refetch()} /> : null}
        {historyQuery.data && history.length === 0 ? <EmptyState title="No historical reports" message="Report history is empty for this strategy." /> : null}
        {history.length > 0 ? (
          <>
            <div className="table-wrap">
              <table aria-label="Strategy report history">
                <thead>
                  <tr><th>Type</th><th>Status</th><th>Bucket</th><th>Provider</th><th>Tokens</th><th>Completed</th></tr>
                </thead>
                <tbody>
                  {history.map((report) => (
                    <tr key={report.id}>
                      <td>{titleCase(report.report_type)}</td>
                      <td><ReportStatusPill value={report.status} /></td>
                      <td>{new Date(report.time_bucket).toLocaleString()}</td>
                      <td>{report.provider || 'Unknown'}</td>
                      <td>{report.prompt_tokens + report.completion_tokens}</td>
                      <td>{report.completed_at ? new Date(report.completed_at).toLocaleString() : 'Not completed'}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
            <div className="pagination-controls" aria-label="Report history pagination">
              <button type="button" className="secondary-button" disabled={reportOffset === 0} onClick={() => setReportOffset(Math.max(0, reportOffset - reportsPageSize))}>Previous</button>
              <span>Offset {reportOffset}</span>
              <button type="button" className="secondary-button" disabled={!hasNext} onClick={() => setReportOffset(reportOffset + reportsPageSize)}>Next</button>
            </div>
          </>
        ) : null}
      </section>
    </div>
  )
}

function LatestRunSummary({ summary }: { summary?: StrategyLatestRunSummary }) {
  if (!summary) {
    return <p className="muted">No latest run summary was returned by this server.</p>
  }
  return (
    <section className="panel" aria-labelledby="latest-run-heading">
      <div className="panel-header">
        <h2 id="latest-run-heading">Latest run summary</h2>
        <EntityLink kind="run" id={summary.id} label="Open run" copy={false} />
      </div>
      <dl className="kv-grid">
        <dt>Run ID</dt><dd><EntityId kind="run" id={summary.id} /></dd>
        <dt>Status</dt><dd>{titleCase(summary.status)}</dd>
        <dt>Signal</dt><dd>{summary.signal ? titleCase(summary.signal) : 'Not available'}</dd>
        <dt>Started</dt><dd>{new Date(summary.started_at).toLocaleString()}</dd>
        <dt>Completed</dt><dd>{summary.completed_at ? new Date(summary.completed_at).toLocaleString() : 'Not completed'}</dd>
      </dl>
    </section>
  )
}

function DetailTabs({ activeTab, onChange }: { activeTab: DetailTab; onChange: (tab: DetailTab) => void }) {
  const tabs: DetailTab[] = ['overview', 'config', 'reports']
  function onKeyDown(event: KeyboardEvent<HTMLDivElement>) {
    if (event.key !== 'ArrowRight' && event.key !== 'ArrowLeft') return
    event.preventDefault()
    const currentIndex = tabs.indexOf(activeTab)
    const direction = event.key === 'ArrowRight' ? 1 : -1
    onChange(tabs[(currentIndex + direction + tabs.length) % tabs.length])
  }
  return (
    <div className="tabs" role="tablist" aria-label="Strategy detail tabs" onKeyDown={onKeyDown}>
      {tabs.map((tab) => (
        <button key={tab} type="button" role="tab" aria-selected={activeTab === tab} className={activeTab === tab ? 'active' : ''} onClick={() => onChange(tab)}>
          {tab === 'overview' ? 'Overview' : tab === 'config' ? 'Config' : 'Reports'}
        </button>
      ))}
    </div>
  )
}

export function StrategyDetailPage() {
  const { id } = useParams()
  const navigate = useNavigate()
  const [searchParams, setSearchParams] = useSearchParams()
  const strategyId = id ?? ''
  const tabParam = searchParams.get('tab')
  const activeTab: DetailTab = tabParam === 'config' || tabParam === 'reports' ? tabParam : 'overview'
  const realtime = useRealtime()
  const queryClient = useQueryClient()
  const [dialogAction, setDialogAction] = useState<StrategyAction | null>(null)
  const [dialogError, setDialogError] = useState<ActionDialogError>(null)
  const [deleteDialogOpen, setDeleteDialogOpen] = useState(false)
  const [deleteToken, setDeleteToken] = useState('')
  const [deleteDialogError, setDeleteDialogError] = useState<ActionDialogError>(null)
  const [verifiedMessage, setVerifiedMessage] = useState<string | null>(null)
  const [verifying, setVerifying] = useState(false)
  const [deleteVerifying, setDeleteVerifying] = useState(false)
  const [realtimeStale, setRealtimeStale] = useState(false)

  const strategyQuery = useQuery({
    queryKey: queryKeys.strategyDetail(strategyId),
    queryFn: ({ signal }) => getStrategy(strategyId, signal),
    enabled: Boolean(strategyId),
  })

  const runningRunsQuery = useQuery({
    queryKey: queryKeys.runningRuns,
    queryFn: ({ signal }) => getRuns({ strategy_id: strategyId, status: 'running', limit: 1, offset: 0 }, signal),
    enabled: Boolean(strategyId && strategyQuery.data?.is_paper),
    retry: false,
  })

  useEffect(() => {
    if (!strategyId || realtime.events.length === 0) return
    const latest = realtime.events[0]
    if (latest.strategy_id === strategyId || latest.run_id === strategyQuery.data?.latest_run_summary?.id) setRealtimeStale(true)
  }, [realtime.events, strategyId, strategyQuery.data?.latest_run_summary?.id])

  const actionMutation = useMutation({
    mutationFn: async (action: StrategyAction) => {
      assertActionAllowed(strategyQuery.data, action, realtimeStale)
      if (isAccessTokenExpiringSoon()) await refreshAccessToken()
      switch (action) {
        case 'pause':
          return pauseStrategy(strategyId)
        case 'resume':
          return resumeStrategy(strategyId)
        case 'skip-next':
          return skipNextStrategy(strategyId)
        case 'run':
          return runStrategy(strategyId)
      }
    },
    retry: false,
    onSuccess: async (_result, action) => {
      setVerifying(true)
      setDialogError(null)
      try {
        const confirmed = await verifiedRefetch(queryClient, strategyId)
        setVerifiedMessage(`${actionLabels[action].success}. Confirmed server state: ${confirmed.status}${confirmed.skip_next_run ? ' · skip next: yes' : ''}`)
        setDialogAction(null)
      } catch {
        setDialogError({ message: `${actionLabels[action].button} was accepted, but the confirmed server state could not be refetched. Completion is unknown until the strategy detail loads successfully.`, unknownCompletion: true })
      } finally {
        setVerifying(false)
      }
    },
    onError: async (error, action) => {
      setDialogError(mutationErrorMessage(error, action))
      if (isApiClientError(error) && ['conflict', 'validation', 'bad_request', 'rate_limited'].includes(error.kind)) {
        await queryClient.invalidateQueries({ queryKey: queryKeys.strategyDetail(strategyId) })
      }
    },
  })

  const deleteMutation = useMutation({
    mutationFn: async () => {
      if (!strategyQuery.data) throw new Error('Strategy is not loaded.')
      if (!strategyQuery.data.is_paper) throw new Error('Live strategies cannot be deleted by this paper-only workflow.')
      if (realtimeStale) throw new Error('Strategy detail is stale. Refresh before deleting.')
      if (runningRunsQuery.data && runningRunsQuery.data.data.length > 0) throw new Error('This strategy has a running run. Wait for it to finish before deleting.')
      if (deleteToken.trim() !== deleteTypedToken) throw new Error('Type DELETE to confirm strategy deletion.')
      if (isAccessTokenExpiringSoon()) await refreshAccessToken()
      return deleteStrategy(strategyId)
    },
    retry: false,
    onSuccess: async () => {
      setDeleteVerifying(true)
      setDeleteDialogError(null)
      try {
        await verifyDeleteAbsence(queryClient, strategyId)
        setDeleteDialogOpen(false)
        setDeleteToken('')
        navigate('/strategies', { replace: true, state: { message: 'Verified strategy deleted.' } })
      } catch {
        setDeleteDialogError({ message: 'Delete was accepted, but verified absence failed. Completion is unknown until the strategy detail returns 404 or the list confirms removal.', unknownCompletion: true })
      } finally {
        setDeleteVerifying(false)
      }
    },
    onError: async (error) => {
      if (isApiClientError(error) && error.kind === 'not_found') {
        await queryClient.invalidateQueries({ queryKey: queryKeys.strategyList })
        setDeleteDialogOpen(false)
        setDeleteToken('')
        navigate('/strategies', { replace: true, state: { message: 'Strategy was already deleted.' } })
        return
      }
      setDeleteDialogError(deleteErrorMessage(error))
      if (isApiClientError(error) && ['conflict', 'validation', 'bad_request', 'rate_limited'].includes(error.kind)) {
        await queryClient.invalidateQueries({ queryKey: queryKeys.strategyDetail(strategyId) })
        await queryClient.invalidateQueries({ queryKey: queryKeys.runningRuns })
      }
    },
  })

  const strategy = strategyQuery.data
  const canPause = Boolean(strategy?.is_paper && strategy.status === 'active' && !realtimeStale)
  const canResume = Boolean(strategy?.is_paper && strategy.status === 'paused' && !realtimeStale)
  const canSkipNext = Boolean(strategy?.is_paper && strategy.status === 'active' && !strategy.skip_next_run && !realtimeStale)
  const canRun = Boolean(strategy?.is_paper && strategy.status === 'active' && !realtimeStale)
  const hasRunningRuns = Boolean(runningRunsQuery.data?.data.length)
  const deleteBusy = deleteMutation.isPending || deleteVerifying
  const canDelete = Boolean(strategy?.is_paper && !realtimeStale && !runningRunsQuery.isLoading && !hasRunningRuns && !runningRunsQuery.error)
  const busy = actionMutation.isPending || verifying
  const activeDialogLabels = dialogAction ? actionLabels[dialogAction] : actionLabels.pause
  const notFound = isApiClientError(strategyQuery.error) && strategyQuery.error.kind === 'not_found'
  const showStale = Boolean(strategy && (strategyQuery.isStale || realtimeStale || realtime.status === 'disconnected' || realtime.status === 'degraded'))

  function setTab(tab: DetailTab) {
    const next = new URLSearchParams(searchParams)
    if (tab === 'overview') next.delete('tab')
    else next.set('tab', tab)
    setSearchParams(next)
  }

  return (
    <div className="detail-stack">
      <Breadcrumbs items={[{ label: 'Cockpit', to: '/cockpit' }, { label: 'Strategies', to: '/strategies' }, { label: strategy?.name ?? 'Strategy detail' }]} />

      <section className="panel hero-panel">
        <p className="eyebrow">Strategy detail</p>
        {strategyQuery.isLoading ? <LoadingState label="Loading strategy detail…" /> : null}
        {notFound ? <div role="alert" className="error-box">Strategy not found. Return to the strategies list and verify the link.</div> : null}
        {strategyQuery.error && !notFound ? <ErrorState error={strategyQuery.error} onRetry={() => void strategyQuery.refetch()} /> : null}
        {strategy ? (
          <>
            <div className="panel-header">
              <div>
                <h1>{strategy.name}</h1>
                <p className="muted">{strategy.ticker} · {titleCase(strategy.market_type)}</p>
              </div>
              <div className="header-cluster">
                <Link className="secondary-link" to={`/strategies/${strategy.id}/edit`}>Edit safe fields</Link>
                <StatusPill value={strategy.status} />
                <ModePill isPaper={strategy.is_paper} />
              </div>
            </div>
            <LastUpdated date={strategyQuery.dataUpdatedAt || strategy.updated_at} />
            <StaleBanner show={showStale} message="Strategy detail may be stale. Refresh before taking operational action." />
            {verifiedMessage ? <p role="status" className="success-box">{verifiedMessage}</p> : null}
            {!strategy.is_paper ? <p role="status" className="warning-box">Live strategies cannot use this paper-mode action workflow.</p> : null}
            {strategy.is_paper && strategy.status === 'inactive' ? <p role="status" className="warning-box">Inactive paper strategies cannot use low-risk action controls.</p> : null}
            {strategy.is_paper && strategy.skip_next_run ? <p role="status" className="warning-box">This strategy is already marked to skip its next scheduled run.</p> : null}
            {hasRunningRuns ? <p role="status" className="warning-box">This strategy has a running run. Destructive delete is blocked until running work is complete.</p> : null}
            {runningRunsQuery.error ? <p role="status" className="warning-box">Running-run verification failed. Destructive delete is disabled until the server check succeeds.</p> : null}
            {realtimeStale ? <p role="status" className="warning-box">Realtime activity was received for this strategy. Refresh before using action controls.</p> : null}

            <DetailTabs activeTab={activeTab} onChange={setTab} />

            {activeTab === 'overview' ? (
              <div className="detail-grid" role="tabpanel" aria-label="Strategy overview">
                <section className="panel" aria-labelledby="identity-heading">
                  <h2 id="identity-heading">Identity</h2>
                  <dl className="kv-grid">
                    <dt>ID</dt><dd><EntityId kind="strategy" id={strategy.id} /></dd>
                    <dt>Description</dt><dd>{strategy.description || 'No description'}</dd>
                    <dt>Ticker</dt><dd>{strategy.ticker}</dd>
                    <dt>Market</dt><dd>{titleCase(strategy.market_type)}</dd>
                    <dt>Status</dt><dd><StatusPill value={strategy.status} /></dd>
                    <dt>Mode</dt><dd><ModePill isPaper={strategy.is_paper} /></dd>
                    <dt>Schedule</dt><dd>{strategy.schedule_cron || 'Not scheduled'}</dd>
                    <dt>Skip next run</dt><dd>{strategy.skip_next_run ? 'Yes' : 'No'}</dd>
                    <dt>Created</dt><dd>{new Date(strategy.created_at).toLocaleString()}</dd>
                    <dt>Updated</dt><dd>{new Date(strategy.updated_at).toLocaleString()}</dd>
                  </dl>
                </section>
                <LatestRunSummary summary={strategy.latest_run_summary} />
              </div>
            ) : activeTab === 'config' ? (
              <div role="tabpanel" aria-label="Strategy config">
                <JsonViewer value={strategy.config} />
              </div>
            ) : (
              <ReportsPanel strategyId={strategy.id} realtimeStale={realtimeStale} />
            )}

            <section className="panel" aria-labelledby="actions-heading">
              <h2 id="actions-heading">Paper action controls</h2>
              <p className="muted">Low-risk strategy actions are paper-only, confirmed before submit, never optimistic, and disabled when detail data is stale.</p>
              <div className="action-row">
              <button type="button" className="danger-button" disabled={!canPause || busy} onClick={() => { setDialogError(null); setDialogAction('pause') }}>
                Pause paper strategy
              </button>
              <button type="button" className="secondary-button" disabled={!canResume || busy} onClick={() => { setDialogError(null); setDialogAction('resume') }}>
                Resume paper strategy
              </button>
              <button type="button" className="secondary-button" disabled={!canSkipNext || busy} onClick={() => { setDialogError(null); setDialogAction('skip-next') }}>
                Skip next paper run
              </button>
              <button type="button" className="secondary-button" disabled={!canRun || busy} onClick={() => { setDialogError(null); setDialogAction('run') }}>
                Run paper strategy now
              </button>
              </div>
            </section>

            <section className="panel danger-zone" aria-labelledby="delete-strategy-heading">
              <h2 id="delete-strategy-heading">Destructive paper delete</h2>
              <p className="muted">Slice 21 is paper-only. Delete requires typed confirmation, never uses optimistic state, and verifies server absence before leaving detail.</p>
              <button type="button" className="danger-button" disabled={!canDelete || busy || deleteBusy} onClick={() => { setDeleteDialogError(null); setDeleteToken(''); setDeleteDialogOpen(true) }}>
                Delete paper strategy
              </button>
            </section>
          </>
        ) : null}
      </section>

      {strategy ? (
        <ConfirmationDialog
          open={Boolean(dialogAction)}
          title={activeDialogLabels.title}
          confirmLabel={activeDialogLabels.confirm}
          busy={busy}
          disableDismiss={busy}
          error={dialogError ? <>{dialogError.message}{dialogError.unknownCompletion ? <strong> Do not retry until server state is verified.</strong> : null}</> : null}
          onCancel={() => { if (!busy) setDialogAction(null) }}
          onConfirm={() => { if (!busy && dialogAction) actionMutation.mutate(dialogAction) }}
        >
          <p><strong>{strategy.name}</strong> ({strategy.ticker}) is currently <strong>{strategy.status}</strong>.</p>
          <p><strong>PAPER only.</strong> This action may affect scheduled or active paper behavior. It will not be optimistically applied; the UI will refetch and display the confirmed server state.</p>
        </ConfirmationDialog>
      ) : null}

      {strategy ? (
        <ConfirmationDialog
          open={deleteDialogOpen}
          title="Delete paper strategy?"
          confirmLabel="Delete paper strategy"
          busy={deleteBusy}
          disableDismiss={deleteBusy}
          error={deleteDialogError ? <>{deleteDialogError.message}{deleteDialogError.unknownCompletion ? <strong> Do not retry until server state is verified.</strong> : null}</> : null}
          onCancel={() => { if (!deleteBusy) { setDeleteDialogOpen(false); setDeleteToken(''); setDeleteDialogError(null) } }}
          onConfirm={() => { if (!deleteBusy) deleteMutation.mutate() }}
        >
          <p><strong>{strategy.name}</strong> ({strategy.ticker}) will be permanently deleted if the server accepts the request.</p>
          <p><strong>PAPER only.</strong> Backend paper/RBAC/running-run preconditions are still unresolved, so the UI blocks live strategies and running-run evidence before submit.</p>
          <label className="form-field">
            <span>Type DELETE to confirm</span>
            <input value={deleteToken} onChange={(event) => setDeleteToken(event.target.value)} autoComplete="off" />
          </label>
        </ConfirmationDialog>
      ) : null}
    </div>
  )
}
