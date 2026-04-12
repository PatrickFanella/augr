import { useQuery } from '@tanstack/react-query'
import { AlertTriangle, Loader2, ShieldCheck } from 'lucide-react'
import { useMemo } from 'react'
import { Bar, BarChart, Cell, ResponsiveContainer, Tooltip } from 'recharts'

import { PageHeader } from '@/components/layout/page-header'
import { Badge } from '@/components/ui/badge'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { apiClient } from '@/lib/api/client'
import type { AutomationJobHealth } from '@/lib/api/types'

function formatRelativeTime(iso?: string): string {
  if (!iso) return '--'
  const diff = Date.now() - new Date(iso).getTime()
  const seconds = Math.floor(diff / 1000)
  if (seconds < 60) return `${seconds}s ago`
  const minutes = Math.floor(seconds / 60)
  if (minutes < 60) return `${minutes}m ago`
  const hours = Math.floor(minutes / 60)
  if (hours < 24) return `${hours}h ago`
  const days = Math.floor(hours / 24)
  return `${days}d ago`
}

function formatDuration(ms: number): string {
  if (ms < 60_000) return `${Math.floor(ms / 1000)}s`
  if (ms < 3_600_000) return `${Math.floor(ms / 60_000)}m`
  return `${Math.floor(ms / 3_600_000)}h`
}

function jobStatusBadge(job: AutomationJobHealth) {
  if (!job.enabled) return <Badge variant="secondary">Disabled</Badge>
  if (job.consecutive_failures >= 3) return <Badge variant="destructive">Failing</Badge>
  if (job.consecutive_failures > 0) return <Badge variant="warning">Degraded</Badge>
  return <Badge variant="success">Healthy</Badge>
}

function buildFailureRateSeries(
  runs: { status: string; started_at: string }[],
  bins = 10,
): { bin: string; rate: number; failed: number; total: number }[] {
  if (!runs.length) return []
  const sorted = [...runs].sort(
    (a, b) => new Date(b.started_at).getTime() - new Date(a.started_at).getTime(),
  )
  const perBin = Math.ceil(sorted.length / bins)
  const result = []
  for (let i = 0; i < bins; i++) {
    const chunk = sorted.slice(i * perBin, (i + 1) * perBin)
    if (!chunk.length) break
    const failed = chunk.filter((r) => r.status === 'failed').length
    result.unshift({
      bin: `${i + 1}`,
      rate: chunk.length ? Math.round((failed / chunk.length) * 100) : 0,
      failed,
      total: chunk.length,
    })
  }
  return result
}

const STALE_THRESHOLD_MS = 60 * 60 * 1000

