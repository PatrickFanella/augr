import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'

import { PageHeader } from '@/components/layout/page-header'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { apiClient } from '@/lib/api/client'
import type { AuditLogEntry, CircuitBreakerPhase, EngineStatus } from '@/lib/api/types'

const circuitBreakerBadge: Record<CircuitBreakerPhase, { label: string; variant: 'success' | 'destructive' | 'warning' }> = {
  open: { label: 'Open', variant: 'success' },
  tripped: { label: 'Tripped', variant: 'destructive' },
  cooldown: { label: 'Cooldown', variant: 'warning' },
}

function truncateDetails(details?: unknown) {
  if (details == null) {
    return '—'
  }

  const raw = typeof details === 'string' ? details : JSON.stringify(details)
  if (raw.length <= 80) {
    return raw
  }

  return `${raw.slice(0, 77)}...`
}

function formatUtilizationValue(value: number) {
  return Number.isInteger(value) ? String(value) : value.toFixed(1)
}

function percentDisplayValue(value: number, usesFraction: boolean) {
  return usesFraction ? value * 100 : value
}


function utilizationBarClass(ratio: number) {
  if (ratio > 0.9) return 'bg-red-500'
  if (ratio >= 0.7) return 'bg-amber-500'
  return 'bg-emerald-500'
}

function UtilizationRow({
  label,
  current,
  max,
  suffix = '',
  testId,
}: {
  label: string
  current: number
  max: number
  suffix?: string
  testId: string
}) {
  const ratio = max > 0 ? current / max : 0
  const width = Math.max(0, Math.min(ratio * 100, 100))

  return (
    <div className="space-y-2">
      <div className="flex items-center justify-between gap-4 text-sm">
        <span className="font-medium">{label}</span>
        <span className="text-muted-foreground">
          {formatUtilizationValue(current)}{suffix} / {formatUtilizationValue(max)}{suffix}
        </span>
      </div>
      <div className="h-3 overflow-hidden rounded-full bg-muted">
        <div
          data-testid={testId}
          className={`h-full rounded-full transition-all ${utilizationBarClass(ratio)}`}
          style={{ width: `${width}%` }}
        />
      </div>
    </div>
  )
}


