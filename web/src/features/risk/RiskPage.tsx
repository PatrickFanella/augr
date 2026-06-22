import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useEffect, useState } from 'react'
import { Link, useSearchParams } from 'react-router-dom'

import { getAllocatorDiagnostics, getRiskBreakers, getRiskCockpit, getRiskStatus, resetRiskBreaker, resumeMarketKillSwitch, stopMarketKillSwitch, toggleKillSwitch } from '@/shared/api/endpoints'
import { isApiClientError } from '@/shared/api/errors'
import { refreshAccessToken } from '@/shared/auth/refresh'
import { isAccessTokenExpiringSoon } from '@/shared/auth/tokenStore'
import { ConfirmationDialog } from '@/shared/components/ConfirmationDialog'
import { EmptyState, ErrorState, LastUpdated, LoadingState, StaleBanner } from '@/shared/components/QueryStates'
import { queryKeys } from '@/shared/query/keys'
import { marketTypes } from '@/shared/types/domain'
import type { MarketType, RiskBreakerState, RiskCockpitExposure, RiskCockpitSummary, RiskEngineStatus } from '@/shared/types/domain'
import { useRealtime } from '@/shared/websocket/RealtimeProvider'

function percent(value?: number) {
  if (value === undefined) return '—'
  return `${new Intl.NumberFormat(undefined, { maximumFractionDigits: 2 }).format(value * 100)}%`
}

function numberValue(value: number) {
  return new Intl.NumberFormat(undefined, { maximumFractionDigits: 2 }).format(value)
}

function timeValue(value?: string) {
  return value ? new Date(value).toLocaleString() : '—'
}

function displayEnum(value: string) {
  return value.replaceAll('_', ' ')
}

function statusTone(value: string, mapping: Record<string, string>) {
  return mapping[value] ?? 'unknown'
}

function StatusPill({ value, known }: { value: string; known: string[] }) {
  const normalized = displayEnum(value)
  return <span className={`status-pill ${known.includes(value) ? value : 'unknown'}`}>{known.includes(value) ? normalized : `Unknown: ${normalized}`}</span>
}

function RiskStatusPanel({ status }: { status: RiskEngineStatus }) {
  return (
    <div className="reports-stack">
      <div className="metrics-grid">
        <div><span className="muted">Risk status</span><strong><span className={`status-pill ${statusTone(status.risk_status, { normal: 'active', warning: 'warning', breached: 'breached' })}`}>{displayEnum(status.risk_status)}</span></strong></div>
        <div><span className="muted">Circuit breaker</span><strong><span className={`status-pill ${statusTone(status.circuit_breaker.state, { open: 'active', tripped: 'warning', cooldown: 'degraded' })}`}>{displayEnum(status.circuit_breaker.state)}</span></strong></div>
        <div><span className="muted">Kill switch</span><strong><span className={`status-pill ${status.kill_switch.active ? 'breached' : 'active'}`}>{status.kill_switch.active ? 'Active' : 'Inactive'}</span></strong></div>
        <div><span className="muted">Open positions</span><strong>{status.position_limits.current_open_positions ?? '—'} / {status.position_limits.max_concurrent}</strong></div>
      </div>
      <dl className="kv-grid">
        <dt>Max per position</dt><dd>{percent(status.position_limits.max_per_position_pct)}</dd>
        <dt>Max total exposure</dt><dd>{percent(status.position_limits.max_total_pct)}</dd>
        <dt>Current total exposure</dt><dd>{percent(status.position_limits.current_total_exposure_pct)}</dd>
        <dt>Max per market</dt><dd>{percent(status.position_limits.max_per_market_pct)}</dd>
        <dt>Circuit reason</dt><dd>{status.circuit_breaker.reason ?? '—'}</dd>
        <dt>Kill reason</dt><dd>{status.kill_switch.reason ?? '—'}</dd>
        <dt>Tripped at</dt><dd>{timeValue(status.circuit_breaker.tripped_at)}</dd>
        <dt>Cooldown end</dt><dd>{timeValue(status.circuit_breaker.cooldown_end)}</dd>
      </dl>
    </div>
  )
}

