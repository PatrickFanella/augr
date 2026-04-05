import { useMutation, useQuery } from '@tanstack/react-query'
import {
  Activity,
  BarChart3,
  CalendarDays,
  Clock,
  ExternalLink,
  FileText,
  FlaskConical,
  Loader2,
  Play,
  Receipt,
  Sparkles,
  TrendingUp,
} from 'lucide-react'
import { useMemo, useState } from 'react'
import { Link, useNavigate, useParams } from 'react-router-dom'

import { UpcomingEventsWidget } from '@/components/calendar/upcoming-events-widget'
import { PageHeader } from '@/components/layout/page-header'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { apiClient } from '@/lib/api/client'
import { formatCurrency } from '@/lib/format'
import { cn } from '@/lib/utils'
import { describeCron } from '@/lib/cron-describe'
import type {
  FilingAnalysis,
  OrderSide,
  OrderStatus,
  SECFiling,
  Strategy,
  StrategyStatus,
} from '@/lib/api/types'

// --- helpers ---

function todayStr(): string {
  return new Date().toISOString().slice(0, 10)
}

function thirtyDaysStr(): string {
  const d = new Date()
  d.setDate(d.getDate() + 30)
  return d.toISOString().slice(0, 10)
}

function formatDate(iso?: string): string {
  if (!iso) return '--'
  return new Date(iso).toLocaleDateString('en-US', {
    month: 'short',
    day: 'numeric',
    year: 'numeric',
  })
}

function formatDateTime(iso?: string): string {
  if (!iso) return '--'
  return new Date(iso).toLocaleString(undefined, {
    month: 'short',
    day: 'numeric',
    hour: '2-digit',
    minute: '2-digit',
  })
}

function formatNum(n?: number): string {
  if (n == null) return '--'
  return n.toLocaleString('en-US', { minimumFractionDigits: 2, maximumFractionDigits: 2 })
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
  return strategy.status ?? (strategy.is_active ? 'active' : 'inactive')
}

const SIDE_VARIANTS: Record<OrderSide, 'success' | 'destructive'> = {
  buy: 'success',
  sell: 'destructive',
}

const STATUS_VARIANTS: Record<OrderStatus, 'default' | 'success' | 'destructive' | 'warning' | 'secondary'> = {
  pending: 'default',
  submitted: 'default',
  partial: 'warning',
  filled: 'success',
  cancelled: 'secondary',
  rejected: 'destructive',
}

function HourBadge({ hour }: { hour: string }) {
  switch (hour) {
    case 'bmo':
      return <Badge variant="default">BMO</Badge>
    case 'amc':
      return <Badge variant="warning">AMC</Badge>
    default:
      return <Badge variant="secondary">{hour || '--'}</Badge>
  }
}

function epsColor(actual?: number, estimate?: number): string {
  if (actual == null || estimate == null) return ''
  if (actual > estimate) return 'text-emerald-400'
  if (actual < estimate) return 'text-red-400'
  return ''
}

function SentimentBadge({ sentiment }: { sentiment: string }) {
  switch (sentiment.toLowerCase()) {
    case 'bullish':
      return <Badge variant="success">{sentiment}</Badge>
    case 'bearish':
      return <Badge variant="destructive">{sentiment}</Badge>
    default:
      return <Badge variant="secondary">{sentiment}</Badge>
  }
}

function ImpactBadge({ impact }: { impact: string }) {
  switch (impact.toLowerCase()) {
    case 'high':
      return <Badge variant="destructive">High</Badge>
    case 'medium':
      return <Badge variant="warning">Medium</Badge>
    case 'low':
      return <Badge variant="secondary">Low</Badge>
    default:
      return <Badge variant="secondary">{impact || '--'}</Badge>
  }
}

