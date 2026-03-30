import { useQuery } from '@tanstack/react-query'
import { AlertCircle, CheckCircle2, Clock, Loader2, XCircle } from 'lucide-react'

import { Badge } from '@/components/ui/badge'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { apiClient } from '@/lib/api/client'
import type { PipelineRun, PipelineStatus, UUID } from '@/lib/api/types'

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

function RunRow({ run }: { run: PipelineRun }) {
  return (
    <li className="flex items-center gap-3 rounded-lg border p-3 transition-colors hover:bg-secondary/40">
      <div className="min-w-0 flex-1">
        <div className="flex items-center gap-2">
          <p className="truncate text-sm font-medium">{run.ticker}</p>
          {run.signal ? (
            <Badge variant={run.signal === 'buy' ? 'success' : run.signal === 'sell' ? 'destructive' : 'secondary'}>
              {run.signal}
            </Badge>
          ) : null}
        </div>
        <p className="flex items-center gap-1 text-xs text-muted-foreground">
          <Clock className="size-3" />
          {formatDate(run.started_at)}
          {run.completed_at ? ` — ${formatDate(run.completed_at)}` : ''}
        </p>
      </div>
      <RunStatusBadge status={run.status} />
    </li>
  )
}

interface StrategyRunHistoryProps {
  strategyId: UUID
}

export function StrategyRunHistory({ strategyId }: StrategyRunHistoryProps) {
  const { data, isLoading, isError } = useQuery({
    queryKey: ['runs', { strategy_id: strategyId }],
    queryFn: () => apiClient.listRuns({ strategy_id: strategyId, limit: 20 }),
    refetchInterval: 15_000,
  })

  return (
    <Card data-testid="strategy-run-history">
      <CardHeader>
        <CardTitle>Run history</CardTitle>
        <CardDescription>Recent pipeline executions</CardDescription>
      </CardHeader>
      <CardContent>
        {isLoading ? (
          <div className="space-y-3" data-testid="run-history-loading">
            {Array.from({ length: 3 }).map((_, i) => (
              <div key={i} className="flex items-center gap-3 rounded-lg border p-3">
                <div className="h-4 w-32 animate-pulse rounded bg-muted" />
                <div className="ml-auto h-5 w-16 animate-pulse rounded-full bg-muted" />
              </div>
            ))}
          </div>
        ) : isError ? (
          <p className="text-sm text-muted-foreground" data-testid="run-history-error">
            Unable to load run history.
          </p>
        ) : !data?.data.length ? (
          <div className="flex flex-col items-center gap-2 py-8 text-center" data-testid="run-history-empty">
            <Clock className="size-8 text-muted-foreground" />
            <p className="text-sm text-muted-foreground">No runs yet</p>
          </div>
        ) : (
          <ul className="space-y-2" data-testid="run-history-list">
            {data.data.map((run) => (
              <RunRow key={run.id} run={run} />
            ))}
          </ul>
        )}
      </CardContent>
    </Card>
  )
}
