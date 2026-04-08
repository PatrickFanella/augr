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

  const { data: ordersData } = useQuery({
    queryKey: ['strategy-orders', id],
    queryFn: () => apiClient.listOrders({ strategy_id: id, limit: 5 }),
    enabled: !!id,
  })

  const { data: backtestsData } = useQuery({
    queryKey: ['strategy-backtests', id],
    queryFn: () => apiClient.listBacktestConfigs({ strategy_id: id }),
    enabled: !!id,
  })

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

      {strategy.prediction_market && (
        <Card>
          <CardHeader>
            <CardTitle>Prediction Market</CardTitle>
            <CardDescription>{strategy.prediction_market.question}</CardDescription>
          </CardHeader>
          <CardContent className="space-y-4">
            {/* Probability bar */}
            <div>
              <div className="mb-1 flex justify-between text-xs text-muted-foreground">
                <span>YES {(strategy.prediction_market.yes_price * 100).toFixed(1)}%</span>
                <span>NO {(strategy.prediction_market.no_price * 100).toFixed(1)}%</span>
              </div>
              <div className="flex h-3 overflow-hidden rounded-full bg-muted">
                <div
                  className="bg-success transition-all"
                  style={{ width: `${strategy.prediction_market.yes_price * 100}%` }}
                />
              </div>
            </div>

            <dl className="grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
              <div>
                <dt className="font-mono text-[11px] uppercase tracking-[0.16em] text-muted-foreground">Volume 24h</dt>
                <dd className="mt-1 font-mono text-sm font-medium">${strategy.prediction_market.volume_24h.toLocaleString(undefined, { maximumFractionDigits: 0 })}</dd>
              </div>
              <div>
                <dt className="font-mono text-[11px] uppercase tracking-[0.16em] text-muted-foreground">Liquidity</dt>
                <dd className="mt-1 font-mono text-sm font-medium">${strategy.prediction_market.liquidity.toLocaleString(undefined, { maximumFractionDigits: 0 })}</dd>
              </div>
              {strategy.prediction_market.best_ask_yes != null && strategy.prediction_market.best_bid_yes != null && (
                <div>
                  <dt className="font-mono text-[11px] uppercase tracking-[0.16em] text-muted-foreground">YES Spread</dt>
                  <dd className="mt-1 font-mono text-sm font-medium">
                    {strategy.prediction_market.best_bid_yes.toFixed(3)} / {strategy.prediction_market.best_ask_yes.toFixed(3)}
                  </dd>
                </div>
              )}
              {strategy.prediction_market.end_date && (
                <div>
                  <dt className="font-mono text-[11px] uppercase tracking-[0.16em] text-muted-foreground">Resolves</dt>
                  <dd className="mt-1 text-sm font-medium">
                    {new Date(strategy.prediction_market.end_date).toLocaleDateString(undefined, { month: 'short', day: 'numeric', year: 'numeric' })}
                    <span className="ml-1 text-xs text-muted-foreground">
                      ({Math.max(0, Math.ceil((new Date(strategy.prediction_market.end_date).getTime() - Date.now()) / 86400000))}d)
                    </span>
                  </dd>
                </div>
              )}
            </dl>

            {strategy.prediction_market.resolution_criteria && (
              <div>
                <dt className="mb-1 font-mono text-[11px] uppercase tracking-[0.16em] text-muted-foreground">Resolution criteria</dt>
                <dd className="text-sm text-muted-foreground">{strategy.prediction_market.resolution_criteria}</dd>
              </div>
            )}
          </CardContent>
        </Card>
      )}

      {(() => {
        // eslint-disable-next-line @typescript-eslint/no-explicit-any
        const rulesEngine = (strategy.config as any)?.rules_engine
        if (!rulesEngine) return null
        return (
          <Card>
            <CardHeader>
              <CardTitle>Rules Engine</CardTitle>
            </CardHeader>
            <CardContent className="space-y-4">
              {rulesEngine.name && (
                <div>
                  <span className="text-xs uppercase tracking-wide text-muted-foreground">Strategy</span>
                  <p className="font-medium">{rulesEngine.name}</p>
                  {rulesEngine.description && <p className="text-sm text-muted-foreground">{rulesEngine.description}</p>}
                </div>
              )}

              {rulesEngine.entry?.conditions && (
                <div>
                  <span className="text-xs uppercase tracking-wide text-muted-foreground">Entry ({rulesEngine.entry?.operator})</span>
                  <ul className="mt-1 space-y-1">
                    {rulesEngine.entry.conditions.map((c: { field: string; op: string; value?: unknown; ref?: string }, i: number) => (
                      <li key={i} className="text-sm font-mono">
                        {c.field} {c.op} {c.value != null ? String(c.value) : c.ref}
                      </li>
                    ))}
                  </ul>
                </div>
              )}

              {rulesEngine.exit?.conditions && (
                <div>
                  <span className="text-xs uppercase tracking-wide text-muted-foreground">Exit ({rulesEngine.exit?.operator})</span>
                  <ul className="mt-1 space-y-1">
                    {rulesEngine.exit.conditions.map((c: { field: string; op: string; value?: unknown; ref?: string }, i: number) => (
                      <li key={i} className="text-sm font-mono">
                        {c.field} {c.op} {c.value != null ? String(c.value) : c.ref}
                      </li>
                    ))}
                  </ul>
                </div>
              )}

              {rulesEngine.position_sizing && (
                <div>
                  <span className="text-xs uppercase tracking-wide text-muted-foreground">Position Sizing</span>
                  <div className="mt-1 grid grid-cols-2 gap-2 text-sm">
                    <span className="text-muted-foreground">Method</span>
                    <span className="font-mono">{rulesEngine.position_sizing.method}</span>
                    {rulesEngine.position_sizing.fraction_pct != null && (
                      <><span className="text-muted-foreground">Fraction</span><span className="font-mono">{rulesEngine.position_sizing.fraction_pct}%</span></>
                    )}
                    {rulesEngine.position_sizing.risk_per_trade_pct != null && (
                      <><span className="text-muted-foreground">Risk/Trade</span><span className="font-mono">{(rulesEngine.position_sizing.risk_per_trade_pct * 100).toFixed(1)}%</span></>
                    )}
                    {rulesEngine.position_sizing.atr_multiplier != null && (
                      <><span className="text-muted-foreground">ATR Mult</span><span className="font-mono">{rulesEngine.position_sizing.atr_multiplier}</span></>
                    )}
                  </div>
                </div>
              )}

              {rulesEngine.stop_loss && (
                <div>
                  <span className="text-xs uppercase tracking-wide text-muted-foreground">Stop Loss</span>
                  <div className="mt-1 grid grid-cols-2 gap-2 text-sm">
                    <span className="text-muted-foreground">Method</span>
                    <span className="font-mono">{rulesEngine.stop_loss.method}</span>
                    {rulesEngine.stop_loss.atr_multiplier != null && (
                      <><span className="text-muted-foreground">ATR Mult</span><span className="font-mono">{Number(rulesEngine.stop_loss.atr_multiplier).toFixed(2)}</span></>
                    )}
                    {rulesEngine.stop_loss.pct != null && (
                      <><span className="text-muted-foreground">Pct</span><span className="font-mono">{rulesEngine.stop_loss.pct}%</span></>
                    )}
                  </div>
                </div>
              )}

              {rulesEngine.take_profit && (
                <div>
                  <span className="text-xs uppercase tracking-wide text-muted-foreground">Take Profit</span>
                  <div className="mt-1 grid grid-cols-2 gap-2 text-sm">
                    <span className="text-muted-foreground">Method</span>
                    <span className="font-mono">{rulesEngine.take_profit.method}</span>
                    {rulesEngine.take_profit.ratio != null && (
                      <><span className="text-muted-foreground">R:R Ratio</span><span className="font-mono">{Number(rulesEngine.take_profit.ratio).toFixed(2)}</span></>
                    )}
                    {rulesEngine.take_profit.atr_multiplier != null && (
                      <><span className="text-muted-foreground">ATR Mult</span><span className="font-mono">{Number(rulesEngine.take_profit.atr_multiplier).toFixed(2)}</span></>
                    )}
                  </div>
                </div>
              )}

              {rulesEngine.filters && (
                <div>
                  <span className="text-xs uppercase tracking-wide text-muted-foreground">Filters</span>
                  <div className="mt-1 grid grid-cols-2 gap-2 text-sm">
                    {rulesEngine.filters.min_volume != null && (
                      <><span className="text-muted-foreground">Min Volume</span><span className="font-mono">{Number(rulesEngine.filters.min_volume).toLocaleString()}</span></>
                    )}
                    {rulesEngine.filters.min_atr != null && (
                      <><span className="text-muted-foreground">Min ATR</span><span className="font-mono">{rulesEngine.filters.min_atr}</span></>
                    )}
                  </div>
                </div>
              )}
            </CardContent>
          </Card>
        )
      })()}

      <div className="grid gap-4 xl:grid-cols-[minmax(0,0.95fr)_minmax(0,1.05fr)]">
        <StrategyRunHistory strategyId={strategy.id} />
        <StrategyConfigEditor
          strategy={strategy}
          onSave={(data) => updateMutation.mutate(data)}
          isSaving={updateMutation.isPending}
        />
      </div>

      {(ordersData?.data?.length ?? 0) > 0 && (
        <Card>
          <CardHeader>
            <CardTitle>Recent Orders</CardTitle>
            <CardDescription>Last 5 orders placed by this strategy</CardDescription>
          </CardHeader>
          <CardContent>
            <div className="overflow-x-auto">
              <table className="w-full text-sm">
                <thead>
                  <tr className="border-b border-border text-left font-mono text-[11px] uppercase tracking-[0.16em] text-muted-foreground">
                    <th className="pb-2 font-medium">Ticker</th>
                    <th className="pb-2 font-medium">Side</th>
                    <th className="pb-2 font-medium">Status</th>
                    <th className="pb-2 font-medium">Date</th>
                  </tr>
                </thead>
                <tbody>
                  {ordersData!.data.map((order) => (
                    <tr key={order.id} className="border-b border-border last:border-0">
                      <td className="py-2 font-mono text-[13px]">
                        <Link to={`/orders/${order.id}`} className="text-primary hover:underline">
                          {order.ticker}
                        </Link>
                      </td>
                      <td className="py-2">
                        <Badge variant={order.side === 'buy' ? 'success' : 'destructive'}>{order.side}</Badge>
                      </td>
                      <td className="py-2">
                        <Badge variant="outline">{order.status}</Badge>
                      </td>
                      <td className="py-2 text-muted-foreground">{new Date(order.created_at).toLocaleDateString()}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          </CardContent>
        </Card>
      )}

      {(backtestsData?.data?.length ?? 0) > 0 && (
        <Card>
          <CardHeader>
            <CardTitle>Backtests</CardTitle>
            <CardDescription>Backtest configurations linked to this strategy</CardDescription>
          </CardHeader>
          <CardContent>
            <ul className="space-y-2">
              {backtestsData!.data.map((config) => (
                <li key={config.id} className="flex items-center justify-between rounded-md border border-border p-3">
                  <div>
                    <Link to={`/backtests/${config.id}`} className="text-sm font-medium text-primary hover:underline">
                      {config.name}
                    </Link>
                    {config.description && (
                      <p className="text-xs text-muted-foreground">{config.description}</p>
                    )}
                  </div>
                  <span className="text-xs text-muted-foreground">
                    {new Date(config.start_date).toLocaleDateString()} &ndash; {new Date(config.end_date).toLocaleDateString()}
                  </span>
                </li>
              ))}
            </ul>
          </CardContent>
        </Card>
      )}
    </div>
  )
}