function FilingAnalysisResult({ analysis }: { analysis: FilingAnalysis }) {
  return (
    <div className="space-y-2 rounded-md border border-border/50 bg-accent/20 p-3 text-sm">
      <div className="flex flex-wrap items-center gap-2">
        <SentimentBadge sentiment={analysis.sentiment} />
        <ImpactBadge impact={analysis.impact} />
        <Badge variant="outline">{analysis.action.replace(/_/g, ' ')}</Badge>
        <span className="ml-auto text-xs text-muted-foreground">
          confidence: {(analysis.confidence * 100).toFixed(0)}%
        </span>
      </div>
      <p className="text-muted-foreground">{analysis.summary}</p>
      {analysis.key_items.length > 0 && (
        <div>
          <span className="text-xs font-medium uppercase tracking-wider text-muted-foreground">Key items:</span>
          <ul className="ml-4 mt-1 list-disc text-xs text-muted-foreground">
            {analysis.key_items.map((item, i) => (
              <li key={i}>{item}</li>
            ))}
          </ul>
        </div>
      )}
    </div>
  )
}

function filingKey(f: SECFiling): string {
  return `${f.symbol}-${f.form}-${f.access_number}`
}

// --- skeletons ---

function LoadingRows({ count = 3 }: { count?: number }) {
  return (
    <div className="space-y-2">
      {Array.from({ length: count }).map((_, i) => (
        <div key={i} className="flex items-center gap-3 rounded-lg border p-3">
          <div className="h-4 w-24 animate-pulse rounded bg-muted" />
          <div className="h-4 w-16 animate-pulse rounded bg-muted" />
          <div className="ml-auto h-5 w-16 animate-pulse rounded-full bg-muted" />
        </div>
      ))}
    </div>
  )
}

// ===================== Page =====================