export function ReliabilityPage() {
  const healthQuery = useQuery({
    queryKey: ['automation-health'],
    queryFn: () => apiClient.getAutomationHealth(),
    refetchInterval: 30_000,
  })

  const runningRunsQuery = useQuery({
    queryKey: ['runs', { status: 'running', limit: 50 }],
    queryFn: () => apiClient.listRuns({ status: 'running', limit: 50 } as Parameters<typeof apiClient.listRuns>[0]),
    refetchInterval: 30_000,
  })

  const recentRunsQuery = useQuery({
    queryKey: ['runs', { limit: 50 }],
    queryFn: () => apiClient.listRuns({ limit: 50 } as Parameters<typeof apiClient.listRuns>[0]),
    refetchInterval: 60_000,
  })

  const data = healthQuery.data
  const jobs = data?.jobs ?? []

  const runningRuns = useMemo(() => runningRunsQuery.data?.data ?? [], [runningRunsQuery.data])
  const recentRuns = useMemo(() => recentRunsQuery.data?.data ?? [], [recentRunsQuery.data])

  const staleRuns = useMemo(
    () =>
      runningRuns.filter(
        (r) => Date.now() - new Date(r.started_at).getTime() > STALE_THRESHOLD_MS,
      ),
    [runningRuns],
  )

  const oldestRunningMs = useMemo(() => {
    if (!runningRuns.length) return null
    const oldest = Math.min(...runningRuns.map((r) => new Date(r.started_at).getTime()))
    return Date.now() - oldest
  }, [runningRuns])

  const failureRateSeries = useMemo(() => buildFailureRateSeries(recentRuns), [recentRuns])

  const overallFailureRate = useMemo(() => {
    if (!recentRuns.length) return null
    const failed = recentRuns.filter((r) => r.status === 'failed').length
    return Math.round((failed / recentRuns.length) * 100)
  }, [recentRuns])

  return (
    <div className="space-y-4" data-testid="reliability-page">
      <PageHeader
        title="Reliability"
        description="Automation health and system status."
        meta={<ShieldCheck className="size-4 text-muted-foreground" />}
      />

      {data && (
        <Card>
          <CardHeader>
            <CardTitle>System Status</CardTitle>
          </CardHeader>
          <CardContent>
            <div className="flex items-center gap-3">
              <Badge variant={data.healthy ? 'success' : 'destructive'}>
                {data.healthy ? 'Healthy' : 'Degraded'}
              </Badge>
              <span className="text-sm text-muted-foreground">
                {data.total_jobs} job{data.total_jobs !== 1 ? 's' : ''} total
                {data.failing_jobs > 0 && (
                  <span className="ml-2 text-destructive">
                    · {data.failing_jobs} failing
                  </span>
                )}
                {data.degraded_jobs > 0 && (
                  <span className="ml-2 text-amber-500">
                    · {data.degraded_jobs} degraded
                  </span>
                )}
              </span>
            </div>
          </CardContent>
        </Card>
      )}

      <div className="grid gap-4 sm:grid-cols-2">
        <Card data-testid="stale-run-card">
          <CardHeader>
            <CardTitle className="flex items-center gap-2">
              Active Runs
              {staleRuns.length > 0 && (
                <AlertTriangle className="size-4 text-amber-500" />
              )}
            </CardTitle>
          </CardHeader>
          <CardContent>
            {runningRunsQuery.isLoading ? (
              <div className="flex items-center gap-2 py-2 text-sm text-muted-foreground">
                <Loader2 className="size-4 animate-spin" />
                Loading...
              </div>
            ) : (
              <div className="space-y-2">
                <div className="flex items-baseline gap-2">
                  <span className="text-3xl font-bold tabular-nums">
                    {runningRuns.length}
                  </span>
                  <span className="text-sm text-muted-foreground">running</span>
                </div>
                {oldestRunningMs != null && (
                  <p className="text-xs text-muted-foreground">
                    Oldest: {formatDuration(oldestRunningMs)}
                  </p>
                )}
                {staleRuns.length > 0 ? (
                  <p className="text-xs font-medium text-amber-500">
                    {staleRuns.length} stale run{staleRuns.length !== 1 ? 's' : ''} (&gt;1h)
                  </p>
                ) : runningRuns.length > 0 ? (
                  <p className="text-xs text-muted-foreground">No stale runs</p>
                ) : (
                  <p className="text-xs text-muted-foreground">No active runs</p>
                )}
              </div>
            )}
          </CardContent>
        </Card>

        <Card data-testid="failure-rate-card">
          <CardHeader>
            <CardTitle>Pipeline Failure Rate</CardTitle>
          </CardHeader>
          <CardContent>
            {recentRunsQuery.isLoading ? (
              <div className="flex items-center gap-2 py-2 text-sm text-muted-foreground">
                <Loader2 className="size-4 animate-spin" />
                Loading...
              </div>
            ) : (
              <div className="space-y-2">
                <div className="flex items-baseline gap-2">
                  <span className="text-3xl font-bold tabular-nums">
                    {overallFailureRate ?? '--'}
                    {overallFailureRate != null && '%'}
                  </span>
                  <span className="text-sm text-muted-foreground">last {recentRuns.length} runs</span>
                </div>
                {failureRateSeries.length > 1 ? (
                  <ResponsiveContainer width="100%" height={48}>
                    <BarChart data={failureRateSeries} barSize={8}>
                      <Tooltip
                        content={({ active, payload }) => {
                          if (!active || !payload?.length) return null
                          const d = payload[0].payload as { failed: number; total: number; rate: number }
                          return (
                            <div className="rounded border border-border bg-background px-2 py-1 text-xs shadow">
                              {d.failed}/{d.total} failed ({d.rate}%)
                            </div>
                          )
                        }}
                      />
                      <Bar dataKey="rate" radius={[2, 2, 0, 0]}>
                        {failureRateSeries.map((entry, index) => (
                          <Cell
                            key={index}
                            fill={entry.rate > 50 ? 'hsl(var(--destructive))' : entry.rate > 0 ? 'hsl(var(--warning))' : 'hsl(var(--success))'}
                          />
                        ))}
                      </Bar>
                    </BarChart>
                  </ResponsiveContainer>
                ) : (
                  <p className="text-xs text-muted-foreground">
                    {recentRuns.length === 0 ? 'No run history yet' : 'Insufficient data for chart'}
                  </p>
                )}
              </div>
            )}
          </CardContent>
        </Card>
      </div>

      <Card>
        <CardHeader>
          <CardTitle>Automation Health</CardTitle>
        </CardHeader>
        <CardContent>
          {healthQuery.isLoading && (
            <div className="flex items-center gap-2 py-6 text-sm text-muted-foreground">
              <Loader2 className="size-4 animate-spin" />
              Loading...
            </div>
          )}

          {healthQuery.isError && (
            <p className="py-4 text-sm text-destructive">
              Failed to load automation health.
            </p>
          )}

          {!healthQuery.isLoading && jobs.length === 0 && !healthQuery.isError && (
            <p className="py-4 text-sm text-muted-foreground">
              No automation jobs found.
            </p>
          )}

          {jobs.length > 0 && (
            <div className="overflow-x-auto">
              <table className="w-full text-left text-sm">
                <thead>
                  <tr className="border-b border-border text-xs font-medium uppercase tracking-wider text-muted-foreground">
                    <th className="px-2 py-2">Name</th>
                    <th className="px-2 py-2">Status</th>
                    <th className="px-2 py-2">Running</th>
                    <th className="px-2 py-2 text-right">Error Count</th>
                    <th className="px-2 py-2">Last Run</th>
                  </tr>
                </thead>
                <tbody>
                  {jobs.map((job) => (
                    <tr
                      key={job.name}
                      className="border-b border-border/50 hover:bg-accent/30"
                    >
                      <td className="px-2 py-1.5 font-mono font-medium">
                        {job.name}
                      </td>
                      <td className="px-2 py-1.5">
                        {jobStatusBadge(job)}
                      </td>
                      <td className="px-2 py-1.5">
                        {job.running ? (
                          <span className="inline-flex items-center gap-1 text-emerald-400">
                            <span className="inline-block size-2 rounded-full bg-emerald-400" />
                            Yes
                          </span>
                        ) : (
                          <span className="text-muted-foreground">No</span>
                        )}
                      </td>
                      <td className="px-2 py-1.5 text-right font-mono">
                        {job.error_count > 0 ? (
                          <span className="text-destructive">{job.error_count}</span>
                        ) : (
                          job.error_count
                        )}
                      </td>
                      <td className="px-2 py-1.5 text-xs text-muted-foreground">
                        {formatRelativeTime(job.last_run)}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </CardContent>
      </Card>
    </div>
  )
}
