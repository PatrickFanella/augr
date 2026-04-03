import { useQuery } from '@tanstack/react-query';
import { Activity, Clock, Pause } from 'lucide-react';

import { Badge } from '@/components/ui/badge';
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card';
import { apiClient } from '@/lib/api/client';
import type { Strategy, StrategyStatus } from '@/lib/api/types';

function MarketTypeBadge({ type }: { type: Strategy['market_type'] }) {
  const variants: Record<Strategy['market_type'], 'default' | 'secondary' | 'outline'> = {
    stock: 'default',
    crypto: 'secondary',
    polymarket: 'outline',
  };

  return <Badge variant={variants[type]}>{type}</Badge>;
}

function resolveStrategyStatus(strategy: Strategy): StrategyStatus {
  if (strategy.status) {
    return strategy.status;
  }

  return strategy.is_active ? 'active' : 'inactive';
}

export function ActiveStrategies() {
  const { data, isLoading, isError } = useQuery({
    queryKey: ['strategies', 'active'],
    queryFn: () => apiClient.listStrategies({ status: 'active', limit: 20 }),
    refetchInterval: 30_000,
  });
  const strategies = (data?.data ?? []).filter(
    (strategy) => resolveStrategyStatus(strategy) === 'active',
  );

  return (
    <Card data-testid="active-strategies">
      <CardHeader>
        <CardTitle>Active strategies</CardTitle>
        <CardDescription>Currently enabled trading strategies</CardDescription>
      </CardHeader>
      <CardContent>
        {isLoading ? (
          <div className="space-y-2.5" data-testid="active-strategies-loading">
            {Array.from({ length: 3 }).map((_, i) => (
              <div
                key={i}
                className="flex items-center gap-3 rounded-lg border border-white/8 bg-background/45 p-3"
              >
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
          <div
            className="flex flex-col items-center gap-2 py-8 text-center"
            data-testid="active-strategies-empty"
          >
            <Pause className="size-8 text-muted-foreground" />
            <p className="text-sm text-muted-foreground">No active strategies</p>
          </div>
        ) : (
          <ul className="space-y-2" data-testid="active-strategies-list">
            {strategies.map((strategy) => (
              <li
                key={strategy.id}
                className="grid gap-3 rounded-lg border border-white/8 bg-background/45 p-3 transition-colors hover:border-primary/15 hover:bg-accent/45 xl:grid-cols-[minmax(0,1fr)_auto] xl:items-center"
              >
                <div className="flex min-w-0 gap-3">
                  <div className="flex size-9 shrink-0 items-center justify-center rounded-md border border-primary/15 bg-primary/10 text-primary">
                    <Activity className="size-4" />
                  </div>
                  <div className="min-w-0 flex-1">
                    <div className="flex flex-wrap items-center gap-2">
                      <p className="truncate font-medium">{strategy.name}</p>
                      <Badge variant="success">active</Badge>
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
                    </div>
                  </div>
                </div>
                <div className="flex items-center gap-2 xl:justify-end">
                  <MarketTypeBadge type={strategy.market_type} />
                  {strategy.is_paper ? <Badge variant="warning">paper</Badge> : null}
                </div>
              </li>
            ))}
          </ul>
        )}
      </CardContent>
    </Card>
  );
}
