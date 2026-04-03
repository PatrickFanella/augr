import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Activity, Clock, Pause, Play, Plus } from 'lucide-react'
import { useState } from 'react'
import { Link } from 'react-router-dom'

import { PageHeader } from '@/components/layout/page-header'
import { CreateStrategyDialog } from '@/components/strategies/create-strategy-dialog'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { apiClient } from '@/lib/api/client'
import type { Strategy, StrategyCreateRequest, StrategyStatus } from '@/lib/api/types'

function MarketTypeBadge({ type }: { type: Strategy['market_type'] }) {
  const variants: Record<Strategy['market_type'], 'default' | 'secondary' | 'outline'> = {
    stock: 'default',
    crypto: 'secondary',
    polymarket: 'outline',
  }
  return <Badge variant={variants[type]}>{type}</Badge>
}

function statusVariant(status: StrategyStatus): 'success' | 'warning' | 'secondary' {
  switch (status) {
    case 'active':
      return 'success'
    case 'paused':
      return 'warning'
    default:
      return 'secondary'
  }
}

function resolveStrategyStatus(strategy: Strategy): StrategyStatus {
  if (strategy.status) {
    return strategy.status
  }

  return strategy.is_active ? 'active' : 'inactive'
}

function formatDate(dateStr: string) {
  return new Date(dateStr).toLocaleString(undefined, {
    month: 'short',
    day: 'numeric',
    hour: '2-digit',
    minute: '2-digit',
  })
}

export function StrategiesPage() {
  const [createOpen, setCreateOpen] = useState(false)
  const queryClient = useQueryClient()

  const { data, isLoading, isError } = useQuery({
    queryKey: ['strategies'],
    queryFn: () => apiClient.listStrategies({ limit: 100 }),
    refetchInterval: 30_000,
  })

  const createMutation = useMutation({
    mutationFn: (req: StrategyCreateRequest) => apiClient.createStrategy(req),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['strategies'] })
      setCreateOpen(false)
    },
  })

  const runMutation = useMutation({
    mutationFn: (id: string) => apiClient.runStrategy(id),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['strategies'] })
      queryClient.invalidateQueries({ queryKey: ['runs'] })
    },
  })

  return (
    <div className="space-y-4" data-testid="strategies-page">
      <PageHeader
        eyebrow="Execution"
        title="Strategies"
        description="Create, schedule, run, and inspect strategy lifecycles from a single dense workspace."
        actions={(
          <Button onClick={() => setCreateOpen(true)} data-testid="create-strategy-button">
            <Plus className="mr-2 size-4" />
            New strategy
          </Button>
        )}
      />

      <Card>
        <CardHeader>
          <CardTitle>All strategies</CardTitle>
          <CardDescription>
            {data != null ? `${data.total ?? data.data?.length ?? 0} total tracked strategies` : 'Loading…'}
          </CardDescription>
        </CardHeader>
        <CardContent>
          {isLoading ? (
            <div className="space-y-2.5" data-testid="strategies-loading">
              {Array.from({ length: 3 }).map((_, i) => (
                <div key={i} className="flex items-center gap-3 rounded-lg border p-3">
                  <div className="h-4 w-32 animate-pulse rounded bg-muted" />
                  <div className="ml-auto h-5 w-16 animate-pulse rounded-full bg-muted" />
                </div>
              ))}
            </div>
          ) : isError ? (
            <p className="text-sm text-muted-foreground" data-testid="strategies-error">
              Unable to load strategies. Start the API server to see live data.
            </p>
          ) : !data?.data?.length ? (
            <div
              className="flex flex-col items-center gap-2 py-8 text-center"
              data-testid="strategies-empty"
            >
              <Pause className="size-8 text-muted-foreground" />
              <p className="text-sm text-muted-foreground">
                No strategies yet. Create your first strategy to get started.
              </p>
            </div>
          ) : (
            <ul className="space-y-2" data-testid="strategies-list">
              {(data?.data ?? []).map((strategy) => {
                const strategyStatus = resolveStrategyStatus(strategy)

                return (
                  <li key={strategy.id}>
                    <div className="grid gap-3 rounded-lg border border-white/8 bg-background/45 p-3 transition-colors hover:border-primary/20 hover:bg-accent/45 xl:grid-cols-[minmax(0,1.6fr)_auto] xl:items-center">
                      <div className="flex min-w-0 gap-3">
                        <div className="flex size-9 shrink-0 items-center justify-center rounded-md border border-primary/15 bg-primary/10 text-primary">
                          <Activity className="size-4" />
                        </div>
                        <div className="min-w-0 flex-1">
                          <div className="flex flex-wrap items-center gap-2">
                            <Link
                              to={`/strategies/${strategy.id}`}
                              className="truncate font-medium hover:text-primary"
                              data-testid={`strategy-link-${strategy.id}`}
                            >
                              {strategy.name}
                            </Link>
                            <Badge variant={statusVariant(strategyStatus)} data-testid={`strategy-status-${strategy.id}`}>
                              {strategyStatus}
                            </Badge>
                            <MarketTypeBadge type={strategy.market_type} />
                            {strategy.is_paper ? <Badge variant="warning">paper</Badge> : null}
                            {strategy.skip_next_run ? <Badge variant="outline">skip next</Badge> : null}
                          </div>
                          <div className="mt-2 flex flex-wrap items-center gap-x-3 gap-y-1 font-mono text-[11px] uppercase tracking-[0.14em] text-muted-foreground">
                            <span>{strategy.ticker}</span>
                            {strategy.schedule_cron ? (
                              <span className="inline-flex items-center gap-1">
                                <Clock className="size-3" />
                                {strategy.schedule_cron}
                              </span>
                            ) : (
                              <span>manual only</span>
                            )}
                            <span>updated {formatDate(strategy.updated_at)}</span>
                          </div>
                        </div>
                      </div>

                      <div className="flex items-center gap-2 xl:justify-end">
                        <Button
                          variant="outline"
                          size="dense"
                          onClick={() => runMutation.mutate(strategy.id)}
                          disabled={runMutation.isPending}
                          data-testid={`run-strategy-${strategy.id}`}
                        >
                          <Play className="mr-1 size-3" />
                          Run
                        </Button>
                      </div>
                    </div>
                  </li>
                )
              })}
            </ul>
          )}
        </CardContent>
      </Card>

      <CreateStrategyDialog
        open={createOpen}
        onOpenChange={setCreateOpen}
        onSubmit={(formData) => createMutation.mutate(formData)}
        isSubmitting={createMutation.isPending}
      />
    </div>
  )
}
