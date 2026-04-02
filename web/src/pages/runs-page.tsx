import { useQuery } from '@tanstack/react-query'
import { Activity, ChevronLeft, ChevronRight, Search } from 'lucide-react'
import { useMemo, useState } from 'react'
import { Link, useNavigate } from 'react-router-dom'

import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { apiClient } from '@/lib/api/client'
import type { PipelineRun, PipelineSignal, PipelineStatus, Strategy, UUID } from '@/lib/api/types'

const PAGE_SIZE = 20
const PAGE_REQUEST_SIZE = PAGE_SIZE + 1
const STATUS_OPTIONS: PipelineStatus[] = ['running', 'completed', 'failed', 'cancelled']
const SIGNAL_VARIANTS: Record<PipelineSignal, 'success' | 'destructive' | 'secondary'> = {
  buy: 'success',
  sell: 'destructive',
  hold: 'secondary',
}
const STATUS_VARIANTS: Record<PipelineStatus, 'default' | 'success' | 'destructive' | 'warning'> = {
  running: 'default',
  completed: 'success',
  failed: 'destructive',
  cancelled: 'warning',
}
const INTERACTIVE_SELECTOR = 'a, button, input, select, textarea, [role="button"], [role="link"]'

function formatDateFilter(value: string, boundary: 'start' | 'end') {
  if (!value) {
    return undefined
  }

  return boundary === 'start' ? `${value}T00:00:00.000Z` : `${value}T23:59:59.999Z`
}

function strategyLabel(strategy: Strategy) {
  return `${strategy.name} (${strategy.ticker})`
}

function formatStatusLabel(status: PipelineStatus) {
  return status.charAt(0).toUpperCase() + status.slice(1)
}

function formatRunDate(dateStr?: string) {
  if (!dateStr) {
    return '—'
  }

  return new Date(dateStr).toLocaleString(undefined, {
    month: 'short',
    day: 'numeric',
    year: 'numeric',
    hour: '2-digit',
    minute: '2-digit',
  })
}

function RunStatusBadge({ status }: { status: PipelineStatus }) {
  return <Badge variant={STATUS_VARIANTS[status]}>{formatStatusLabel(status)}</Badge>
}

function RunSignalBadge({ signal }: { signal: PipelineSignal }) {
  return <Badge variant={SIGNAL_VARIANTS[signal]}>{signal}</Badge>
}

