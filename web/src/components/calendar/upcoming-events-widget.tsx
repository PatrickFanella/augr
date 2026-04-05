import { useQuery } from '@tanstack/react-query'
import { CalendarDays } from 'lucide-react'

import { Badge } from '@/components/ui/badge'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'

import { apiClient } from '@/lib/api/client'
import type { EarningsEvent, EconomicEvent } from '@/lib/api/types'

interface UpcomingEventsWidgetProps {
  ticker?: string
}

function todayStr(): string {
  return new Date().toISOString().slice(0, 10)
}

function sevenDaysStr(): string {
  const d = new Date()
  d.setDate(d.getDate() + 7)
  return d.toISOString().slice(0, 10)
}

function formatShortDate(iso: string): string {
  if (!iso) return '--'
  return new Date(iso).toLocaleDateString('en-US', { month: 'short', day: 'numeric' })
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

function ImpactBadge({ impact }: { impact: string }) {
  switch (impact.toLowerCase()) {
    case 'high':
      return <Badge variant="destructive">High</Badge>
    case 'medium':
      return <Badge variant="warning">Medium</Badge>
    default:
      return <Badge variant="secondary">{impact || '--'}</Badge>
  }
}

export function UpcomingEventsWidget({ ticker }: UpcomingEventsWidgetProps) {
  const from = todayStr()
  const to = sevenDaysStr()

  const { data: earningsData, isLoading: earningsLoading } = useQuery({
    queryKey: ['upcoming-earnings', from, to],
    queryFn: () => apiClient.getEarningsCalendar({ from, to }),
  })

  const { data: economicData, isLoading: economicLoading } = useQuery({
    queryKey: ['upcoming-economic'],
    queryFn: () => apiClient.getEconomicCalendar(),
  })

  const earnings: EarningsEvent[] = earningsData ?? []
  const economic: EconomicEvent[] = economicData ?? []

  const filteredEarnings = ticker
    ? earnings.filter((e) => e.symbol.toUpperCase() === ticker.toUpperCase())
    : earnings

  const highImpactEconomic = economic.filter((e) => e.impact.toLowerCase() === 'high')

  type EventItem =
    | { kind: 'earnings'; date: string; data: EarningsEvent }
    | { kind: 'economic'; date: string; data: EconomicEvent }

  const items: EventItem[] = [
    ...filteredEarnings.map(
      (e) => ({ kind: 'earnings' as const, date: e.date, data: e }),
    ),
    ...highImpactEconomic.map(
      (e) => ({ kind: 'economic' as const, date: e.time, data: e }),
    ),
  ].sort((a, b) => a.date.localeCompare(b.date))

  const isLoading = earningsLoading || economicLoading

  return (
    <Card>
      <CardHeader>
        <CardTitle className="flex items-center gap-2 text-base">
          <CalendarDays className="size-4 text-muted-foreground" />
          Upcoming events
          {ticker && (
            <span className="font-mono text-xs text-muted-foreground">{ticker.toUpperCase()}</span>
          )}
        </CardTitle>
      </CardHeader>
      <CardContent>
        {isLoading ? (
          <div className="space-y-2">
            {Array.from({ length: 3 }).map((_, i) => (
              <div key={i} className="flex items-center gap-2">
                <div className="h-4 w-16 animate-pulse rounded bg-muted" />
                <div className="h-5 w-12 animate-pulse rounded-full bg-muted" />
                <div className="h-4 w-24 animate-pulse rounded bg-muted" />
              </div>
            ))}
          </div>
        ) : items.length === 0 ? (
          <p className="text-sm text-muted-foreground">No upcoming events in the next 7 days.</p>
        ) : (
          <ul className="space-y-2">
            {items.slice(0, 10).map((item, i) => (
              <li key={i} className="flex flex-wrap items-center gap-2 text-sm">
                <span className="font-mono text-xs text-muted-foreground">
                  {formatShortDate(item.date)}
                </span>
                {item.kind === 'earnings' ? (
                  <>
                    <Badge variant="outline">Earnings</Badge>
                    <span className="font-mono font-medium">{item.data.symbol}</span>
                    <HourBadge hour={item.data.hour} />
                    {item.data.eps_estimate != null && (
                      <span className="text-xs text-muted-foreground">
                        EPS est: {item.data.eps_estimate.toFixed(2)}
                      </span>
                    )}
                  </>
                ) : (
                  <>
                    <ImpactBadge impact={item.data.impact} />
                    <span className="font-medium">{item.data.event}</span>
                    <span className="text-xs text-muted-foreground">{item.data.country}</span>
                  </>
                )}
              </li>
            ))}
          </ul>
        )}
      </CardContent>
    </Card>
  )
}
