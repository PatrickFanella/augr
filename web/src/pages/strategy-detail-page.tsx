import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { ArrowLeft, Pause, Play, SkipForward, Trash2 } from 'lucide-react'
import { useMemo, useState } from 'react'
import { Link, useNavigate, useParams } from 'react-router-dom'

import { PageHeader } from '@/components/layout/page-header'
import { StrategyConfigEditor } from '@/components/strategies/strategy-config-editor'
import { StrategyRunHistory } from '@/components/strategies/strategy-run-history'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { ApiClientError, apiClient } from '@/lib/api/client'
import type { StrategyStatus, StrategyUpdateRequest } from '@/lib/api/types'
import { describeCron } from '@/lib/cron-describe'

function resolveStrategyStatus(strategy: { status?: StrategyStatus; is_active?: boolean }): StrategyStatus {
  if (strategy.status) {
    return strategy.status
  }

  return strategy.is_active ? 'active' : 'inactive'
}

function statusBadgeVariant(status: StrategyStatus): 'success' | 'warning' | 'secondary' {
  switch (status) {
    case 'active':
      return 'success'
    case 'paused':
      return 'warning'
    default:
      return 'secondary'
  }
}

export function StrategyDetailPage() {
  const { id } = useParams<{ id: string }>()
  const navigate = useNavigate()
  const queryClient = useQueryClient()
  const [actionError, setActionError] = useState<string | null>(null)

  const { data: strategy, isLoading, isError } = useQuery({
    queryKey: ['strategy', id],
    queryFn: () => apiClient.getStrategy(id!),
    enabled: !!id,
  })

  const strategyStatus = useMemo(
    () => (strategy ? resolveStrategyStatus(strategy) : 'inactive'),
    [strategy],
  )
  const isStrategyActive = strategyStatus === 'active'
  const isStrategyPaused = strategyStatus === 'paused'

  function handleMutationError(err: unknown) {
    if (err instanceof ApiClientError && err.status === 409) {
      setActionError(err.message)
      return
    }

    if (err instanceof Error) {
      setActionError(err.message)
      return
    }

    setActionError('Unable to update strategy state.')
  }

  const updateMutation = useMutation({
    mutationFn: (data: StrategyUpdateRequest) => apiClient.updateStrategy(id!, data),
    onMutate: () => setActionError(null),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['strategy', id] })
      queryClient.invalidateQueries({ queryKey: ['strategies'] })
    },
    onError: handleMutationError,
  })

  const deleteMutation = useMutation({
    mutationFn: () => apiClient.deleteStrategy(id!),
    onMutate: () => setActionError(null),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['strategies'] })
      navigate('/strategies')
    },
    onError: handleMutationError,
  })

  const runMutation = useMutation({
    mutationFn: () => apiClient.runStrategy(id!),
    onMutate: () => setActionError(null),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['runs', { strategy_id: id }] })
    },
    onError: handleMutationError,
  })

  const pauseMutation = useMutation({
    mutationFn: () => apiClient.pauseStrategy(id!),
    onMutate: () => setActionError(null),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['strategy', id] })
      queryClient.invalidateQueries({ queryKey: ['strategies'] })
    },
    onError: handleMutationError,
  })

  const resumeMutation = useMutation({
    mutationFn: () => apiClient.resumeStrategy(id!),
    onMutate: () => setActionError(null),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['strategy', id] })
      queryClient.invalidateQueries({ queryKey: ['strategies'] })
    },
    onError: handleMutationError,
  })

  const skipMutation = useMutation({
    mutationFn: () => apiClient.skipNextRun(id!),
    onMutate: () => setActionError(null),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['strategy', id] })
      queryClient.invalidateQueries({ queryKey: ['strategies'] })
    },
    onError: handleMutationError,
  })
  const isLifecycleActionPending = pauseMutation.isPending || resumeMutation.isPending || skipMutation.isPending

  if (isLoading) {
    return (
      <div className="space-y-6" data-testid="strategy-detail-loading">
        <div className="h-8 w-48 animate-pulse rounded bg-muted" />
        <div className="h-64 animate-pulse rounded-lg border bg-muted" />
      </div>
    )
  }

  if (isError || !strategy) {
    return (
      <div className="space-y-4" data-testid="strategy-detail-error">
        <Link to="/strategies" className="inline-flex items-center gap-1 text-sm text-muted-foreground hover:text-foreground">
          <ArrowLeft className="size-4" />
          Back to strategies
        </Link>
        <Card>
          <CardContent className="py-8">
            <p className="text-center text-sm text-muted-foreground">
              Unable to load strategy. It may have been deleted or the API server is unavailable.
            </p>
          </CardContent>
        </Card>
      </div>
    )
  }

  return (
    <div className="space-y-4" data-testid="strategy-detail-page">
      <PageHeader
        eyebrow="Strategy detail"
        title={strategy.name}
        description={strategy.description || 'Execution settings, recent runs, and lifecycle controls for this strategy.'}
        meta={(
          <>
            <Badge variant={statusBadgeVariant(strategyStatus)} data-testid="strategy-status-badge">
              {strategyStatus}
            </Badge>
            {strategy.is_paper ? <Badge variant="warning">paper</Badge> : null}
            {strategy.skip_next_run ? <Badge variant="outline">skip next queued</Badge> : null}
          </>
        )}
        actions={(
          <>
            <Link
              to="/strategies"
              className="inline-flex items-center gap-1 rounded-md border border-border bg-background px-3 py-2 text-sm text-muted-foreground transition-colors hover:border-primary/25 hover:text-foreground"
            >
              <ArrowLeft className="size-4" />
              Back
            </Link>
            <Button
              variant="outline"
              onClick={() => runMutation.mutate()}
              disabled={runMutation.isPending}
              data-testid="run-strategy-button"
            >
              <Play className="mr-2 size-4" />
              {runMutation.isPending ? 'Running…' : 'Run now'}
            </Button>
            <Button
              variant="outline"
              onClick={() => pauseMutation.mutate()}
              disabled={!isStrategyActive || isLifecycleActionPending}
              data-testid="pause-strategy-button"
            >
              <Pause className="mr-2 size-4" />
              {pauseMutation.isPending ? 'Pausing…' : 'Pause'}
            </Button>
            <Button
              variant="default"
              onClick={() => resumeMutation.mutate()}
              disabled={!isStrategyPaused || isLifecycleActionPending}
              data-testid="resume-strategy-button"
            >
              <Play className="mr-2 size-4" />
              {resumeMutation.isPending ? 'Resuming…' : 'Resume'}
            </Button>
            <Button
              variant="ghost"
              onClick={() => skipMutation.mutate()}
              disabled={!isStrategyActive || isLifecycleActionPending}
              data-testid="skip-next-button"
            >
              <SkipForward className="mr-2 size-4" />
              {skipMutation.isPending ? 'Skipping…' : 'Skip next'}
            </Button>
            <Button
              variant="destructive"
              onClick={() => deleteMutation.mutate()}
              disabled={deleteMutation.isPending}
              data-testid="delete-strategy-button"
            >
              <Trash2 className="mr-2 size-4" />
              Delete
            </Button>
          </>
        )}
      />
      {actionError ? (
        <p className="text-sm text-destructive" data-testid="strategy-action-error">{actionError}</p>
      ) : null}

      <Card>
        <CardHeader>
          <CardTitle>Overview</CardTitle>
          <CardDescription>Strategy summary and current state</CardDescription>
        </CardHeader>
        <CardContent>
          <dl className="grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
            <div>
              <dt className="font-mono text-[11px] uppercase tracking-[0.16em] text-muted-foreground">Ticker</dt>
              <dd className="mt-1 text-sm font-medium">{strategy.ticker}</dd>
            </div>
            <div>
              <dt className="font-mono text-[11px] uppercase tracking-[0.16em] text-muted-foreground">Market type</dt>
              <dd>
                <Badge variant={strategy.market_type === 'stock' ? 'default' : strategy.market_type === 'crypto' ? 'secondary' : 'outline'}>
                  {strategy.market_type}
                </Badge>
              </dd>
            </div>
            <div>
              <dt className="font-mono text-[11px] uppercase tracking-[0.16em] text-muted-foreground">Status</dt>
              <dd className="flex items-center gap-2">
                <Badge variant={statusBadgeVariant(strategyStatus)}>{strategyStatus}</Badge>
                {strategy.is_paper ? <Badge variant="warning">paper</Badge> : null}
              </dd>
            </div>
            <div>
              <dt className="font-mono text-[11px] uppercase tracking-[0.16em] text-muted-foreground">Schedule</dt>
              <dd className="mt-1 font-mono text-[13px] font-medium">
                {strategy.schedule_cron ? describeCron(strategy.schedule_cron) : 'Manual only'}
                {strategy.schedule_cron && (
                  <span className="ml-2 text-[11px] text-muted-foreground">{strategy.schedule_cron}</span>
                )}
              </dd>
            </div>
          </dl>
        </CardContent>
      </Card>

      <div className="grid gap-4 xl:grid-cols-[minmax(0,0.95fr)_minmax(0,1.05fr)]">
        <StrategyRunHistory strategyId={strategy.id} />
        <StrategyConfigEditor
          strategy={strategy}
          onSave={(data) => updateMutation.mutate(data)}
          isSaving={updateMutation.isPending}
        />
      </div>
    </div>
  )
}
