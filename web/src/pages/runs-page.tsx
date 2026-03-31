import { useQuery } from '@tanstack/react-query'
import { AlertCircle, CheckCircle2, Clock3, Loader2, RefreshCw, XCircle } from 'lucide-react'
import { useMemo, useState } from 'react'

import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { apiClient } from '@/lib/api/client'
import type { PipelineRun, PipelineSignal, PipelineStatus, Strategy } from '@/lib/api/types'

const pageSize = 10

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

const signalConfig: Record<PipelineSignal, { label: string; variant: BadgeVariant }> = {
  buy: { label: 'Buy', variant: 'success' },
  sell: { label: 'Sell', variant: 'destructive' },
  hold: { label: 'Hold', variant: 'secondary' },
}

function formatDate(dateStr: string) {
  return new Date(dateStr).toLocaleString(undefined, {
    month: 'short',
    day: 'numeric',
    year: 'numeric',
    hour: '2-digit',
    minute: '2-digit',
  })
}

function formatDuration(startedAt: string, completedAt?: string) {
  if (!completedAt) {
    return '—'
  }

  const started = new Date(startedAt).getTime()
  const completed = new Date(completedAt).getTime()
  const totalSeconds = Math.floor((completed - started) / 1000)

  if (!Number.isFinite(totalSeconds) || totalSeconds < 0) {
    return '—'
  }

  const days = Math.floor(totalSeconds / 86_400)
  const hours = Math.floor((totalSeconds % 86_400) / 3_600)
  const minutes = Math.floor((totalSeconds % 3_600) / 60)
  const seconds = totalSeconds % 60

  if (days > 0) {
    return `${days}d ${hours}h`
  }

  if (hours > 0) {
    return `${hours}h ${minutes}m`
  }

  if (minutes > 0) {
    return seconds > 0 ? `${minutes}m ${seconds}s` : `${minutes}m`
  }

  return `${seconds}s`
}

function StatusBadge({ status }: { status: PipelineStatus }) {
  const config = statusConfig[status]
  const Icon = config.icon

  return (
    <Badge variant={config.variant} className="gap-1">
      <Icon className={`size-3 ${status === 'running' ? 'animate-spin' : ''}`} />
      {config.label}
    </Badge>
  )
}

function SignalBadge({ signal }: { signal?: PipelineSignal }) {
  if (!signal) {
    return <span className="text-sm text-muted-foreground">—</span>
  }

  const config = signalConfig[signal]

  return <Badge variant={config.variant}>{config.label}</Badge>
}

function RunsTableRow({
  run,
  strategyName,
}: {
  run: PipelineRun
  strategyName?: string
}) {
  return (
    <tr className="border-b last:border-0 hover:bg-secondary/40">
      <td className="px-4 py-3 font-medium">{run.ticker}</td>
      <td className="px-4 py-3">{strategyName ?? 'Unknown strategy'}</td>
      <td className="px-4 py-3">
        <StatusBadge status={run.status} />
      </td>
      <td className="px-4 py-3">
        <SignalBadge signal={run.signal} />
      </td>
      <td className="px-4 py-3 text-sm text-muted-foreground">{formatDate(run.started_at)}</td>
      <td className="px-4 py-3 text-sm text-muted-foreground">
        {formatDuration(run.started_at, run.completed_at)}
      </td>
    </tr>
  )
}

