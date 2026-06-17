import { useQuery, useMutation } from '@tanstack/react-query'
import { CalendarDays, ExternalLink, Loader2, Sparkles } from 'lucide-react'
import { Fragment, useMemo, useState } from 'react'
import type { Dispatch, SetStateAction } from 'react'
import { Link } from 'react-router-dom'

import { PageHeader } from '@/components/layout/page-header'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { apiClient } from '@/lib/api/client'
import type { SECFiling, FilingAnalysis } from '@/lib/api/types'

type Tab = 'earnings' | 'economic' | 'filings' | 'ipo'

type MonthItem =
  | { kind: 'earnings'; date: string; symbol: string; title: string; href: string }
  | { kind: 'economic'; date: string; title: string }
  | {
      kind: 'filing'
      date: string
      symbol: string
      title: string
      href: string
      filing: SECFiling
    }
  | { kind: 'ipo'; date: string; symbol: string; title: string; href: string }

type LocalNote = {
  id: string
  date: string
  ticker?: string
  title: string
}

function formatDate(iso: string): string {
  if (!iso) return '--'
  return new Date(iso).toLocaleDateString('en-US', {
    month: 'short',
    day: 'numeric',
    year: 'numeric',
  })
}

function formatNum(n?: number): string {
  if (n == null) return '--'
  return n.toLocaleString('en-US', { minimumFractionDigits: 2, maximumFractionDigits: 2 })
}

function defaultFrom(): string {
  return new Date().toISOString().slice(0, 10)
}

function defaultTo(daysAhead: number): string {
  const d = new Date()
  d.setDate(d.getDate() + daysAhead)
  return d.toISOString().slice(0, 10)
}

function monthStartStr(date = new Date()): string {
  return new Date(date.getFullYear(), date.getMonth(), 1).toISOString().slice(0, 10)
}

function monthEndStr(date = new Date()): string {
  return new Date(date.getFullYear(), date.getMonth() + 1, 0).toISOString().slice(0, 10)
}

function monthLabel(date = new Date()): string {
  return date.toLocaleDateString('en-US', { month: 'long', year: 'numeric' })
}

function monthDayKey(date: string): string {
  return new Date(date).toISOString().slice(0, 10)
}

function startOfMonth(date = new Date()): Date {
  return new Date(date.getFullYear(), date.getMonth(), 1)
}

function endOfMonth(date = new Date()): Date {
  return new Date(date.getFullYear(), date.getMonth() + 1, 0)
}

function buildMonthGrid(date = new Date()): Array<Date | null> {
  const first = startOfMonth(date)
  const last = endOfMonth(date)
  const startOffset = first.getDay()
  const days: Array<Date | null> = []

  for (let i = 0; i < startOffset; i += 1) days.push(null)
  for (let day = 1; day <= last.getDate(); day += 1) {
    days.push(new Date(date.getFullYear(), date.getMonth(), day))
  }
  while (days.length % 7 !== 0) days.push(null)
  return days
}

function sortBySoonestDate<T>(items: T[], getDate: (item: T) => string): T[] {
  return [...items].sort((a, b) => {
    const aTime = new Date(getDate(a)).getTime()
    const bTime = new Date(getDate(b)).getTime()

    if (Number.isNaN(aTime) && Number.isNaN(bTime)) return 0
    if (Number.isNaN(aTime)) return 1
    if (Number.isNaN(bTime)) return -1

    return aTime - bTime
  })
}

function epsColor(actual?: number, estimate?: number): string {
  if (actual == null || estimate == null) return ''
  if (actual > estimate) return 'text-emerald-400'
  if (actual < estimate) return 'text-red-400'
  return ''
}

// ---------- Earnings Tab ----------