function AllocatorDiagnosticsPanel({ diagnostics }: { diagnostics: Awaited<ReturnType<typeof getAllocatorDiagnostics>> }) {
  return (
    <div className="reports-stack">
      <div className="metrics-grid">
        <div><span className="muted">Buying power utilization</span><strong>{percent(diagnostics.buying_power_utilization_pct)}</strong></div>
        <div><span className="muted">Gross exposure</span><strong>{percent(diagnostics.gross_exposure_pct)}</strong></div>
        <div><span className="muted">Target exposure</span><strong>{percent(diagnostics.target_gross_exposure_pct)}</strong></div>
        <div><span className="muted">Utilization gap</span><strong>{percent(diagnostics.utilization_gap_pct)}</strong></div>
      </div>
      <div className="detail-grid">
        <dl className="kv-grid">
          <dt>Active strategies by market</dt><dd>{Object.entries(diagnostics.active_strategies_by_market).map(([market, count]) => `${displayEnum(market)}: ${count}`).join(', ') || '—'}</dd>
          <dt>Open positions by market</dt><dd>{Object.entries(diagnostics.open_positions_by_market).map(([market, count]) => `${displayEnum(market)}: ${count}`).join(', ') || '—'}</dd>
        </dl>
      </div>
      {diagnostics.warnings.length > 0 ? <div role="alert" className="inline-alert warning">Warnings: {diagnostics.warnings.join(', ')}</div> : null}
    </div>
  )
}

function ExposureRows({ exposures }: { exposures: RiskCockpitExposure[] }) {
  if (exposures.length === 0) return <EmptyState title="No cockpit exposure" message="No risk cockpit exposures are available." />
  return (
    <>
      <div className="table-wrap">
        <table aria-label="Risk exposures">
          <thead><tr><th>Market</th><th>Open positions</th><th>Approved</th><th>Rejected</th><th>Gross exposure</th><th>Net expected value</th></tr></thead>
          <tbody>{exposures.map((exposure) => (
            <tr key={exposure.market_type}>
              <td><StatusPill value={exposure.market_type} known={['stock', 'crypto', 'options', 'polymarket', 'kalshi']} /></td>
              <td>{exposure.open_positions}</td>
              <td>{exposure.approved_decisions}</td>
              <td>{exposure.rejected_decisions}</td>
              <td>{numberValue(exposure.gross_exposure)}</td>
              <td>{numberValue(exposure.net_expected_value)}</td>
            </tr>
          ))}</tbody>
        </table>
      </div>
      <div className="card-list" aria-label="Risk exposure cards">
        {exposures.map((exposure) => (
          <article className="strategy-card" key={exposure.market_type}>
            <h3>{displayEnum(exposure.market_type)}</h3>
            <p>{exposure.open_positions} open positions · {numberValue(exposure.gross_exposure)} gross exposure</p>
            <p>{exposure.approved_decisions} approved / {exposure.rejected_decisions} rejected decisions</p>
          </article>
        ))}
      </div>
    </>
  )
}

function BreakerRows({ breakers, canReset, busyScope, onReset }: { breakers: RiskBreakerState[]; canReset: boolean; busyScope?: string; onReset: (scope: string) => void }) {
  if (breakers.length === 0) return <EmptyState title="No tripped breakers" message="No persisted risk breakers are currently tripped." />
  return (
    <div className="table-wrap">
      <table aria-label="Tripped breakers">
        <thead><tr><th>Scope</th><th>Reason</th><th>Tripped</th><th>Reset at</th><th>Actions</th></tr></thead>
        <tbody>{breakers.map((breaker) => (
          <tr key={`${breaker.scope}-${breaker.tripped_at}`}>
            <td><code>{breaker.scope}</code></td>
            <td>{breaker.reason}</td>
            <td>{timeValue(breaker.tripped_at)}</td>
            <td>{timeValue(breaker.reset_at)}</td>
            <td><button type="button" className="danger-button" title={`Reset ${breaker.scope} breaker`} aria-label={`Reset ${breaker.scope} breaker`} disabled={!canReset || busyScope === breaker.scope} onClick={() => onReset(breaker.scope)}>{busyScope === breaker.scope ? 'Working…' : 'Reset'}</button></td>
          </tr>
        ))}</tbody>
      </table>
    </div>
  )
}

type MarketRow = {
  marketType: MarketType
  active: boolean
  reason?: string
  exposure?: RiskCockpitExposure
}

function buildMarketRows(status: RiskEngineStatus, cockpit?: RiskCockpitSummary, filter?: string): MarketRow[] {
  const names = new Set<string>(marketTypes)
  for (const market of Object.keys(status.market_kill_switches ?? {})) names.add(market)
  for (const exposure of cockpit?.exposures ?? []) names.add(exposure.market_type)
  const rows = Array.from(names).sort().map((marketType) => ({
    marketType: marketType as MarketType,
    active: Boolean(status.market_kill_switches?.[marketType]?.active),
    reason: status.market_kill_switches?.[marketType]?.reason,
    exposure: cockpit?.exposures.find((item) => item.market_type === marketType),
  }))
  return filter ? rows.filter((row) => row.marketType === filter) : rows
}