export function StockDetailPage() {
  const { ticker } = useParams<{ ticker: string }>()
  const navigate = useNavigate()
  const upperTicker = (ticker ?? '').toUpperCase()

  // ---- data queries (parallel) ----

  const { data: strategiesData, isLoading: strategiesLoading } = useQuery({
    queryKey: ['stock-strategies', upperTicker],
    queryFn: () => apiClient.listStrategies({ ticker: upperTicker }),
    enabled: !!ticker,
  })

  const { data: ordersData, isLoading: ordersLoading } = useQuery({
    queryKey: ['stock-orders', upperTicker],
    queryFn: () => apiClient.listOrders({ ticker: upperTicker, limit: 10 }),
    enabled: !!ticker,
  })

  const { data: tradesData, isLoading: tradesLoading } = useQuery({
    queryKey: ['stock-trades', upperTicker],
    queryFn: () => apiClient.listTrades({ ticker: upperTicker, limit: 10 }),
    enabled: !!ticker,
  })

  const { data: positionsData, isLoading: positionsLoading } = useQuery({
    queryKey: ['stock-positions', upperTicker],
    queryFn: () => apiClient.listPositions({ ticker: upperTicker }),
    enabled: !!ticker,
  })

  const { data: earningsData, isLoading: earningsLoading } = useQuery({
    queryKey: ['stock-earnings', upperTicker],
    queryFn: () => apiClient.getEarningsCalendar({ from: todayStr(), to: thirtyDaysStr() }),
    enabled: !!ticker,
  })

  const { data: filingsData, isLoading: filingsLoading } = useQuery({
    queryKey: ['stock-filings', upperTicker],
    queryFn: () => apiClient.getFilings({ ticker: upperTicker }),
    enabled: !!ticker,
  })

  const { data: universeData } = useQuery({
    queryKey: ['stock-universe', upperTicker],
    queryFn: () => apiClient.listUniverse({ search: upperTicker, limit: 1 }),
    enabled: !!ticker,
  })

  // ---- derived data ----

  const strategies = strategiesData?.data ?? []
  const orders = ordersData?.data ?? []
  const trades = tradesData?.data ?? []
  const positions = (positionsData?.data ?? []).filter((p) => !p.closed_at)
  const allEarnings = earningsData ?? []
  const tickerEarnings = allEarnings.filter(
    (e) => e.symbol.toUpperCase() === upperTicker,
  )
  const filings = filingsData ?? []
  const universeInfo = (universeData?.data ?? []).find(
    (t) => t.ticker.toUpperCase() === upperTicker,
  )

  // backtest configs for strategies matching this ticker
  const strategyIds = useMemo(() => strategies.map((s) => s.id), [strategies])

  const { data: backtestConfigsData, isLoading: backtestsLoading } = useQuery({
    queryKey: ['stock-backtests', strategyIds],
    queryFn: () => apiClient.listBacktestConfigs({}),
    enabled: strategyIds.length > 0,
  })

  const backtestConfigs = useMemo(() => {
    const all = backtestConfigsData?.data ?? []
    const idSet = new Set(strategyIds)
    return all.filter((bc) => idSet.has(bc.strategy_id))
  }, [backtestConfigsData, strategyIds])

  // filing analysis state
  const [analyses, setAnalyses] = useState<Record<string, FilingAnalysis>>({})
  const analyzeMutation = useMutation({
    mutationFn: (filing: SECFiling) =>
      apiClient.analyzeFiling({
        symbol: filing.symbol,
        form: filing.form,
        url: filing.url,
      }),
    onSuccess: (result, filing) => {
      setAnalyses((prev) => ({ ...prev, [filingKey(filing)]: result }))
    },
  })

  if (!ticker) {
    return <p className="p-8 text-muted-foreground">No ticker specified.</p>
  }

  return (
    <div className="space-y-4" data-testid="stock-detail-page">
      {/* Header */}
      <PageHeader
        eyebrow="Stock"
        title={upperTicker}
        description={universeInfo?.name}
        meta={
          <div className="flex flex-wrap items-center gap-2">
            {universeInfo && (
              <>
                <Badge variant="secondary">{universeInfo.exchange}</Badge>
                <Badge variant="outline">{universeInfo.index_group}</Badge>
                <Badge variant="default">Score: {universeInfo.watch_score.toFixed(2)}</Badge>
              </>
            )}
          </div>
        }
        actions={
          <Button variant="outline" size="sm" onClick={() => navigate(`/options?ticker=${upperTicker}`)}>
            <TrendingUp className="mr-1.5 size-3.5" />
            Options chain
          </Button>
        }
      />

      {/* Two-column layout */}
      <div className="grid gap-4 xl:grid-cols-[minmax(0,1.4fr)_minmax(320px,0.9fr)]">
        {/* Left column: strategies, orders, trades, positions */}
        <div className="space-y-4">
          {/* Active Strategies */}
          <Card>
            <CardHeader>
              <CardTitle className="flex items-center gap-2 text-base">
                <Activity className="size-4 text-muted-foreground" />
                Active strategies
                <Badge variant="secondary">{strategies.length}</Badge>
              </CardTitle>
            </CardHeader>
            <CardContent>
              {strategiesLoading ? (
                <LoadingRows />
              ) : strategies.length === 0 ? (
                <p className="text-sm text-muted-foreground">
                  No strategies for {upperTicker}.
                </p>
              ) : (
                <ul className="space-y-2">
                  {strategies.map((strategy) => {
                    const st = resolveStrategyStatus(strategy)
                    return (
                      <li key={strategy.id}>
                        <div className="flex items-center gap-3 rounded-lg border border-border p-3 transition-colors hover:bg-accent/45">
                          <div className="min-w-0 flex-1">
                            <div className="flex flex-wrap items-center gap-2">
                              <Link
                                to={`/strategies/${strategy.id}`}
                                className="truncate font-medium hover:text-primary"
                              >
                                {strategy.name}
                              </Link>
                              <Badge variant={statusVariant(st)}>{st}</Badge>
                              {strategy.is_paper && <Badge variant="warning">paper</Badge>}
                            </div>
                            <div className="mt-1 flex flex-wrap items-center gap-x-3 gap-y-1 font-mono text-[11px] uppercase tracking-[0.14em] text-muted-foreground">
                              {strategy.schedule_cron ? (
                                <span className="inline-flex items-center gap-1">
                                  <Clock className="size-3" />
                                  {describeCron(strategy.schedule_cron)}
                                </span>
                              ) : (
                                <span>manual only</span>
                              )}
                            </div>
                          </div>
                          <Link to={`/strategies/${strategy.id}`}>
                            <Button variant="outline" size="dense">
                              <Play className="mr-1 size-3" />
                              View
                            </Button>
                          </Link>
                        </div>
                      </li>
                    )
                  })}
                </ul>
              )}
            </CardContent>
          </Card>

          {/* Recent Orders */}
          <Card>
            <CardHeader>
              <CardTitle className="flex items-center gap-2 text-base">
                <Receipt className="size-4 text-muted-foreground" />
                Recent orders
                <Badge variant="secondary">{orders.length}</Badge>
              </CardTitle>
            </CardHeader>
            <CardContent>
              {ordersLoading ? (
                <LoadingRows />
              ) : orders.length === 0 ? (
                <p className="text-sm text-muted-foreground">No orders for {upperTicker}.</p>
              ) : (
                <div className="overflow-x-auto">
                  <table className="w-full text-sm">
                    <thead>
                      <tr className="border-b border-border text-left font-mono text-[11px] uppercase tracking-[0.16em] text-muted-foreground">
                        <th className="pb-2 font-medium">Side</th>
                        <th className="pb-2 font-medium">Type</th>
                        <th className="pb-2 font-medium">Qty</th>
                        <th className="pb-2 font-medium">Status</th>
                        <th className="pb-2 font-medium">Fill price</th>
                        <th className="pb-2 font-medium">Submitted</th>
                      </tr>
                    </thead>
                    <tbody>
                      {orders.map((order) => (
                        <tr
                          key={order.id}
                          className="cursor-pointer border-b border-border/50 transition-colors hover:bg-accent/45 last:border-0"
                          onClick={() => navigate(`/orders/${order.id}`)}
                        >
                          <td className="py-2">
                            <Badge variant={SIDE_VARIANTS[order.side]}>{order.side}</Badge>
                          </td>
                          <td className="py-2 text-muted-foreground">{order.order_type}</td>
                          <td className="py-2 font-mono text-[13px]">{order.quantity}</td>
                          <td className="py-2">
                            <Badge variant={STATUS_VARIANTS[order.status]}>
                              {order.status}
                            </Badge>
                          </td>
                          <td className="py-2 font-mono text-[13px]">
                            {order.filled_avg_price != null
                              ? `$${Number(order.filled_avg_price).toFixed(2)}`
                              : '--'}
                          </td>
                          <td className="py-2 font-mono text-[13px] text-muted-foreground">
                            {formatDateTime(order.submitted_at)}
                          </td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                </div>
              )}
            </CardContent>
          </Card>

          {/* Recent Trades */}
          <Card>
            <CardHeader>
              <CardTitle className="flex items-center gap-2 text-base">
                <TrendingUp className="size-4 text-muted-foreground" />
                Recent trades
                <Badge variant="secondary">{trades.length}</Badge>
              </CardTitle>
            </CardHeader>
            <CardContent>
              {tradesLoading ? (
                <LoadingRows />
              ) : trades.length === 0 ? (
                <p className="text-sm text-muted-foreground">No trades for {upperTicker}.</p>
              ) : (
                <div className="overflow-x-auto">
                  <table className="w-full text-sm">
                    <thead>
                      <tr className="border-b border-border text-left font-mono text-[11px] uppercase tracking-[0.16em] text-muted-foreground">
                        <th className="pb-2 font-medium">Side</th>
                        <th className="pb-2 font-medium">Qty</th>
                        <th className="pb-2 font-medium text-right">Price</th>
                        <th className="pb-2 font-medium">Date</th>
                      </tr>
                    </thead>
                    <tbody>
                      {trades.map((trade) => (
                        <tr
                          key={trade.id}
                          className="border-b border-border/50 transition-colors hover:bg-accent/45 last:border-0"
                        >
                          <td className="py-2">
                            <Badge variant={SIDE_VARIANTS[trade.side]}>{trade.side}</Badge>
                          </td>
                          <td className="py-2 font-mono text-[13px]">{trade.quantity}</td>
                          <td className="py-2 text-right font-mono text-[13px]">
                            {formatCurrency(trade.price)}
                          </td>
                          <td className="py-2 font-mono text-[13px] text-muted-foreground">
                            {formatDateTime(trade.executed_at)}
                          </td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                </div>
              )}
            </CardContent>
          </Card>

          {/* Open Positions */}
          <Card>
            <CardHeader>
              <CardTitle className="flex items-center gap-2 text-base">
                <BarChart3 className="size-4 text-muted-foreground" />
                Open positions
                <Badge variant="secondary">{positions.length}</Badge>
              </CardTitle>
            </CardHeader>
            <CardContent>
              {positionsLoading ? (
                <LoadingRows />
              ) : positions.length === 0 ? (
                <p className="text-sm text-muted-foreground">No open positions for {upperTicker}.</p>
              ) : (
                <div className="space-y-3">
                  {positions.map((pos) => (
                    <div
                      key={pos.id}
                      className="rounded-lg border border-border p-3"
                    >
                      <div className="flex flex-wrap items-center gap-2">
                        <Badge variant={pos.side === 'long' ? 'success' : 'destructive'}>
                          {pos.side}
                        </Badge>
                        <span className="font-mono text-sm font-medium">
                          {pos.quantity} shares
                        </span>
                      </div>
                      <div className="mt-2 grid grid-cols-2 gap-x-4 gap-y-1 text-sm sm:grid-cols-4">
                        <div>
                          <span className="text-xs text-muted-foreground">Avg entry</span>
                          <p className="font-mono">{formatCurrency(pos.avg_entry)}</p>
                        </div>
                        <div>
                          <span className="text-xs text-muted-foreground">Current</span>
                          <p className="font-mono">
                            {pos.current_price != null ? formatCurrency(pos.current_price) : '--'}
                          </p>
                        </div>
                        <div>
                          <span className="text-xs text-muted-foreground">Unrealized P&L</span>
                          <p
                            className={cn(
                              'font-mono font-medium',
                              pos.unrealized_pnl != null && pos.unrealized_pnl >= 0 && 'text-emerald-400',
                              pos.unrealized_pnl != null && pos.unrealized_pnl < 0 && 'text-red-400',
                            )}
                          >
                            {pos.unrealized_pnl != null ? formatCurrency(pos.unrealized_pnl) : '--'}
                          </p>
                        </div>
                        <div>
                          <span className="text-xs text-muted-foreground">Stop / TP</span>
                          <p className="font-mono text-xs">
                            {pos.stop_loss != null ? formatCurrency(pos.stop_loss) : '--'}
                            {' / '}
                            {pos.take_profit != null ? formatCurrency(pos.take_profit) : '--'}
                          </p>
                        </div>
                      </div>
                    </div>
                  ))}
                </div>
              )}
            </CardContent>
          </Card>
        </div>

        {/* Right column: earnings, filings, backtests, events widget */}
        <div className="space-y-4">
          {/* Upcoming Events Widget */}
          <UpcomingEventsWidget ticker={upperTicker} />

          {/* Upcoming Earnings (detailed) */}
          <Card>
            <CardHeader>
              <CardTitle className="flex items-center gap-2 text-base">
                <CalendarDays className="size-4 text-muted-foreground" />
                Earnings
                <Badge variant="secondary">{tickerEarnings.length}</Badge>
              </CardTitle>
            </CardHeader>
            <CardContent>
              {earningsLoading ? (
                <LoadingRows count={2} />
              ) : tickerEarnings.length === 0 ? (
                <p className="text-sm text-muted-foreground">
                  No upcoming earnings for {upperTicker} in the next 30 days.
                </p>
              ) : (
                <div className="space-y-3">
                  {tickerEarnings.map((ev, i) => (
                    <div key={`${ev.symbol}-${ev.date}-${i}`} className="rounded-lg border border-border p-3">
                      <div className="flex flex-wrap items-center gap-2">
                        <span className="font-medium">{formatDate(ev.date)}</span>
                        <HourBadge hour={ev.hour} />
                        <span className="text-xs text-muted-foreground">
                          Q{ev.quarter} {ev.year}
                        </span>
                      </div>
                      <div className="mt-2 grid grid-cols-2 gap-2 text-sm">
                        <div>
                          <span className="text-xs text-muted-foreground">EPS est</span>
                          <p className="font-mono">{formatNum(ev.eps_estimate)}</p>
                        </div>
                        <div>
                          <span className="text-xs text-muted-foreground">EPS actual</span>
                          <p className={cn('font-mono', epsColor(ev.eps_actual, ev.eps_estimate))}>
                            {formatNum(ev.eps_actual)}
                          </p>
                        </div>
                        <div>
                          <span className="text-xs text-muted-foreground">Rev est</span>
                          <p className="font-mono">{formatNum(ev.revenue_estimate)}</p>
                        </div>
                        <div>
                          <span className="text-xs text-muted-foreground">Rev actual</span>
                          <p className={cn('font-mono', epsColor(ev.revenue_actual, ev.revenue_estimate))}>
                            {formatNum(ev.revenue_actual)}
                          </p>
                        </div>
                      </div>
                    </div>
                  ))}
                </div>
              )}
            </CardContent>
          </Card>

          {/* SEC Filings */}
          <Card>
            <CardHeader>
              <CardTitle className="flex items-center gap-2 text-base">
                <FileText className="size-4 text-muted-foreground" />
                SEC Filings
                <Badge variant="secondary">{filings.length}</Badge>
              </CardTitle>
            </CardHeader>
            <CardContent>
              {filingsLoading ? (
                <LoadingRows />
              ) : filings.length === 0 ? (
                <p className="text-sm text-muted-foreground">No filings for {upperTicker}.</p>
              ) : (
                <div className="space-y-2">
                  {filings.map((f, i) => {
                    const key = filingKey(f)
                    const analysis = analyses[key]
                    const isAnalyzing =
                      analyzeMutation.isPending &&
                      analyzeMutation.variables &&
                      filingKey(analyzeMutation.variables) === key

                    return (
                      <div key={`${f.access_number}-${i}`} className="space-y-2">
                        <div className="flex flex-wrap items-center gap-2 rounded-lg border border-border p-3">
                          <Badge variant="secondary">{f.form}</Badge>
                          <span className="text-sm text-muted-foreground">
                            Filed {formatDate(f.filed_date)}
                          </span>
                          {f.report_date && (
                            <span className="text-xs text-muted-foreground">
                              Report {formatDate(f.report_date)}
                            </span>
                          )}
                          <div className="ml-auto flex items-center gap-2">
                            {f.url && (
                              <a
                                href={f.url}
                                target="_blank"
                                rel="noopener noreferrer"
                                className="inline-flex items-center gap-1 text-xs text-primary hover:underline"
                              >
                                <ExternalLink className="size-3" />
                                View
                              </a>
                            )}
                            {analysis ? (
                              <SentimentBadge sentiment={analysis.sentiment} />
                            ) : (
                              <Button
                                variant="outline"
                                size="sm"
                                disabled={isAnalyzing || !f.url}
                                onClick={() => analyzeMutation.mutate(f)}
                                className="gap-1"
                              >
                                {isAnalyzing ? (
                                  <Loader2 className="size-3 animate-spin" />
                                ) : (
                                  <Sparkles className="size-3" />
                                )}
                                Analyze
                              </Button>
                            )}
                          </div>
                        </div>
                        {analysis && <FilingAnalysisResult analysis={analysis} />}
                      </div>
                    )
                  })}
                </div>
              )}
            </CardContent>
          </Card>

          {/* Backtest Configs */}
          <Card>
            <CardHeader>
              <CardTitle className="flex items-center gap-2 text-base">
                <FlaskConical className="size-4 text-muted-foreground" />
                Backtests
                <Badge variant="secondary">{backtestConfigs.length}</Badge>
              </CardTitle>
            </CardHeader>
            <CardContent>
              {strategies.length > 0 && backtestsLoading ? (
                <LoadingRows count={2} />
              ) : backtestConfigs.length === 0 ? (
                <p className="text-sm text-muted-foreground">
                  No backtest configs linked to {upperTicker} strategies.
                </p>
              ) : (
                <ul className="space-y-2">
                  {backtestConfigs.map((bc) => (
                    <li key={bc.id}>
                      <Link
                        to={`/backtests/${bc.id}`}
                        className="flex items-center gap-3 rounded-lg border border-border p-3 transition-colors hover:bg-accent/45"
                      >
                        <div className="min-w-0 flex-1">
                          <p className="truncate font-medium">{bc.name}</p>
                          <p className="text-xs text-muted-foreground">
                            {formatDate(bc.start_date)} - {formatDate(bc.end_date)}
                          </p>
                        </div>
                        <Badge variant="secondary">
                          ${bc.simulation.initial_capital.toLocaleString()}
                        </Badge>
                      </Link>
                    </li>
                  ))}
                </ul>
              )}
            </CardContent>
          </Card>
        </div>
      </div>
    </div>
  )
}