export function RunsPage() {
  const [offset, setOffset] = useState(0)

  const runsQuery = useQuery({
    queryKey: ['runs', { limit: pageSize, offset }],
    queryFn: () => apiClient.listRuns({ limit: pageSize, offset }),
    refetchInterval: 15_000,
  })

  const strategiesQuery = useQuery({
    queryKey: ['strategies', 'runs-page'],
    queryFn: () => apiClient.listStrategies({ limit: 1000 }),
    staleTime: 60_000,
  })

  const strategyNames = useMemo(
    () =>
      new Map(
        (strategiesQuery.data?.data ?? []).map((strategy: Strategy) => [strategy.id, strategy.name]),
      ),
    [strategiesQuery.data],
  )

  const runs = runsQuery.data?.data ?? []
  const isLoading = runsQuery.isLoading || (strategiesQuery.data == null && strategiesQuery.isLoading)
  const canGoPrevious = offset > 0
  const canGoNext = runs.length === pageSize

  return (
    <div className="space-y-6" data-testid="runs-page">
      <div>
        <h2 className="text-2xl font-semibold tracking-tight">Pipeline runs</h2>
        <p className="text-sm text-muted-foreground">
          Review recent strategy executions, signals, and pipeline outcomes.
        </p>
      </div>

      <Card>
        <CardHeader>
          <CardTitle>Run history</CardTitle>
          <CardDescription>
            {runsQuery.data != null ? `Showing ${offset + 1}-${offset + runs.length} runs` : 'Loading…'}
          </CardDescription>
        </CardHeader>
        <CardContent className="space-y-4">
          {isLoading ? (
            <div className="space-y-3" data-testid="runs-loading">
              {Array.from({ length: 5 }).map((_, index) => (
                <div key={index} className="h-12 animate-pulse rounded-md bg-muted" />
              ))}
            </div>
          ) : runsQuery.isError ? (
            <div className="flex flex-col items-start gap-3" data-testid="runs-error">
              <p className="text-sm text-muted-foreground">
                Unable to load pipeline runs. Start the API server to see live data.
              </p>
              <Button type="button" variant="outline" onClick={() => void runsQuery.refetch()}>
                <RefreshCw className="size-4" />
                Retry
              </Button>
            </div>
          ) : runs.length === 0 && offset === 0 ? (
            <div className="flex flex-col items-center gap-2 py-8 text-center" data-testid="runs-empty">
              <Clock3 className="size-8 text-muted-foreground" />
              <p className="text-sm text-muted-foreground">No pipeline runs yet.</p>
            </div>
          ) : (
            <>
              <div className="overflow-x-auto">
                <table className="min-w-full border-collapse" data-testid="runs-table">
                  <thead>
                    <tr className="border-b text-left text-sm text-muted-foreground">
                      <th className="px-4 pb-3 font-medium">Ticker</th>
                      <th className="px-4 pb-3 font-medium">Strategy name</th>
                      <th className="px-4 pb-3 font-medium">Status</th>
                      <th className="px-4 pb-3 font-medium">Signal</th>
                      <th className="px-4 pb-3 font-medium">Started</th>
                      <th className="px-4 pb-3 font-medium">Duration</th>
                    </tr>
                  </thead>
                  <tbody>
                    {runs.map((run) => (
                      <RunsTableRow
                        key={`${run.id}-${run.trade_date}`}
                        run={run}
                        strategyName={strategyNames.get(run.strategy_id)}
                      />
                    ))}
                  </tbody>
                </table>
              </div>

              {runs.length === 0 ? (
                <p className="text-sm text-muted-foreground" data-testid="runs-page-empty-page">
                  No runs found for this page.
                </p>
              ) : null}

              <div className="flex items-center justify-between" data-testid="runs-pagination">
                <p className="text-sm text-muted-foreground">
                  Page {Math.floor(offset / pageSize) + 1}
                </p>
                <div className="flex gap-2">
                  <Button
                    type="button"
                    variant="outline"
                    onClick={() => setOffset((currentOffset) => Math.max(0, currentOffset - pageSize))}
                    disabled={!canGoPrevious || runsQuery.isFetching}
                  >
                    Previous
                  </Button>
                  <Button
                    type="button"
                    variant="outline"
                    onClick={() => setOffset((currentOffset) => currentOffset + pageSize)}
                    disabled={!canGoNext || runsQuery.isFetching}
                  >
                    Next
                  </Button>
                </div>
              </div>
            </>
          )}
        </CardContent>
      </Card>
    </div>
  )
}