export function RunsPage() {
  const navigate = useNavigate()
  const [draftStrategyId, setDraftStrategyId] = useState<UUID | ''>('')
  const [draftStatus, setDraftStatus] = useState<PipelineStatus | ''>('')
  const [draftStartDate, setDraftStartDate] = useState('')
  const [draftEndDate, setDraftEndDate] = useState('')
  const [strategyId, setStrategyId] = useState<UUID | ''>('')
  const [status, setStatus] = useState<PipelineStatus | ''>('')
  const [startDate, setStartDate] = useState('')
  const [endDate, setEndDate] = useState('')
  const [offset, setOffset] = useState(0)

  const { data: strategiesData } = useQuery({
    queryKey: ['strategies', 'runs-filter-options'],
    queryFn: () => apiClient.listStrategies({ limit: 500 }),
  })

  const { data, isLoading, isError } = useQuery({
    queryKey: ['runs', strategyId, status, startDate, endDate, offset],
    queryFn: () =>
      apiClient.listRuns({
        strategy_id: strategyId || undefined,
        status: status || undefined,
        start_date: formatDateFilter(startDate, 'start'),
        end_date: formatDateFilter(endDate, 'end'),
        limit: PAGE_REQUEST_SIZE,
        offset,
      }),
    refetchInterval: 15_000,
    refetchIntervalInBackground: false,
  })

  const strategies = useMemo(() => strategiesData?.data ?? [], [strategiesData?.data])
  const strategiesById = useMemo(
    () => new Map(strategies.map((strategy) => [strategy.id, strategy])),
    [strategies],
  )
  const visibleRuns = (data?.data ?? []).slice(0, PAGE_SIZE)
  const visibleCount = visibleRuns.length
  const hasNextPage = (data?.data?.length ?? 0) > PAGE_SIZE
  const pageLabel = useMemo(() => Math.floor(offset / PAGE_SIZE) + 1, [offset])
  const hasActiveFilters = Boolean(strategyId || status || startDate || endDate)

  function applyFilters() {
    setOffset(0)
    setStrategyId(draftStrategyId)
    setStatus(draftStatus)
    setStartDate(draftStartDate)
    setEndDate(draftEndDate)
  }

  function clearFilters() {
    setDraftStrategyId('')
    setDraftStatus('')
    setDraftStartDate('')
    setDraftEndDate('')
    setStrategyId('')
    setStatus('')
    setStartDate('')
    setEndDate('')
    setOffset(0)
  }

  return (
    <div className="space-y-6" data-testid="runs-page">
      <div className="space-y-1">
        <h2 className="text-2xl font-semibold tracking-tight">Pipeline runs</h2>
        <p className="text-sm text-muted-foreground">
          Review recent executions, narrow the list by strategy and status, and jump into run
          details.
        </p>
      </div>

      <Card>
        <CardHeader>
          <CardTitle>Filter runs</CardTitle>
          <CardDescription>Apply filters to narrow down the run history table.</CardDescription>
        </CardHeader>
        <CardContent>
          <form
            className="grid gap-3 lg:grid-cols-[minmax(0,1fr)_220px_170px_170px_auto]"
            onSubmit={(event) => {
              event.preventDefault()
              applyFilters()
            }}
          >
            <select
              value={draftStrategyId}
              onChange={(event) => setDraftStrategyId(event.target.value as UUID | '')}
              aria-label="Strategy"
              className="flex h-9 w-full rounded-md border border-input bg-transparent px-3 py-1 text-sm shadow-xs transition-colors focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring"
            >
              <option value="">All strategies</option>
              {strategies.map((strategy) => (
                <option key={strategy.id} value={strategy.id}>
                  {strategyLabel(strategy)}
                </option>
              ))}
            </select>
            <select
              value={draftStatus}
              onChange={(event) => setDraftStatus(event.target.value as PipelineStatus | '')}
              aria-label="Status"
              className="flex h-9 w-full rounded-md border border-input bg-transparent px-3 py-1 text-sm shadow-xs transition-colors focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring"
            >
              <option value="">All statuses</option>
              {STATUS_OPTIONS.map((option) => (
                <option key={option} value={option}>
                  {formatStatusLabel(option)}
                </option>
              ))}
            </select>
            <Input
              type="date"
              value={draftStartDate}
              onChange={(event) => setDraftStartDate(event.target.value)}
              aria-label="From date"
              max={draftEndDate || undefined}
            />
            <Input
              type="date"
              value={draftEndDate}
              onChange={(event) => setDraftEndDate(event.target.value)}
              aria-label="To date"
              min={draftStartDate || undefined}
            />
            <div className="flex gap-2">
              <Button type="submit" data-testid="apply-run-filters">
                <Search className="size-4" />
                Apply
              </Button>
              <Button type="button" variant="outline" onClick={clearFilters}>
                Clear
              </Button>
            </div>
          </form>
        </CardContent>
      </Card>

      <Card>
        <CardHeader className="gap-3 sm:flex-row sm:items-end sm:justify-between">
          <div className="space-y-1.5">
            <CardTitle>Run history</CardTitle>
            <CardDescription>
              {isLoading
                ? 'Loading…'
                : isError
                  ? 'Unable to load runs'
                  : visibleCount
                    ? `Showing ${offset + 1}-${offset + visibleCount} on page ${pageLabel}`
                    : 'No runs on this page'}
            </CardDescription>
          </div>
          <div className="flex items-center gap-2">
            <Button
              type="button"
              variant="outline"
              size="sm"
              onClick={() => setOffset((current) => Math.max(0, current - PAGE_SIZE))}
              disabled={offset === 0}
            >
              <ChevronLeft className="size-4" />
              Previous
            </Button>
            <Button
              type="button"
              variant="outline"
              size="sm"
              onClick={() => setOffset((current) => current + PAGE_SIZE)}
              disabled={!hasNextPage}
            >
              Next
              <ChevronRight className="size-4" />
            </Button>
          </div>
        </CardHeader>
        <CardContent>
          {isLoading ? (
            <div className="space-y-3" data-testid="runs-loading">
              {Array.from({ length: 5 }).map((_, index) => (
                <div key={index} className="flex items-center gap-3 rounded-lg border p-3">
                  <div className="h-4 w-32 animate-pulse rounded bg-muted" />
                  <div className="h-4 w-24 animate-pulse rounded bg-muted" />
                  <div className="ml-auto h-5 w-20 animate-pulse rounded-full bg-muted" />
                </div>
              ))}
            </div>
          ) : isError ? (
            <p className="text-sm text-muted-foreground" data-testid="runs-error">
              Unable to load runs. Start the API server to see live data.
            </p>
          ) : !visibleRuns.length ? (
            <div className="flex flex-col items-center gap-2 py-8 text-center" data-testid="runs-empty">
              <Activity className="size-8 text-muted-foreground" />
              <p className="text-sm text-muted-foreground">
                {hasActiveFilters ? 'No runs matched the current filters.' : 'No runs yet.'}
              </p>
            </div>
          ) : (
            <div className="overflow-x-auto">
              <table className="w-full text-sm" data-testid="runs-table">
                <thead>
                  <tr className="border-b text-left text-muted-foreground">
                    <th className="pb-2 font-medium">Ticker</th>
                    <th className="pb-2 font-medium">Strategy</th>
                    <th className="pb-2 font-medium">Status</th>
                    <th className="pb-2 font-medium">Signal</th>
                    <th className="pb-2 font-medium">Started</th>
                    <th className="pb-2 font-medium">Completed</th>
                  </tr>
                </thead>
                <tbody>
                  {visibleRuns.map((run: PipelineRun) => {
                    const strategy = strategiesById.get(run.strategy_id)

                    return (
                      <tr
                        key={run.id}
                        className="cursor-pointer border-b transition-colors hover:bg-secondary/40 focus-within:bg-secondary/40 last:border-0"
                        data-testid={`run-row-${run.id}`}
                        tabIndex={0}
                        onClick={(event) => {
                          if ((event.target as HTMLElement).closest(INTERACTIVE_SELECTOR)) {
                            return
                          }

                          navigate(`/runs/${run.id}`)
                        }}
                        onKeyDown={(event) => {
                          if (
                            event.key !== 'Enter' &&
                            event.key !== ' '
                          ) {
                            return
                          }
                          const interactiveElement = (event.target as HTMLElement).closest(
                            INTERACTIVE_SELECTOR,
                          )

                          if (interactiveElement) {
                            if (event.key === ' ') {
                              event.preventDefault()
                            }
                            return
                          }

                          event.preventDefault()
                          navigate(`/runs/${run.id}`)
                        }}
                      >
                        <td className="py-0 font-medium">
                          <Link
                            to={`/runs/${run.id}`}
                            className="block w-full cursor-pointer py-3 hover:underline focus-visible:underline"
                            data-testid={`run-link-${run.id}`}
                          >
                            {run.ticker}
                          </Link>
                        </td>
                        <td className="py-3 text-muted-foreground">
                          {strategy ? strategyLabel(strategy) : run.strategy_id}
                        </td>
                        <td className="py-3">
                          <RunStatusBadge status={run.status} />
                        </td>
                        <td className="py-3">
                          {run.signal ? <RunSignalBadge signal={run.signal} /> : '—'}
                        </td>
                        <td className="py-3">{formatRunDate(run.started_at)}</td>
                        <td className="py-3">{formatRunDate(run.completed_at)}</td>
                      </tr>
                    )
                  })}
                </tbody>
              </table>
            </div>
          )}
        </CardContent>
      </Card>
    </div>
  )
}
