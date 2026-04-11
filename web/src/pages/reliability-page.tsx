import { useQuery } from '@tanstack/react-query'
import { Loader2, ShieldCheck } from 'lucide-react'

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

function jobStatusBadge(job: AutomationJobHealth) {
  if (!job.enabled) return <Badge variant="secondary">Disabled</Badge>
  if (job.consecutive_failures >= 3) return <Badge variant="destructive">Failing</Badge>
  if (job.consecutive_failures > 0) return <Badge variant="warning">Degraded</Badge>
  return <Badge variant="success">Healthy</Badge>
}

export function ReliabilityPage() {
  const healthQuery = useQuery({
    queryKey: ['automation-health'],
    queryFn: () => apiClient.getAutomationHealth(),
    refetchInterval: 30_000,
  })

  const data = healthQuery.data
  const jobs = data?.jobs ?? []

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
              </span>
            </div>
          </CardContent>
        </Card>
      )}

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