export function RiskPage() {
  const queryClient = useQueryClient()
  const [reason, setReason] = useState('')
  const [showReasonInput, setShowReasonInput] = useState(false)
  const [auditLimit, setAuditLimit] = useState(10)

  const { data, isLoading, isError, error } = useQuery<EngineStatus>({
    queryKey: ['riskStatus'],
    queryFn: () => apiClient.getRiskStatus(),
  })

  const {
    data: auditData,
    isLoading: auditLoading,
    isError: auditError,
  } = useQuery({
    queryKey: ['auditLog', auditLimit],
    queryFn: () => apiClient.listAuditLog({ limit: auditLimit }),
  })

  const toggleMutation = useMutation({
    mutationFn: (params: { active: boolean; reason?: string }) =>
      apiClient.toggleKillSwitch(params),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['riskStatus'] })
      queryClient.invalidateQueries({ queryKey: ['auditLog'] })
      setReason('')
      setShowReasonInput(false)
    },
  })

  const killSwitch = data?.kill_switch
  const circuitBreaker = data?.circuit_breaker
  const auditEntries = auditData?.data ?? []

  function handleToggle() {
    if (!killSwitch) return

    if (!killSwitch.active && !showReasonInput) {
      setShowReasonInput(true)
      return
    }

    toggleMutation.mutate({
      active: !killSwitch.active,
      reason: killSwitch.active ? undefined : reason || undefined,
    })
  }

  return (
    <div className="space-y-4" data-testid="risk-page">
      <PageHeader
        eyebrow="Controls"
        title="Risk engine"
        description="Review circuit breaker state, kill switch controls, exposure utilization, and recent audit events."
      />

      <div className="grid gap-4 xl:grid-cols-[minmax(0,1fr)_minmax(0,1fr)]">
        <Card>
          <CardHeader>
            <CardTitle>Circuit Breaker</CardTitle>
            <CardDescription>Current state of the circuit breaker</CardDescription>
          </CardHeader>
          <CardContent>
            {isLoading && <div data-testid="circuit-breaker-loading" className="h-20 animate-pulse rounded bg-muted" />}
            {isError && <p className="text-sm text-destructive">Failed to load: {(error as Error).message}</p>}
            {circuitBreaker && (
              <div className="space-y-3">
                <div className="flex items-center gap-2">
                  <span className="text-sm font-medium">State:</span>
                  <Badge variant={circuitBreakerBadge[circuitBreaker.state].variant}>
                    {circuitBreakerBadge[circuitBreaker.state].label}
                  </Badge>
                </div>
                {circuitBreaker.reason && (
                  <p className="text-sm text-muted-foreground">
                    <span className="font-medium">Reason:</span> {circuitBreaker.reason}
                  </p>
                )}
                {circuitBreaker.tripped_at && (
                  <p className="text-sm text-muted-foreground">
                    <span className="font-medium">Tripped at:</span>{' '}
                    {new Date(circuitBreaker.tripped_at).toLocaleString()}
                  </p>
                )}
                {circuitBreaker.cooldown_end && (
                  <p className="text-sm text-muted-foreground">
                    <span className="font-medium">Cooldown ends:</span>{' '}
                    {new Date(circuitBreaker.cooldown_end).toLocaleString()}
                  </p>
                )}
              </div>
            )}
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle>Kill Switch</CardTitle>
            <CardDescription>Emergency trading halt</CardDescription>
          </CardHeader>
          <CardContent>
            {isLoading && <div data-testid="kill-switch-loading" className="h-20 animate-pulse rounded bg-muted" />}
            {isError && <p className="text-sm text-destructive">Failed to load: {(error as Error).message}</p>}
            {killSwitch && (
              <div className="space-y-4">
                <div className="flex items-center gap-2">
                  <span className="text-sm font-medium">State:</span>
                  <Badge variant={killSwitch.active ? 'destructive' : 'success'}>
                    {killSwitch.active ? 'Active' : 'Inactive'}
                  </Badge>
                </div>
                {killSwitch.active && killSwitch.reason && (
                  <p className="text-sm text-muted-foreground">
                    <span className="font-medium">Reason:</span> {killSwitch.reason}
                  </p>
                )}
                {killSwitch.active && killSwitch.activated_at && (
                  <p className="text-sm text-muted-foreground">
                    <span className="font-medium">Activated at:</span>{' '}
                    {new Date(killSwitch.activated_at).toLocaleString()}
                  </p>
                )}

                {showReasonInput && !killSwitch.active ? (
                  <div className="space-y-2">
                    <Label htmlFor="kill-reason">Reason for activation</Label>
                    <Input
                      id="kill-reason"
                      placeholder="Enter reason..."
                      value={reason}
                      onChange={(e) => setReason(e.target.value)}
                    />
                    <div className="flex gap-2">
                      <Button
                        variant="default"
                        size="sm"
                        disabled={!reason.trim() || toggleMutation.isPending}
                        onClick={handleToggle}
                      >
                        {toggleMutation.isPending ? 'Activating...' : 'Confirm Activate'}
                      </Button>
                      <Button
                        variant="outline"
                        size="sm"
                        onClick={() => {
                          setShowReasonInput(false)
                          setReason('')
                        }}
                      >
                        Cancel
                      </Button>
                    </div>
                  </div>
                ) : null}

                {!showReasonInput ? (
                  <Button
                    variant={killSwitch.active ? 'outline' : 'default'}
                    disabled={toggleMutation.isPending}
                    onClick={handleToggle}
                    data-testid="kill-switch-toggle"
                  >
                    {toggleMutation.isPending
                      ? 'Processing...'
                      : killSwitch.active
                        ? 'Deactivate'
                        : 'Activate'}
                  </Button>
                ) : null}
              </div>
            )}
          </CardContent>
        </Card>
      </div>

      {data ? (
        <Card>
          <CardHeader>
            <CardTitle>Position limit utilization</CardTitle>
            <CardDescription>Current usage against configured open position and exposure caps</CardDescription>
          </CardHeader>
          <CardContent className="space-y-4">
            <UtilizationRow
              label="Open positions"
              current={data.position_limits.current_open_positions ?? 0}
              max={data.position_limits.max_concurrent}
              testId="risk-utilization-open-positions"
            />
            <UtilizationRow
              label="Total exposure"
              current={percentDisplayValue(
                data.position_limits.current_total_exposure_pct ?? 0,
                data.position_limits.max_total_pct <= 1,
              )}
              max={percentDisplayValue(data.position_limits.max_total_pct, data.position_limits.max_total_pct <= 1)}
              suffix="%"
              testId="risk-utilization-total-exposure"
            />
          </CardContent>
        </Card>
      ) : null}


      <Card>
        <CardHeader>
          <CardTitle>Audit Log</CardTitle>
          <CardDescription>Recent system events</CardDescription>
        </CardHeader>
        <CardContent>
          {auditLoading ? (
            <div data-testid="audit-log-loading" className="space-y-2">
              {Array.from({ length: 3 }).map((_, index) => (
                <div key={index} className="h-8 animate-pulse rounded bg-muted" />
              ))}
            </div>
          ) : auditError ? (
            <p className="text-sm text-muted-foreground">Unable to load audit log.</p>
          ) : !auditEntries.length ? (
            <p className="text-sm text-muted-foreground">No audit log entries.</p>
          ) : (
            <div className="space-y-2">
              <div className="max-h-96 overflow-y-auto">
                <table className="w-full text-sm" data-testid="audit-log-table">
                  <thead>
                    <tr className="border-b border-white/8 text-left font-mono text-[11px] uppercase tracking-[0.16em] text-muted-foreground">
                      <th className="pb-2 font-medium">Event</th>
                      <th className="pb-2 font-medium">Entity</th>
                      <th className="pb-2 font-medium">Details</th>
                      <th className="pb-2 font-medium">Time</th>
                    </tr>
                  </thead>
                  <tbody>
                    {auditEntries.map((entry: AuditLogEntry) => (
                      <tr key={entry.id} className="border-b border-white/6 last:border-0">
                        <td className="py-2">
                          <Badge variant="outline">{entry.event_type}</Badge>
                        </td>
                        <td className="py-2 text-muted-foreground">{entry.entity_type ?? '—'}</td>
                        <td className="max-w-xs py-2 text-muted-foreground" title={typeof entry.details === 'string' ? entry.details : JSON.stringify(entry.details)}>
                          {truncateDetails(entry.details)}
                        </td>
                        <td className="py-2 text-muted-foreground">{new Date(entry.created_at).toLocaleString()}</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
              {auditEntries.length >= auditLimit ? (
                <Button
                  variant="outline"
                  size="sm"
                  onClick={() => setAuditLimit((prev) => prev + 10)}
                  data-testid="load-more-audit"
                >
                  Load more
                </Button>
              ) : null}
            </div>
          )}
        </CardContent>
      </Card>
    </div>
  )
}
