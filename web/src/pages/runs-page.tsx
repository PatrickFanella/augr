import { useQuery } from '@tanstack/react-query'
import { Activity, AlertCircle, CheckCircle2, Loader2, XCircle } from 'lucide-react'
import { useNavigate } from 'react-router-dom'

import { Badge } from '@/components/ui/badge'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { apiClient } from '@/lib/api/client'
import type { PipelineRun, PipelineStatus } from '@/lib/api/types'

type BadgeVariant = 'default' | 'secondary' | 'success' | 'destructive' | 'warning'

interface StatusInfo {
  icon: typeof CheckCircle2
  label: string
  variant: BadgeVariant
}

const statusConfig: Record<PipelineStatus, StatusInfo> = {
  completed: { icon: CheckCircle2, label: 'Completed', variant: 'success' },
  running: { icon: Loader2, label: 'Running', variant: 'default' },
  failed: { icon: XCircle, label: 'Failed', variant: 'destructive' },
  cancelled: { icon: AlertCircle, label: 'Cancelled', variant: 'warning' },
}

function RunStatusBadge({ status }: { status: PipelineStatus }) {
  const config = statusConfig[status]
  const Icon = config.icon

  return (
    <Badge variant={config.variant} className="gap-1">
      <Icon className={`size-3 ${status === 'running' ? 'animate-spin' : ''}`} />
      {config.label}
    </Badge>
  )
}

function formatDate(dateStr: string) {
  return new Date(dateStr).toLocaleString(undefined, {
    month: 'short',
    day: 'numeric',
    hour: '2-digit',
    minute: '2-digit',
  })
}

export function RunsPage() {
  const navigate = useNavigate()
  const { data, isLoading, isError } = useQuery({
    queryKey: ['runs'],
    queryFn: () => apiClient.listRuns({ limit: 50 }),
    refetchInterval: 15_000,
  })
  const runs = data?.data ?? []

  return (
    <div className="space-y-6" data-testid="runs-page">
      <div>
        <h2 className="text-2xl font-semibold tracking-tight">Pipeline runs</h2>
        <p className="text-sm text-muted-foreground">
          Review recent strategy executions and inspect full run details.
        </p>
      </div>

      <Card>
        <CardHeader>
          <CardTitle>Recent runs</CardTitle>
          <CardDescription>
            {data != null ? `${data.total ?? runs.length} runs` : 'Loading…'}
          </CardDescription>
        </CardHeader>
        <CardContent>
          {isLoading ? (
            <div className="space-y-3" data-testid="runs-loading">
              {Array.from({ length: 5 }).map((_, i) => (
                <div key={i} className="flex items-center gap-3 rounded-lg border p-3">
                  <div className="h-4 w-16 animate-pulse rounded bg-muted" />
                  <div className="h-5 w-20 animate-pulse rounded-full bg-muted" />
                  <div className="ml-auto h-4 w-28 animate-pulse rounded bg-muted" />
                </div>
              ))}
            </div>
          ) : isError ? (
            <p className="text-sm text-muted-foreground" data-testid="runs-error">
              Unable to load runs. Start the API server to see live data.
            </p>
          ) : !runs.length ? (
            <div className="flex flex-col items-center gap-2 py-8 text-center" data-testid="runs-empty">
              <Activity className="size-8 text-muted-foreground" />
              <p className="text-sm text-muted-foreground">No runs yet</p>
            </div>
          ) : (
            <div className="overflow-x-auto">
              <table className="w-full text-sm" data-testid="runs-table">
                <thead>
                  <tr className="border-b text-left text-muted-foreground">
                    <th className="pb-2 font-medium">Ticker</th>
                    <th className="pb-2 font-medium">Status</th>
                    <th className="pb-2 font-medium">Signal</th>
                    <th className="pb-2 font-medium">Started</th>
                    <th className="pb-2 font-medium">Completed</th>
                  </tr>
                </thead>
                <tbody>
                  {runs.map((run: PipelineRun) => (
                    <tr
                      key={run.id}
                      className="cursor-pointer border-b transition-colors hover:bg-secondary/40 last:border-0"
                      data-testid={`run-row-${run.id}`}
                      role="link"
                      tabIndex={0}
                      onClick={() => navigate(`/runs/${run.id}`)}
                      onKeyDown={(event) => {
                        if (event.key === 'Enter' || event.key === ' ') {
                          event.preventDefault()
                          navigate(`/runs/${run.id}`)
                        }
                      }}
                    >
                      <td className="py-3 font-medium">{run.ticker}</td>
                      <td className="py-3">
                        <RunStatusBadge status={run.status} />
                      </td>
                      <td className="py-3">
                        {run.signal ? (
                          <Badge
                            variant={
                              run.signal === 'buy'
                                ? 'success'
                                : run.signal === 'sell'
                                  ? 'destructive'
                                  : 'secondary'
                            }
                          >
                            {run.signal}
                          </Badge>
                        ) : (
                          '—'
                        )}
                      </td>
                      <td className="py-3">{formatDate(run.started_at)}</td>
                      <td className="py-3">{run.completed_at ? formatDate(run.completed_at) : '—'}</td>
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