function MarketStopRows({ rows, canUseControls, busyMarket, onStop, onResume }: {
  rows: MarketRow[]
  canUseControls: boolean
  busyMarket?: string
  onStop: (marketType: MarketType) => void
  onResume: (marketType: MarketType) => void
}) {
  if (rows.length === 0) return <EmptyState title="No market controls" message="No markets match the selected filter." />
  return (
    <div className="table-wrap">
      <table aria-label="Per-market kill switch controls">
        <thead><tr><th>Market</th><th>Status</th><th>Reason</th><th>Open positions</th><th>Gross exposure</th><th>Actions</th></tr></thead>
        <tbody>{rows.map((row) => {
          const busy = busyMarket === row.marketType
          return (
            <tr key={row.marketType}>
              <td><StatusPill value={row.marketType} known={[...marketTypes]} /></td>
              <td><span className={`status-pill ${row.active ? 'warning' : 'normal'}`}>{row.active ? 'Stopped' : 'Open'}</span></td>
              <td>{row.reason ?? '—'}</td>
              <td>{row.exposure?.open_positions ?? '—'}</td>
              <td>{row.exposure ? numberValue(row.exposure.gross_exposure) : '—'}</td>
              <td>
                <div className="action-row compact-actions">
                  <button type="button" className="danger-button" disabled={!canUseControls || row.active || busy} onClick={() => onStop(row.marketType)}>{busy ? 'Working…' : `Stop ${displayEnum(row.marketType)} market`}</button>
                  <button type="button" className="secondary-button" disabled={!canUseControls || !row.active || busy} onClick={() => onResume(row.marketType)}>{busy ? 'Working…' : `Resume ${displayEnum(row.marketType)} market`}</button>
                </div>
              </td>
            </tr>
          )
        })}</tbody>
      </table>
    </div>
  )
}

type KillSwitchAction = 'activate' | 'deactivate'
type MarketStopAction = 'stop' | 'resume'
type MarketDialogAction = { action: MarketStopAction; marketType: MarketType } | null
type BreakerResetDialog = { scope: string } | null
type DialogError = { message: string; unknownCompletion?: boolean } | null

function killSwitchErrorMessage(error: unknown, action: KillSwitchAction): DialogError {
  const label = action === 'activate' ? 'activation' : 'deactivation'
  if (!isApiClientError(error)) {
    if (error instanceof Error) return { message: error.message }
    return { message: `Kill switch ${label} failed. Completion is unknown; verify risk status before retrying.`, unknownCompletion: true }
  }
  switch (error.kind) {
    case 'unauthorized':
      return { message: action === 'deactivate' ? 'Admin key was rejected or your session is unauthorized.' : 'Your session is unauthorized. Sign in again before changing the kill switch.' }
    case 'validation':
    case 'bad_request':
      return { message: 'The server rejected the reason or request body. Review the fields and try again.' }
    case 'not_implemented':
      return { message: 'Kill switch controls are not configured on this server.' }
    case 'rate_limited':
      return { message: 'Kill switch controls are rate limited. Wait before trying again.' }
    case 'network':
      return { message: `Network failed during kill switch ${label}. Completion is unknown; verify status before retrying.`, unknownCompletion: true }
    case 'server':
      return { message: `Server could not safely complete kill switch ${label}. Verify status before retrying.` }
    default:
      return { message: `Kill switch ${label} did not complete. Verify risk status before retrying.` }
  }
}

function marketStopErrorMessage(error: unknown, action: MarketStopAction): DialogError {
  const label = action === 'stop' ? 'stop' : 'resume'
  if (!isApiClientError(error)) {
    if (error instanceof Error) return { message: error.message }
    return { message: `Market ${label} did not complete. Completion is unknown; verify risk status before retrying.`, unknownCompletion: true }
  }
  switch (error.kind) {
    case 'unauthorized':
      return { message: 'Your session is unauthorized. Sign in again before changing market controls.' }
    case 'validation':
    case 'bad_request':
      return { message: action === 'stop' ? 'The server rejected the market stop reason or market type.' : 'The server rejected the market resume request.' }
    case 'not_implemented':
      return { message: 'Per-market risk controls are not configured on this server.' }
    case 'rate_limited':
      return { message: 'Per-market risk controls are rate limited. Wait before trying again.' }
    case 'network':
      return { message: `Network failed during market ${label}. Completion is unknown; verify status before retrying.`, unknownCompletion: true }
    case 'server':
      return { message: `Server could not safely complete market ${label}. Verify status before retrying.` }
    default:
      return { message: `Market ${label} did not complete. Verify risk status before retrying.` }
  }
}

