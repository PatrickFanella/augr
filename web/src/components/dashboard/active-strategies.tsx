import { useQuery } from '@tanstack/react-query'
import { Activity, Clock, Pause } from 'lucide-react'

import { Badge } from '@/components/ui/badge'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { apiClient } from '@/lib/api/client'
import type { Strategy } from '@/lib/api/types'

function MarketTypeBadge({ type }: { type: Strategy['market_type'] }) {
  const variants: Record<Strategy['market_type'], 'default' | 'secondary' | 'outline'> = {
    stock: 'default',
    crypto: 'secondary',
    polymarket: 'outline',
  }

  return <Badge variant={variants[type]}>{type}</Badge>
}

export function ActiveStrategies() {
  const { data, isLoading, isError } = useQuery({
    queryKey: ['strategies', 'active'],
    queryFn: () => apiClient.listStrategies({ is_active: true, limit: 20 }),
    refetchInterval: 30_000,
  })
  const strategies = data?.data ?? []

  return (
    <Card data-testid="active-strategies">
      <CardHeader>
        <CardTitle>Active strategies</CardTitle>
        <CardDescription>Currently enabled trading strategies</CardDescription>
      </CardHeader>
      <CardContent>
        {isLoading ? (
          <div className="space-y-3" data-testid="active-strategies-loading">
            {Array.from({ length: 3 }).map((_, i) => (
              <div key={i} className="flex items-center gap-3 rounded-lg border p-3">
                <div className="h-4 w-32 animate-pulse rounded bg-muted" />
                <div className="ml-auto h-5 w-16 animate-pulse rounded-full bg-muted" />
              </div>
            ))}
          </div>
        ) : isError ? (
          <p className="text-sm text-muted-foreground" data-testid="active-strategies-error">
            Unable to load strategies. Start the API server to see live data.
          </p>
        ) : strategies.length === 0 ? (
          <div className="flex flex-col items-center gap-2 py-8 text-center" data-testid="active-strategies-empty">
            <Pause className="size-8 text-muted-foreground" />
            <p className="text-sm text-muted-foreground">No active strategies</p>
          </div>
        ) : (
          <ul className="space-y-2" data-testid="active-strategies-list">
            {strategies.map((strategy) => (
              <li
                key={strategy.id}
                className="flex items-center gap-3 rounded-lg border p-3 transition-colors hover:bg-secondary/40"
              >
                <div className="flex size-8 shrink-0 items-center justify-center rounded-full bg-primary/10 text-primary">
                  <Activity className="size-4" />
                </div>
                <div className="min-w-0 flex-1">
                  <p className="truncate font-medium">{strategy.name}</p>
                  <p className="text-xs text-muted-foreground">
                    {strategy.ticker}
                    {strategy.schedule_cron ? (
                      <span className="ml-2 inline-flex items-center gap-1">
                        <Clock className="size-3" />
                        scheduled
                      </span>
                    ) : null}
                  </p>
                </div>
                <div className="flex items-center gap-2">
                  <MarketTypeBadge type={strategy.market_type} />
                  {strategy.is_paper ? <Badge variant="warning">paper</Badge> : null}
                </div>
              </li>
            ))}
          </ul>
        )}
      </CardContent>
    </Card>
  )
}
