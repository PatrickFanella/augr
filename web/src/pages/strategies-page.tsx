import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Activity, Clock, Pause, Play, Plus } from 'lucide-react'
import { useState } from 'react'
import { Link } from 'react-router-dom'

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
    <div className="space-y-6" data-testid="strategies-page">
      <div className="flex items-center justify-between">
        <div>
          <h2 className="text-2xl font-semibold tracking-tight">Strategies</h2>
          <p className="text-sm text-muted-foreground">
            Create, configure, and monitor your trading strategies.
          </p>
        </div>
        <Button onClick={() => setCreateOpen(true)} data-testid="create-strategy-button">
          <Plus className="mr-2 size-4" />
          New strategy
        </Button>
      </div>

      <Card>
        <CardHeader>
          <CardTitle>All strategies</CardTitle>
          <CardDescription>
            {data != null
              ? `${data.total ?? data.data?.length ?? 0} strategies`
              : 'Loading…'}
          </CardDescription>
        </CardHeader>
        <CardContent>
          {isLoading ? (
            <div className="space-y-3" data-testid="strategies-loading">
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
                    <div className="flex items-center gap-3 rounded-lg border p-3 transition-colors hover:bg-secondary/40">
                      <div className="flex size-8 shrink-0 items-center justify-center rounded-full bg-primary/10 text-primary">
                        <Activity className="size-4" />
                      </div>
                      <Link
                        to={`/strategies/${strategy.id}`}
                        className="min-w-0 flex-1"
                        data-testid={`strategy-link-${strategy.id}`}
                      >
                        <div className="flex items-center gap-2">
                          <p className="truncate font-medium hover:underline">
                            {strategy.name}
                          </p>
                          <Badge variant={statusVariant(strategyStatus)} data-testid={`strategy-status-${strategy.id}`}>
                            {strategyStatus}
                          </Badge>
                          {strategy.skip_next_run ? <Badge variant="outline">skip next</Badge> : null}
                        </div>
                        <p className="text-xs text-muted-foreground">
                          {strategy.ticker}
                          {strategy.schedule_cron ? (
                            <span className="ml-2 inline-flex items-center gap-1">
                              <Clock className="size-3" />
                              scheduled
                            </span>
                          ) : null}
                          <span className="ml-2">
                            Updated {formatDate(strategy.updated_at)}
                          </span>
                        </p>
                      </Link>
                      <div className="flex items-center gap-2">
                        <MarketTypeBadge type={strategy.market_type} />
                        {strategy.is_paper ? (
                          <Badge variant="warning">paper</Badge>
                        ) : null}
                        <Button
                          variant="outline"
                          size="sm"
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