function breakerResetErrorMessage(error: unknown): DialogError {
  if (!isApiClientError(error)) {
    if (error instanceof Error) return { message: error.message }
    return { message: 'Breaker reset did not complete. Completion is unknown; verify breaker and risk status before retrying.', unknownCompletion: true }
  }
  switch (error.kind) {
    case 'unauthorized':
      return { message: 'Admin key was rejected or your session is unauthorized.' }
    case 'validation':
    case 'bad_request':
      return { message: 'The server rejected the breaker reset scope.' }
    case 'not_implemented':
      return { message: 'Breaker reset is not configured on this server.' }
    case 'rate_limited':
      return { message: 'Breaker reset is rate limited. Wait before trying again.' }
    case 'network':
      return { message: 'Network failed during breaker reset. Completion is unknown; verify status before retrying.', unknownCompletion: true }
    case 'server':
      return { message: 'Server could not safely complete breaker reset. Verify status before retrying.' }
    default:
      return { message: 'Breaker reset did not complete. Verify breaker and risk status before retrying.' }
  }
}

export function RiskPage() {
  const realtime = useRealtime()
  const queryClient = useQueryClient()
  const [searchParams, setSearchParams] = useSearchParams()
  const [realtimeStale, setRealtimeStale] = useState(false)
  const [dialogAction, setDialogAction] = useState<KillSwitchAction | null>(null)
  const [marketDialogAction, setMarketDialogAction] = useState<MarketDialogAction>(null)
  const [breakerDialog, setBreakerDialog] = useState<BreakerResetDialog>(null)
  const [reason, setReason] = useState('')
  const [marketReason, setMarketReason] = useState('')
  const [adminKey, setAdminKey] = useState('')
  const [breakerAdminKey, setBreakerAdminKey] = useState('')
  const [dialogError, setDialogError] = useState<DialogError>(null)
  const [marketDialogError, setMarketDialogError] = useState<DialogError>(null)
  const [breakerDialogError, setBreakerDialogError] = useState<DialogError>(null)
  const [verifiedMessage, setVerifiedMessage] = useState<string | null>(null)
  const [verifying, setVerifying] = useState(false)
  const [marketVerifying, setMarketVerifying] = useState(false)
  const [breakerVerifying, setBreakerVerifying] = useState(false)
  const statusQuery = useQuery({ queryKey: queryKeys.riskStatus, queryFn: ({ signal }) => getRiskStatus(signal) })
  const cockpitQuery = useQuery({ queryKey: queryKeys.riskCockpit, queryFn: ({ signal }) => getRiskCockpit(signal) })
  const breakersQuery = useQuery({ queryKey: queryKeys.riskBreakers, queryFn: ({ signal }) => getRiskBreakers(signal) })
  const allocatorDiagnosticsQuery = useQuery({ queryKey: queryKeys.allocatorDiagnostics, queryFn: ({ signal }) => getAllocatorDiagnostics(signal) })

  useEffect(() => {
    const latest = realtime.events[0]
    if (!latest) return
    if (latest.type === 'circuit_breaker' || latest.type === 'position_update' || latest.type === 'order_filled') {
      setRealtimeStale(true)
      void statusQuery.refetch()
      void cockpitQuery.refetch()
      void breakersQuery.refetch()
    }
  }, [breakersQuery, cockpitQuery, realtime.events, statusQuery])

  const killSwitchActive = Boolean(statusQuery.data?.kill_switch.active)
  const mutation = useMutation({
    mutationFn: async (action: KillSwitchAction) => {
      const trimmedReason = reason.trim()
      if (!trimmedReason) throw new Error('Reason is required.')
      if (realtimeStale || realtime.status === 'disconnected' || realtime.status === 'degraded') throw new Error('Risk data is stale or realtime is degraded. Refresh before changing the kill switch.')
      if (action === 'deactivate' && !adminKey.trim()) throw new Error('Admin key is required to deactivate the global kill switch.')
      if (isAccessTokenExpiringSoon()) await refreshAccessToken()
      return toggleKillSwitch({ active: action === 'activate', reason: trimmedReason }, action === 'deactivate' ? adminKey.trim() : undefined)
    },
    retry: false,
    onSuccess: async (_response, action) => {
      setVerifying(true)
      setDialogError(null)
      try {
        await queryClient.invalidateQueries({ queryKey: queryKeys.riskStatus })
        await queryClient.invalidateQueries({ queryKey: queryKeys.riskCockpit })
        await queryClient.invalidateQueries({ queryKey: queryKeys.riskBreakers })
        const confirmed = await queryClient.fetchQuery({ queryKey: queryKeys.riskStatus, queryFn: ({ signal }) => getRiskStatus(signal), retry: false })
        const expected = action === 'activate'
        if (confirmed.kill_switch.active !== expected) {
          setDialogError({ message: `Kill switch ${action} was accepted, but verified status is still ${confirmed.kill_switch.active ? 'active' : 'inactive'}. Do not assume completion until operators review backend status.`, unknownCompletion: true })
          return
        }
        setVerifiedMessage(`Verified kill switch ${confirmed.kill_switch.active ? 'active' : 'inactive'} at ${new Date(confirmed.updated_at).toLocaleString()}.`)
        setDialogAction(null)
        setReason('')
        setAdminKey('')
      } catch {
        setDialogError({ message: `Kill switch ${action} was accepted, but risk status verification failed. Completion is unknown until /risk/status is confirmed.`, unknownCompletion: true })
      } finally {
        setVerifying(false)
      }
    },
    onError: (error, action) => setDialogError(killSwitchErrorMessage(error, action)),
  })

  const marketMutation = useMutation({
    mutationFn: async ({ action, marketType }: { action: MarketStopAction; marketType: MarketType }) => {
      if (realtimeStale || realtime.status === 'disconnected' || realtime.status === 'degraded') throw new Error('Risk data is stale or realtime is degraded. Refresh before changing market controls.')
      if (action === 'stop') {
        const trimmedReason = marketReason.trim()
        if (!trimmedReason) throw new Error('Reason is required.')
        if (isAccessTokenExpiringSoon()) await refreshAccessToken()
        return stopMarketKillSwitch(marketType, { reason: trimmedReason })
      }
      if (isAccessTokenExpiringSoon()) await refreshAccessToken()
      return resumeMarketKillSwitch(marketType)
    },
    retry: false,
    onSuccess: async (_response, variables) => {
      setMarketVerifying(true)
      setMarketDialogError(null)
      try {
        await queryClient.invalidateQueries({ queryKey: queryKeys.riskStatus })
        await queryClient.invalidateQueries({ queryKey: queryKeys.riskCockpit })
        await queryClient.invalidateQueries({ queryKey: queryKeys.riskBreakers })
        await queryClient.invalidateQueries({ queryKey: queryKeys.strategies })
        const confirmed = await queryClient.fetchQuery({ queryKey: queryKeys.riskStatus, queryFn: ({ signal }) => getRiskStatus(signal), retry: false })
        const actual = confirmed.market_kill_switches?.[variables.marketType]?.active ?? false
        const expected = variables.action === 'stop'
        if (actual !== expected) {
          setMarketDialogError({ message: `Market ${variables.action} was accepted, but verified ${displayEnum(variables.marketType)} status is still ${actual ? 'stopped' : 'open'}. Do not assume completion until operators review backend status.`, unknownCompletion: true })
          return
        }
        setVerifiedMessage(`Verified ${displayEnum(variables.marketType)} market ${actual ? 'stopped' : 'open'} at ${new Date(confirmed.updated_at).toLocaleString()}.`)
        setMarketDialogAction(null)
        setMarketReason('')
      } catch {
        setMarketDialogError({ message: `Market ${variables.action} was accepted, but risk status verification failed. Completion is unknown until /risk/status is confirmed.`, unknownCompletion: true })
      } finally {
        setMarketVerifying(false)
      }
    },
    onError: (error, variables) => setMarketDialogError(marketStopErrorMessage(error, variables.action)),
  })

  const breakerMutation = useMutation({
    mutationFn: async ({ scope }: { scope: string }) => {
      const trimmedScope = scope.trim()
      if (!trimmedScope) throw new Error('Breaker scope is required.')
      if (!breakerAdminKey.trim()) throw new Error('Admin key is required to reset a breaker.')
      if (realtimeStale || realtime.status === 'disconnected' || realtime.status === 'degraded') throw new Error('Risk data is stale or realtime is degraded. Refresh before resetting breakers.')
      if (isAccessTokenExpiringSoon()) await refreshAccessToken()
      return resetRiskBreaker({ scope: trimmedScope }, breakerAdminKey.trim())
    },
    retry: false,
    onSuccess: async (response) => {
      setBreakerVerifying(true)
      setBreakerDialogError(null)
      try {
        await queryClient.invalidateQueries({ queryKey: queryKeys.riskStatus })
        await queryClient.invalidateQueries({ queryKey: queryKeys.riskCockpit })
        await queryClient.invalidateQueries({ queryKey: queryKeys.riskBreakers })
        const confirmedBreakers = await queryClient.fetchQuery({ queryKey: queryKeys.riskBreakers, queryFn: ({ signal }) => getRiskBreakers(signal), retry: false })
        await queryClient.fetchQuery({ queryKey: queryKeys.riskStatus, queryFn: ({ signal }) => getRiskStatus(signal), retry: false })
        await queryClient.fetchQuery({ queryKey: queryKeys.riskCockpit, queryFn: ({ signal }) => getRiskCockpit(signal), retry: false })
        if (confirmedBreakers.tripped.some((breaker) => breaker.scope === response.scope && !breaker.reset_at)) {
          setBreakerDialogError({ message: `Breaker reset was accepted, but verified scope ${response.scope} is still tripped. Do not assume completion until operators review backend status.`, unknownCompletion: true })
          return
        }
        setVerifiedMessage(`Verified breaker ${response.scope} reset at ${new Date().toLocaleString()}.`)
        setBreakerDialog(null)
      } catch {
        setBreakerDialogError({ message: 'Breaker reset was accepted, but breaker/risk verification failed. Completion is unknown until `/risk/breakers` and `/risk/status` are confirmed.', unknownCompletion: true })
      } finally {
        setBreakerVerifying(false)
      }
    },
    onError: (error) => setBreakerDialogError(breakerResetErrorMessage(error)),
    onSettled: () => setBreakerAdminKey(''),
  })

  const busy = mutation.isPending || verifying
  const marketBusy = marketMutation.isPending || marketVerifying
  const breakerBusy = breakerMutation.isPending || breakerVerifying
  const canUseKillSwitch = Boolean(statusQuery.data && !busy && !realtimeStale && realtime.status !== 'disconnected' && realtime.status !== 'degraded')
  const canUseMarketControls = Boolean(statusQuery.data && !marketBusy && !realtimeStale && realtime.status !== 'disconnected' && realtime.status !== 'degraded')
  const canResetBreakers = Boolean(breakersQuery.data && !breakerBusy && !realtimeStale && realtime.status !== 'disconnected' && realtime.status !== 'degraded')
  const activeDialogTitle = dialogAction === 'deactivate' ? 'Deactivate global kill switch?' : 'Activate global kill switch?'
  const activeDialogConfirm = dialogAction === 'deactivate' ? 'Deactivate kill switch' : 'Activate kill switch'
  const marketTypeFilter = searchParams.get('market_type') ?? ''
  const marketRows = statusQuery.data ? buildMarketRows(statusQuery.data, cockpitQuery.data, marketTypeFilter) : []
  const activeMarketDialogTitle = marketDialogAction?.action === 'resume' ? `Resume ${displayEnum(marketDialogAction.marketType)} market?` : `Stop ${marketDialogAction ? displayEnum(marketDialogAction.marketType) : 'market'} market?`
  const activeMarketDialogConfirm = marketDialogAction?.action === 'resume' ? 'Resume market' : 'Stop market'

  return (
    <div className="detail-stack">
      <nav className="breadcrumbs" aria-label="Breadcrumbs"><Link to="/cockpit">Cockpit</Link><span aria-hidden="true">/</span><span>Risk</span></nav>
      <section className="panel hero-panel">
        <p className="eyebrow">Safety console</p>
        <div className="panel-header"><div><h1>Risk</h1><p className="muted">Inspect global risk, circuit breakers, kill switch state, market exposure, persisted breakers, and approved risk controls. This slice adds only breaker reset on top of existing controls.</p></div><span className="status-pill active">Risk console</span></div>
        <StaleBanner show={realtimeStale || realtime.status === 'disconnected' || realtime.status === 'degraded'} message="Risk console data is read-only and may be stale after realtime risk or execution activity." />
        {verifiedMessage ? <p role="status" className="success-box">{verifiedMessage}</p> : null}
      </section>

      <section className="panel" aria-labelledby="risk-status-heading">
        <div className="panel-header"><div><h2 id="risk-status-heading">Risk engine status</h2><p className="muted">Current engine projection, kill switch, circuit breaker, and position limits.</p></div>{statusQuery.data ? <LastUpdated date={statusQuery.dataUpdatedAt} /> : null}</div>
        {statusQuery.isLoading ? <LoadingState label="Loading risk status…" /> : null}
        {statusQuery.error ? <ErrorState error={statusQuery.error} onRetry={() => void statusQuery.refetch()} /> : null}
        {statusQuery.data ? <RiskStatusPanel status={statusQuery.data} /> : null}
      </section>

      <section className="panel" aria-labelledby="kill-switch-actions-heading">
        <div className="panel-header">
          <div>
            <h2 id="kill-switch-actions-heading">Global kill switch controls</h2>
            <p className="muted">Activation and deactivation require confirmation, a reason, no optimistic UI, and verified `/risk/status` after submit.</p>
          </div>
          <span className={`status-pill ${killSwitchActive ? 'warning' : 'normal'}`}>{killSwitchActive ? 'Active' : 'Inactive'}</span>
        </div>
        {statusQuery.data ? (
          <div className="action-row">
            <button type="button" className="danger-button" disabled={!canUseKillSwitch || killSwitchActive} onClick={() => { setDialogError(null); setDialogAction('activate') }}>Activate global kill switch</button>
            <button type="button" className="secondary-button" disabled={!canUseKillSwitch || !killSwitchActive} onClick={() => { setDialogError(null); setDialogAction('deactivate') }}>Deactivate global kill switch</button>
          </div>
        ) : <p className="muted">Load risk status before using kill switch controls.</p>}
      </section>

      <section className="panel" aria-labelledby="market-controls-heading">
        <div className="panel-header">
          <div>
            <h2 id="market-controls-heading">Per-market stop and resume</h2>
            <p className="muted">Stop is C2 with a required reason. Resume is C3/live-gated by policy, but the backend currently exposes no RBAC or resume reason contract; the UI still verifies `/risk/status` before reporting completion.</p>
          </div>
          {statusQuery.data ? <LastUpdated date={statusQuery.dataUpdatedAt} /> : null}
        </div>
        <form className="filter-bar filter-bar-single" aria-label="Market filter" onSubmit={(event) => event.preventDefault()}>
          <label>
            Market filter
            <select value={marketTypeFilter} onChange={(event) => {
              const next = new URLSearchParams(searchParams)
              if (event.target.value) next.set('market_type', event.target.value)
              else next.delete('market_type')
              setSearchParams(next)
            }}>
              <option value="">All markets</option>
              {marketTypes.map((marketType) => <option key={marketType} value={marketType}>{displayEnum(marketType)}</option>)}
            </select>
          </label>
        </form>
        {statusQuery.data ? (
          <MarketStopRows
            rows={marketRows}
            canUseControls={canUseMarketControls}
            busyMarket={marketDialogAction && marketBusy ? marketDialogAction.marketType : undefined}
            onStop={(marketType) => { setMarketDialogError(null); setMarketReason(''); setMarketDialogAction({ action: 'stop', marketType }) }}
            onResume={(marketType) => { setMarketDialogError(null); setMarketReason(''); setMarketDialogAction({ action: 'resume', marketType }) }}
          />
        ) : <p className="muted">Load risk status before using per-market controls.</p>}
      </section>

      <section className="panel" aria-labelledby="risk-cockpit-heading">
        <div className="panel-header"><div><h2 id="risk-cockpit-heading">Cockpit exposure</h2><p className="muted">Aggregated market exposure and decision counts. Feature unavailable means backend dependencies are not configured.</p></div>{cockpitQuery.data ? <LastUpdated date={cockpitQuery.dataUpdatedAt} /> : null}</div>
        {cockpitQuery.isLoading ? <LoadingState label="Loading risk cockpit…" /> : null}
        {cockpitQuery.error ? <ErrorState error={cockpitQuery.error} onRetry={() => void cockpitQuery.refetch()} /> : null}
        {cockpitQuery.data ? <>
          <div className="metrics-grid">
            <div><span className="muted">Generated</span><strong>{timeValue(cockpitQuery.data.generated_at)}</strong></div>
            <div><span className="muted">Kill switch</span><strong>{cockpitQuery.data.kill_switch_active ? 'Active' : 'Inactive'}</strong></div>
            <div><span className="muted">Circuit breaker</span><strong>{cockpitQuery.data.circuit_breaker ? 'Tripped' : 'Open'}</strong></div>
          </div>
          {cockpitQuery.data.warnings.length > 0 ? <div role="alert" className="inline-alert warning">{cockpitQuery.data.warnings.join(', ')}</div> : null}
          <ExposureRows exposures={cockpitQuery.data.exposures} />
        </> : null}
      </section>

      <section className="panel" aria-labelledby="allocator-diagnostics-heading">
        <div className="panel-header"><div><h2 id="allocator-diagnostics-heading">Allocator diagnostics</h2><p className="muted">Read-only allocator health and exposure guidance. This surface adds visibility into buying power and market allocation pressure.</p></div>{allocatorDiagnosticsQuery.data ? <LastUpdated date={allocatorDiagnosticsQuery.dataUpdatedAt} /> : null}</div>
        {allocatorDiagnosticsQuery.isLoading ? <LoadingState label="Loading allocator diagnostics…" /> : null}
        {allocatorDiagnosticsQuery.error ? <ErrorState error={allocatorDiagnosticsQuery.error} onRetry={() => void allocatorDiagnosticsQuery.refetch()} /> : null}
        {allocatorDiagnosticsQuery.data ? <AllocatorDiagnosticsPanel diagnostics={allocatorDiagnosticsQuery.data} /> : null}
      </section>

      <section className="panel" aria-labelledby="breakers-heading">
        <div className="panel-header"><div><h2 id="breakers-heading">Tripped breakers</h2><p className="muted">Reset requires a one-shot admin key, no optimistic UI, key clearing on every outcome, and verified breaker/risk refetch.</p></div>{breakersQuery.data ? <LastUpdated date={breakersQuery.dataUpdatedAt} /> : null}</div>
        {breakersQuery.isLoading ? <LoadingState label="Loading breakers…" /> : null}
        {breakersQuery.error ? <ErrorState error={breakersQuery.error} onRetry={() => void breakersQuery.refetch()} /> : null}
        {breakersQuery.data ? <BreakerRows breakers={breakersQuery.data.tripped} canReset={canResetBreakers} busyScope={breakerDialog && breakerBusy ? breakerDialog.scope : undefined} onReset={(scope) => { setBreakerDialogError(null); setBreakerAdminKey(''); setBreakerDialog({ scope }) }} /> : null}
      </section>

      <ConfirmationDialog
        open={Boolean(dialogAction)}
        title={activeDialogTitle}
        confirmLabel={activeDialogConfirm}
        busy={busy}
        disableDismiss={busy}
        error={dialogError ? <>{dialogError.message}{dialogError.unknownCompletion ? <strong> Do not retry until risk status is verified.</strong> : null}</> : null}
        onCancel={() => { if (!busy) setDialogAction(null) }}
        onConfirm={() => { if (!busy && dialogAction) mutation.mutate(dialogAction) }}
      >
        <p><strong>Global control.</strong> This changes the API-toggle kill switch only. The UI will verify the final status via <code>/risk/status</code> before showing completion.</p>
        <label>
          Reason
          <textarea value={reason} onChange={(event) => setReason(event.target.value)} placeholder={dialogAction === 'deactivate' ? 'Why is it safe to deactivate now?' : 'Why is trading being halted?'} />
        </label>
        {dialogAction === 'deactivate' ? (
          <label>
            Admin key
            <input type="password" value={adminKey} onChange={(event) => setAdminKey(event.target.value)} placeholder="One-time admin key" />
          </label>
        ) : null}
      </ConfirmationDialog>

      <ConfirmationDialog
        open={Boolean(marketDialogAction)}
        title={activeMarketDialogTitle}
        confirmLabel={activeMarketDialogConfirm}
        busy={marketBusy}
        disableDismiss={marketBusy}
        error={marketDialogError ? <>{marketDialogError.message}{marketDialogError.unknownCompletion ? <strong> Do not retry until risk status is verified.</strong> : null}</> : null}
        onCancel={() => { if (!marketBusy) setMarketDialogAction(null) }}
        onConfirm={() => { if (!marketBusy && marketDialogAction) marketMutation.mutate(marketDialogAction) }}
      >
        <p><strong>Market control.</strong> This changes only the selected market kill switch. The UI will verify the final state via <code>/risk/status</code> before showing completion.</p>
        {marketDialogAction?.action === 'stop' ? (
          <label>
            Reason
            <textarea value={marketReason} onChange={(event) => setMarketReason(event.target.value)} placeholder="Why is this market being stopped?" />
          </label>
        ) : (
          <p className="inline-alert warning">Resume removes a market-level safety block. Backend RBAC and resume reason support are unresolved, so verify operator authorization outside the UI before proceeding.</p>
        )}
      </ConfirmationDialog>

      <ConfirmationDialog
        open={Boolean(breakerDialog)}
        title={breakerDialog ? `Reset ${breakerDialog.scope} breaker?` : 'Reset breaker?'}
        confirmLabel="Reset breaker"
        busy={breakerBusy}
        disableDismiss={breakerBusy}
        error={breakerDialogError ? <>{breakerDialogError.message}{breakerDialogError.unknownCompletion ? <strong> Do not retry until breaker and risk status are verified.</strong> : null}</> : null}
        onCancel={() => { if (!breakerBusy) { setBreakerDialog(null); setBreakerAdminKey('') } }}
        onConfirm={() => { if (!breakerBusy && breakerDialog) breakerMutation.mutate(breakerDialog) }}
      >
        <p><strong>Administrative reset.</strong> This is an L4/C4 action. The admin key is sent once as <code>X-Admin-Key</code>, never stored in app state after completion, and the UI verifies <code>/risk/breakers</code>, <code>/risk/status</code>, and <code>/risk/cockpit</code> before reporting completion.</p>
        <p className="inline-alert warning">Backend RBAC/step-up authorization and audit visibility remain unresolved; confirm operator authorization outside the UI before proceeding.</p>
        <label>
          Admin key
          <input type="password" autoComplete="off" value={breakerAdminKey} onChange={(event) => setBreakerAdminKey(event.target.value)} placeholder="One-time admin key" />
        </label>
      </ConfirmationDialog>
    </div>
  )
}
