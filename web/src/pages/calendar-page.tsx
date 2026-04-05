import { useQuery } from '@tanstack/react-query'
import { CalendarDays, ExternalLink, Loader2 } from 'lucide-react'
import { useState } from 'react'

import { PageHeader } from '@/components/layout/page-header'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { apiClient } from '@/lib/api/client'
import type { EarningsEvent, EconomicEvent, SECFiling, IPOEvent } from '@/lib/api/types'

type Tab = 'earnings' | 'economic' | 'filings' | 'ipo'

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

  const events: EarningsEvent[] = data ?? []

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

  const events: EconomicEvent[] = data ?? []

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

function FilingsTab() {
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

  const filings: SECFiling[] = data ?? []

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
              </tr>
            </thead>
            <tbody>
              {filings.map((f, i) => (
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
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
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

  const events: IPOEvent[] = data ?? []

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

  return (
    <div className="space-y-4" data-testid="calendar-page">
      <PageHeader
        title="Calendar"
        description="Earnings, economic events, SEC filings, and IPOs."
        meta={<CalendarDays className="size-4 text-muted-foreground" />}
      />

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
          {activeTab === 'filings' && <FilingsTab />}
          {activeTab === 'ipo' && <IPOTab />}
        </CardContent>
      </Card>
    </div>
  )
}