function EarningsTab() {
  const [from, setFrom] = useState(defaultFrom())
  const [to, setTo] = useState(defaultTo(14))

  const { data, isLoading, isError, refetch } = useQuery({
    queryKey: ['calendar-earnings', from, to],
    queryFn: () => apiClient.getEarningsCalendar({ from, to }),
  })

  const events = useMemo(() => sortBySoonestDate(data ?? [], (event) => event.date), [data])

  return (
    <div className="space-y-3">
      <div className="flex flex-wrap items-end gap-3">
        <label className="space-y-1">
          <span className="text-xs text-muted-foreground">From</span>
          <Input type="date" value={from} onChange={(e) => setFrom(e.target.value)} />
        </label>
        <label className="space-y-1">
          <span className="text-xs text-muted-foreground">To</span>
          <Input type="date" value={to} onChange={(e) => setTo(e.target.value)} />
        </label>
        <Button variant="outline" size="sm" onClick={() => void refetch()}>
          Refresh
        </Button>
      </div>

      {isLoading && <LoadingRows />}
      {isError && <ErrorMessage onRetry={() => void refetch()} />}

      {!isLoading && events.length === 0 && (
        <p className="py-4 text-sm text-muted-foreground">No earnings events found.</p>
      )}

      {events.length > 0 && (
        <div className="overflow-x-auto">
          <table className="w-full text-left text-sm">
            <thead>
              <tr className="border-b border-border text-xs font-medium uppercase tracking-wider text-muted-foreground">
                <th className="px-2 py-2">Symbol</th>
                <th className="px-2 py-2">Date</th>
                <th className="px-2 py-2">Hour</th>
                <th className="px-2 py-2 text-right">EPS Est</th>
                <th className="px-2 py-2 text-right">EPS Actual</th>
                <th className="px-2 py-2 text-right">Rev Est</th>
                <th className="px-2 py-2 text-right">Rev Actual</th>
                <th className="px-2 py-2">Q</th>
              </tr>
            </thead>
            <tbody>
              {events.map((ev, i) => (
                <tr key={`${ev.symbol}-${ev.date}-${i}`} className="border-b border-border/50 hover:bg-accent/30">
                  <td className="px-2 py-1.5 font-mono font-medium">{ev.symbol}</td>
                  <td className="px-2 py-1.5 text-muted-foreground">{formatDate(ev.date)}</td>
                  <td className="px-2 py-1.5">
                    <HourBadge hour={ev.hour} />
                  </td>
                  <td className="px-2 py-1.5 text-right font-mono">{formatNum(ev.eps_estimate)}</td>
                  <td className={`px-2 py-1.5 text-right font-mono ${epsColor(ev.eps_actual, ev.eps_estimate)}`}>
                    {formatNum(ev.eps_actual)}
                  </td>
                  <td className="px-2 py-1.5 text-right font-mono">{formatNum(ev.revenue_estimate)}</td>
                  <td className={`px-2 py-1.5 text-right font-mono ${epsColor(ev.revenue_actual, ev.revenue_estimate)}`}>
                    {formatNum(ev.revenue_actual)}
                  </td>
                  <td className="px-2 py-1.5 text-muted-foreground">Q{ev.quarter} {ev.year}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </div>
  )
}

function HourBadge({ hour }: { hour: string }) {
  switch (hour) {
    case 'bmo':
      return <Badge variant="default">BMO</Badge>
    case 'amc':
      return <Badge variant="warning">AMC</Badge>
    case 'dmh':
      return <Badge variant="secondary">DMH</Badge>
    default:
      return <Badge variant="secondary">{hour || '--'}</Badge>
  }
}

// ---------- Economic Events Tab ----------

function EconomicTab() {
  const { data, isLoading, isError, refetch } = useQuery({
    queryKey: ['calendar-economic'],
    queryFn: () => apiClient.getEconomicCalendar(),
  })

  const events = useMemo(() => sortBySoonestDate(data ?? [], (event) => event.time), [data])

  return (
    <div className="space-y-3">
      {isLoading && <LoadingRows />}
      {isError && <ErrorMessage onRetry={() => void refetch()} />}

      {!isLoading && events.length === 0 && (
        <p className="py-4 text-sm text-muted-foreground">No economic events found.</p>
      )}

      {events.length > 0 && (
        <div className="overflow-x-auto">
          <table className="w-full text-left text-sm">
            <thead>
              <tr className="border-b border-border text-xs font-medium uppercase tracking-wider text-muted-foreground">
                <th className="px-2 py-2">Event</th>
                <th className="px-2 py-2">Country</th>
                <th className="px-2 py-2">Time</th>
                <th className="px-2 py-2">Impact</th>
                <th className="px-2 py-2 text-right">Estimate</th>
                <th className="px-2 py-2 text-right">Actual</th>
                <th className="px-2 py-2 text-right">Previous</th>
                <th className="px-2 py-2">Unit</th>
              </tr>
            </thead>
            <tbody>
              {events.map((ev, i) => (
                <tr key={`${ev.event}-${ev.time}-${i}`} className="border-b border-border/50 hover:bg-accent/30">
                  <td className="px-2 py-1.5 font-medium">{ev.event}</td>
                  <td className="px-2 py-1.5 text-muted-foreground">{ev.country}</td>
                  <td className="px-2 py-1.5 text-muted-foreground">{formatDate(ev.time)}</td>
                  <td className="px-2 py-1.5">
                    <ImpactBadge impact={ev.impact} />
                  </td>
                  <td className="px-2 py-1.5 text-right font-mono">{formatNum(ev.estimate)}</td>
                  <td className="px-2 py-1.5 text-right font-mono">{formatNum(ev.actual)}</td>
                  <td className="px-2 py-1.5 text-right font-mono">{formatNum(ev.previous)}</td>
                  <td className="px-2 py-1.5 text-muted-foreground">{ev.unit}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </div>
  )
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

// ---------- SEC Filings Tab ----------

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

function FilingAnalysisResult({ analysis }: { analysis: FilingAnalysis }) {
  const keyItems = analysis.key_items ?? []

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
      {keyItems.length > 0 && (
        <div>
          <span className="text-xs font-medium uppercase tracking-wider text-muted-foreground">Key items:</span>
          <ul className="ml-4 mt-1 list-disc text-xs text-muted-foreground">
            {keyItems.map((item, i) => (
              <li key={i}>{item}</li>
            ))}
          </ul>
        </div>
      )}
      {analysis.reasoning && (
        <p className="text-xs italic text-muted-foreground">{analysis.reasoning}</p>
      )}
    </div>
  )
}

function FilingsTab({
  analyses,
  setAnalyses,
}: {
  analyses: Record<string, FilingAnalysis>
  setAnalyses: Dispatch<SetStateAction<Record<string, FilingAnalysis>>>
}) {
  const [ticker, setTicker] = useState('')
  const [form, setForm] = useState('')

  const { data, isLoading, isError, refetch } = useQuery({
    queryKey: ['calendar-filings', ticker, form],
    queryFn: () =>
      apiClient.getFilings({
        ticker: ticker || undefined,
        form: form || undefined,
      }),
  })

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

  const filings = useMemo(() => sortBySoonestDate(data ?? [], (filing) => filing.filed_date), [data])

  return (
    <div className="space-y-3">
      <div className="flex flex-wrap items-end gap-3">
        <label className="space-y-1">
          <span className="text-xs text-muted-foreground">Ticker</span>
          <Input
            placeholder="e.g. AAPL"
            value={ticker}
            onChange={(e) => setTicker(e.target.value.toUpperCase())}
            className="w-32"
          />
        </label>
        <label className="space-y-1">
          <span className="text-xs text-muted-foreground">Form Type</span>
          <select
            value={form}
            onChange={(e) => setForm(e.target.value)}
            className="flex h-9 w-32 rounded-md border border-input bg-card px-3 py-1 text-sm text-foreground transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 focus-visible:ring-offset-background"
          >
            <option value="">All</option>
            <option value="10-K">10-K</option>
            <option value="10-Q">10-Q</option>
            <option value="8-K">8-K</option>
          </select>
        </label>
        <Button variant="outline" size="sm" onClick={() => void refetch()}>
          Refresh
        </Button>
      </div>

      {isLoading && <LoadingRows />}
      {isError && <ErrorMessage onRetry={() => void refetch()} />}

      {!isLoading && filings.length === 0 && (
        <p className="py-4 text-sm text-muted-foreground">No filings found.</p>
      )}

      {filings.length > 0 && (
        <div className="overflow-x-auto">
          <table className="w-full text-left text-sm">
            <thead>
              <tr className="border-b border-border text-xs font-medium uppercase tracking-wider text-muted-foreground">
                <th className="px-2 py-2">Symbol</th>
                <th className="px-2 py-2">Form</th>
                <th className="px-2 py-2">Filed</th>
                <th className="px-2 py-2">Report Date</th>
                <th className="px-2 py-2">Link</th>
                <th className="px-2 py-2">Analysis</th>
              </tr>
            </thead>
            <tbody>
              {filings.map((f, i) => {
                const key = filingKey(f)
                const analysis = analyses[key]
                const isAnalyzing = analyzeMutation.isPending && analyzeMutation.variables && filingKey(analyzeMutation.variables) === key
                return (
                  <Fragment key={`${f.access_number}-${i}`}>
                    <tr key={`${f.access_number}-${i}`} className="border-b border-border/50 hover:bg-accent/30">
                      <td className="px-2 py-1.5 font-mono font-medium">{f.symbol}</td>
                      <td className="px-2 py-1.5">
                        <Badge variant="secondary">{f.form}</Badge>
                      </td>
                      <td className="px-2 py-1.5 text-muted-foreground">{formatDate(f.filed_date)}</td>
                      <td className="px-2 py-1.5 text-muted-foreground">{formatDate(f.report_date)}</td>
                      <td className="px-2 py-1.5">
                        {f.url ? (
                          <a
                            href={f.url}
                            target="_blank"
                            rel="noopener noreferrer"
                            className="inline-flex items-center gap-1 text-primary hover:underline"
                          >
                            <ExternalLink className="size-3" />
                            View
                          </a>
                        ) : (
                          '--'
                        )}
                      </td>
                      <td className="px-2 py-1.5">
                        {analysis ? (
                          <div className="flex flex-wrap items-center gap-1.5">
                            <Badge variant="success">Analyzed</Badge>
                            <SentimentBadge sentiment={analysis.sentiment} />
                            <Badge variant="outline">{analysis.impact}</Badge>
                          </div>
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
                      </td>
                    </tr>
                    {analysis && (
                      <tr key={`${f.access_number}-${i}-analysis`} className="border-b border-border/50">
                        <td colSpan={6} className="px-2 py-2">
                          <FilingAnalysisResult analysis={analysis} />
                        </td>
                      </tr>
                    )}
                  </Fragment>
                )
              })}
            </tbody>
          </table>
        </div>
      )}
    </div>
  )
}

function filingKey(f: SECFiling): string {
  return `${f.symbol}-${f.form}-${f.access_number}`
}

function MonthEventItem({ item, analysis }: { item: MonthItem; analysis?: FilingAnalysis }) {
  if (item.kind === 'earnings') {
    return (
      <div className="space-y-1 rounded-md border border-border/50 bg-card/70 p-2 text-xs">
        <div className="flex flex-wrap items-center gap-1.5">
          <Badge variant="outline">Earnings</Badge>
          <Link to={item.href} className="font-mono font-medium text-primary hover:underline">
            {item.symbol}
          </Link>
        </div>
        <p className="text-muted-foreground">{item.title}</p>
      </div>
    )
  }

  if (item.kind === 'economic') {
    return (
      <div className="space-y-1 rounded-md border border-border/50 bg-card/70 p-2 text-xs">
        <Badge variant="secondary">Economic</Badge>
        <p className="text-sm font-medium">{item.title}</p>
      </div>
    )
  }

  if (item.kind === 'ipo') {
    return (
      <div className="space-y-1 rounded-md border border-border/50 bg-card/70 p-2 text-xs">
        <div className="flex flex-wrap items-center gap-1.5">
          <Badge variant="warning">IPO</Badge>
          <Link to={item.href} className="font-mono font-medium text-primary hover:underline">
            {item.symbol}
          </Link>
        </div>
        <p className="text-muted-foreground">{item.title}</p>
      </div>
    )
  }

  return (
    <div className="space-y-2 rounded-md border border-border/50 bg-card/70 p-2 text-xs">
      <div className="flex flex-wrap items-center gap-1.5">
        <Badge variant="secondary">Filing</Badge>
        <Link to={item.href} className="font-mono font-medium text-primary hover:underline">
          {item.symbol}
        </Link>
        <Badge variant="outline">{item.filing.form}</Badge>
        <a
          href={item.filing.url}
          target="_blank"
          rel="noopener noreferrer"
          className="inline-flex items-center gap-1 text-muted-foreground hover:text-primary hover:underline"
        >
          <ExternalLink className="size-3" />
          SEC
        </a>
      </div>
      <div className="flex flex-wrap gap-1">
        {analysis ? (
          <>
            <Badge variant="success">Analyzed</Badge>
            <Badge variant="outline">{analysis.sentiment}</Badge>
            <Badge variant="outline">{analysis.impact}</Badge>
            <Badge variant="outline">{analysis.action.replace(/_/g, ' ')}</Badge>
          </>
        ) : (
          <Badge variant="secondary">Not analyzed</Badge>
        )}
      </div>
      {analysis && <p className="text-muted-foreground">{analysis.summary}</p>}
    </div>
  )
}

function MonthGrid({
  days,
  itemsByDay,
  analyses,
}: {
  days: Array<Date | null>
  itemsByDay: Map<string, MonthItem[]>
  analyses: Record<string, FilingAnalysis>
}) {
  const weekLabels = ['Sun', 'Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat']

  return (
    <div className="space-y-2">
      <div className="grid grid-cols-7 gap-2 text-[11px] uppercase tracking-[0.16em] text-muted-foreground">
        {weekLabels.map((label) => (
          <div key={label} className="px-1">
            {label}
          </div>
        ))}
      </div>
      <div className="grid grid-cols-7 gap-2">
        {days.map((day, index) => {
          if (!day) {
            return <div key={`empty-${index}`} className="min-h-32 rounded-lg border border-dashed border-border/50 bg-muted/10" />
          }

          const key = day.toISOString().slice(0, 10)
          const items = itemsByDay.get(key) ?? []

          return (
            <div key={key} className="min-h-32 rounded-lg border border-border/70 bg-card p-2 shadow-sm">
              <div className="flex items-center justify-between gap-2">
                <span className="text-sm font-medium">{day.getDate()}</span>
                <span className="text-[11px] text-muted-foreground">{items.length} events</span>
              </div>
              <div className="mt-2 space-y-2">
                {items.slice(0, 3).map((item) => (
                  <MonthEventItem
                    key={`${item.kind}-${item.title}-${item.date}`}
                    item={item}
                    analysis={item.kind === 'filing' ? analyses[filingKey(item.filing)] : undefined}
                  />
                ))}
                {items.length > 3 && (
                  <p className="text-[11px] text-muted-foreground">+{items.length - 3} more</p>
                )}
              </div>
            </div>
          )
        })}
      </div>
    </div>
  )
}

// ---------- IPO Tab ----------

function IPOTab() {
  const [from, setFrom] = useState(defaultFrom())
  const [to, setTo] = useState(defaultTo(30))

  const { data, isLoading, isError, refetch } = useQuery({
    queryKey: ['calendar-ipo', from, to],
    queryFn: () => apiClient.getIPOCalendar({ from, to }),
  })

  const events = useMemo(() => sortBySoonestDate(data ?? [], (event) => event.date), [data])

  return (
    <div className="space-y-3">
      <div className="flex flex-wrap items-end gap-3">
        <label className="space-y-1">
          <span className="text-xs text-muted-foreground">From</span>
          <Input type="date" value={from} onChange={(e) => setFrom(e.target.value)} />
        </label>
        <label className="space-y-1">
          <span className="text-xs text-muted-foreground">To</span>
          <Input type="date" value={to} onChange={(e) => setTo(e.target.value)} />
        </label>
        <Button variant="outline" size="sm" onClick={() => void refetch()}>
          Refresh
        </Button>
      </div>

      {isLoading && <LoadingRows />}
      {isError && <ErrorMessage onRetry={() => void refetch()} />}

      {!isLoading && events.length === 0 && (
        <p className="py-4 text-sm text-muted-foreground">No IPO events found.</p>
      )}

      {events.length > 0 && (
        <div className="overflow-x-auto">
          <table className="w-full text-left text-sm">
            <thead>
              <tr className="border-b border-border text-xs font-medium uppercase tracking-wider text-muted-foreground">
                <th className="px-2 py-2">Symbol</th>
                <th className="px-2 py-2">Name</th>
                <th className="px-2 py-2">Exchange</th>
                <th className="px-2 py-2">Date</th>
                <th className="px-2 py-2">Price Range</th>
                <th className="px-2 py-2 text-right">Shares</th>
                <th className="px-2 py-2">Status</th>
              </tr>
            </thead>
            <tbody>
              {events.map((ev, i) => (
                <tr key={`${ev.symbol}-${ev.date}-${i}`} className="border-b border-border/50 hover:bg-accent/30">
                  <td className="px-2 py-1.5 font-mono font-medium">{ev.symbol}</td>
                  <td className="px-2 py-1.5">{ev.name}</td>
                  <td className="px-2 py-1.5 text-muted-foreground">{ev.exchange}</td>
                  <td className="px-2 py-1.5 text-muted-foreground">{formatDate(ev.date)}</td>
                  <td className="px-2 py-1.5 font-mono">{ev.price_range || '--'}</td>
                  <td className="px-2 py-1.5 text-right font-mono">
                    {ev.shares_offered ? ev.shares_offered.toLocaleString() : '--'}
                  </td>
                  <td className="px-2 py-1.5">
                    <StatusBadge status={ev.status} />
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </div>
  )
}

function StatusBadge({ status }: { status: string }) {
  switch (status.toLowerCase()) {
    case 'expected':
      return <Badge variant="default">{status}</Badge>
    case 'priced':
      return <Badge variant="success">{status}</Badge>
    case 'withdrawn':
      return <Badge variant="destructive">{status}</Badge>
    case 'filed':
      return <Badge variant="warning">{status}</Badge>
    default:
      return <Badge variant="secondary">{status || '--'}</Badge>
  }
}

// ---------- Shared ----------

function LoadingRows() {
  return (
    <div className="space-y-3">
      {Array.from({ length: 5 }).map((_, i) => (
        <div key={i} className="flex items-center gap-3 rounded-lg border p-3">
          <div className="h-4 w-16 animate-pulse rounded bg-muted" />
          <div className="h-4 w-20 animate-pulse rounded bg-muted" />
          <div className="h-4 w-20 animate-pulse rounded bg-muted" />
          <div className="ml-auto h-4 w-14 animate-pulse rounded bg-muted" />
        </div>
      ))}
    </div>
  )
}

function ErrorMessage({ onRetry }: { onRetry: () => void }) {
  return (
    <div className="space-y-3">
      <p className="text-sm text-destructive">Failed to load data.</p>
      <Button variant="outline" size="sm" onClick={onRetry}>
        Retry
      </Button>
    </div>
  )
}

// ---------- Main Page ----------

const tabs: { key: Tab; label: string }[] = [
  { key: 'earnings', label: 'Earnings' },
  { key: 'economic', label: 'Economic' },
  { key: 'filings', label: 'SEC Filings' },
  { key: 'ipo', label: 'IPO' },
]

export function CalendarPage() {
  const [activeTab, setActiveTab] = useState<Tab>('earnings')
  const [filingAnalyses, setFilingAnalyses] = useState<Record<string, FilingAnalysis>>({})
  const [localNotes, setLocalNotes] = useState<LocalNote[]>([])
  const [noteDate, setNoteDate] = useState(defaultFrom())
  const [noteTicker, setNoteTicker] = useState('')
  const [noteTitle, setNoteTitle] = useState('')

  const overviewFrom = monthStartStr()
  const overviewTo = monthEndStr()

  const monthEarningsQuery = useQuery({
    queryKey: ['calendar-month-earnings', overviewFrom, overviewTo],
    queryFn: () => apiClient.getEarningsCalendar({ from: overviewFrom, to: overviewTo }),
  })

  const monthEconomicQuery = useQuery({
    queryKey: ['calendar-month-economic'],
    queryFn: () => apiClient.getEconomicCalendar(),
    staleTime: 60_000,
  })

  const monthFilingsQuery = useQuery({
    queryKey: ['calendar-month-filings', overviewFrom, overviewTo],
    queryFn: () => apiClient.getFilings({ from: overviewFrom, to: overviewTo }),
  })

  const monthIpoQuery = useQuery({
    queryKey: ['calendar-month-ipo', overviewFrom, overviewTo],
    queryFn: () => apiClient.getIPOCalendar({ from: overviewFrom, to: overviewTo }),
  })

  const monthDays = useMemo(() => buildMonthGrid(new Date()), [])

  const monthItemsByDay = useMemo(() => {
    const month = new Date().getMonth()
    const year = new Date().getFullYear()
    const map = new Map<string, MonthItem[]>()

    const addItem = (date: string, item: MonthItem) => {
      const key = monthDayKey(date)
      const existing = map.get(key)
      if (existing) existing.push(item)
      else map.set(key, [item])
    }

    ;(monthEarningsQuery.data ?? [])
      .filter((event) => {
        const d = new Date(event.date)
        return d.getFullYear() === year && d.getMonth() === month
      })
      .forEach((event) => {
        addItem(event.date, {
          kind: 'earnings',
          date: event.date,
          symbol: event.symbol,
          title: `${event.hour.toUpperCase()} · Q${event.quarter} ${event.year}`,
          href: `/stocks/${event.symbol}`,
        })
      })

    ;(monthEconomicQuery.data ?? [])
      .filter((event) => {
        const d = new Date(event.time)
        return d.getFullYear() === year && d.getMonth() === month
      })
      .forEach((event) => {
        addItem(event.time, {
          kind: 'economic',
          date: event.time,
          title: `${event.event} · ${event.country} · ${event.impact}`,
        })
      })

    ;(monthFilingsQuery.data ?? [])
      .filter((filing) => {
        const d = new Date(filing.filed_date)
        return d.getFullYear() === year && d.getMonth() === month
      })
      .forEach((filing) => {
        addItem(filing.filed_date, {
          kind: 'filing',
          date: filing.filed_date,
          symbol: filing.symbol,
          title: `${filing.form} filed ${formatDate(filing.filed_date)}`,
          href: `/stocks/${filing.symbol}`,
          filing,
        })
      })

    ;(monthIpoQuery.data ?? []).forEach((event) => {
      addItem(event.date, {
        kind: 'ipo',
        date: event.date,
        symbol: event.symbol,
        title: `${event.name} · ${event.status}`,
        href: `/stocks/${event.symbol}`,
      })
    })

    return map
  }, [monthEarningsQuery.data, monthEconomicQuery.data, monthFilingsQuery.data, monthIpoQuery.data])

  const monthMetrics = useMemo(() => {
    const filings = monthFilingsQuery.data ?? []
    const analyzed = filings.filter((filing) => filingKey(filing) in filingAnalyses).length
    return {
      earnings: monthEarningsQuery.data?.length ?? 0,
      economic: monthEconomicQuery.data?.filter((event) => {
        const d = new Date(event.time)
        const now = new Date()
        return d.getMonth() === now.getMonth() && d.getFullYear() === now.getFullYear()
      }).length ?? 0,
      filings: filings.filter((filing) => {
        const d = new Date(filing.filed_date)
        const now = new Date()
        return d.getMonth() === now.getMonth() && d.getFullYear() === now.getFullYear()
      }).length,
      ipo: monthIpoQuery.data?.length ?? 0,
      analyzed,
    }
  }, [filingAnalyses, monthEconomicQuery.data, monthEarningsQuery.data, monthFilingsQuery.data, monthIpoQuery.data])

  const addLocalNote = () => {
    const trimmedTitle = noteTitle.trim()
    if (!trimmedTitle) return

    setLocalNotes((prev) => [
      {
        id: globalThis.crypto?.randomUUID?.() ?? `${Date.now()}-${prev.length}`,
        date: noteDate,
        ticker: noteTicker.trim() ? noteTicker.trim().toUpperCase() : undefined,
        title: trimmedTitle,
      },
      ...prev,
    ])
    setNoteTitle('')
    setNoteTicker('')
  }

  const overviewLoading =
    monthEarningsQuery.isLoading || monthEconomicQuery.isLoading || monthFilingsQuery.isLoading || monthIpoQuery.isLoading

  return (
    <div className="space-y-4" data-testid="calendar-page">
      <PageHeader
        title="Calendar"
        description="Earnings, economic events, SEC filings, and IPOs."
        meta={<CalendarDays className="size-4 text-muted-foreground" />}
      />

      <Card>
        <CardHeader>
          <CardTitle className="flex flex-wrap items-center gap-2">
            <span>This month at a glance</span>
            <Badge variant="secondary">{monthLabel()}</Badge>
          </CardTitle>
          <p className="text-sm text-muted-foreground">
            Stock links, SEC links, and filing analysis badges are shown inline. Local notes stay in this browser session only.
          </p>
        </CardHeader>
        <CardContent className="space-y-4">
          <div className="flex flex-wrap gap-2 text-xs">
            <Badge variant="secondary">{monthMetrics.earnings} earnings</Badge>
            <Badge variant="secondary">{monthMetrics.economic} economic</Badge>
            <Badge variant="secondary">{monthMetrics.filings} filings</Badge>
            <Badge variant="secondary">{monthMetrics.ipo} IPOs</Badge>
            <Badge variant="success">{monthMetrics.analyzed} filings analyzed locally</Badge>
          </div>

          {overviewLoading && <p className="text-xs text-muted-foreground">Loading month summary…</p>}

          <div className="grid gap-4 xl:grid-cols-[minmax(0,1.5fr)_minmax(320px,0.9fr)]">
            <MonthGrid days={monthDays} itemsByDay={monthItemsByDay} analyses={filingAnalyses} />

            <div className="space-y-3">
              <Card className="border-border/70 bg-card/80">
                <CardHeader className="pb-3">
                  <CardTitle className="text-base">Local session note</CardTitle>
                </CardHeader>
                <CardContent className="space-y-3">
                  <p className="text-xs text-muted-foreground">
                    This is a local-only placeholder for event notes. It is not saved to the backend.
                  </p>
                  <div className="grid gap-2">
                    <label className="space-y-1">
                      <span className="text-xs text-muted-foreground">Date</span>
                      <Input type="date" value={noteDate} onChange={(e) => setNoteDate(e.target.value)} />
                    </label>
                    <label className="space-y-1">
                      <span className="text-xs text-muted-foreground">Ticker (optional)</span>
                      <Input
                        placeholder="AAPL"
                        value={noteTicker}
                        onChange={(e) => setNoteTicker(e.target.value.toUpperCase())}
                      />
                    </label>
                    <label className="space-y-1">
                      <span className="text-xs text-muted-foreground">Note</span>
                      <Input
                        placeholder="Why this event matters"
                        value={noteTitle}
                        onChange={(e) => setNoteTitle(e.target.value)}
                      />
                    </label>
                  </div>
                  <Button size="sm" variant="outline" onClick={addLocalNote} disabled={!noteTitle.trim()}>
                    Add local note
                  </Button>
                </CardContent>
              </Card>

              <div className="space-y-2 rounded-lg border border-border/70 bg-muted/10 p-3 text-sm">
                <div className="flex items-center justify-between gap-2">
                  <span className="font-medium">Session notes</span>
                  <Badge variant="outline">{localNotes.length}</Badge>
                </div>
                {localNotes.length === 0 ? (
                  <p className="text-xs text-muted-foreground">No local notes yet.</p>
                ) : (
                  <ul className="space-y-2">
                    {localNotes.map((note) => (
                      <li key={note.id} className="rounded-md border border-border/50 bg-card/80 p-2">
                        <div className="flex flex-wrap items-center gap-2">
                          <span className="text-xs text-muted-foreground">{formatDate(note.date)}</span>
                          {note.ticker ? (
                            <Link to={`/stocks/${note.ticker}`} className="font-mono text-xs text-primary hover:underline">
                              {note.ticker}
                            </Link>
                          ) : null}
                        </div>
                        <p className="mt-1 text-sm">{note.title}</p>
                      </li>
                    ))}
                  </ul>
                )}
              </div>
            </div>
          </div>
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>
            <div className="flex gap-1">
              {tabs.map((tab) => (
                <button
                  key={tab.key}
                  onClick={() => setActiveTab(tab.key)}
                  className={`rounded-md px-3 py-1.5 text-sm font-medium transition-colors ${
                    activeTab === tab.key
                      ? 'bg-primary/14 text-foreground'
                      : 'text-muted-foreground hover:bg-accent/70 hover:text-foreground'
                  }`}
                >
                  {tab.label}
                </button>
              ))}
            </div>
          </CardTitle>
        </CardHeader>
        <CardContent>
          {activeTab === 'earnings' && <EarningsTab />}
          {activeTab === 'economic' && <EconomicTab />}
          {activeTab === 'filings' && (
            <FilingsTab analyses={filingAnalyses} setAnalyses={setFilingAnalyses} />
          )}
          {activeTab === 'ipo' && <IPOTab />}
        </CardContent>
      </Card>
    </div>
  )
}
