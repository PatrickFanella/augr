import { useQuery } from '@tanstack/react-query';
import { DollarSign, TrendingDown, TrendingUp, Wallet } from 'lucide-react';

import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { apiClient } from '@/lib/api/client';
import { formatCurrency } from '@/lib/format';
import { cn } from '@/lib/utils';

export function PortfolioSummary() {
  const { data, isLoading, isError } = useQuery({
    queryKey: ['portfolio', 'summary'],
    queryFn: () => apiClient.getPortfolioSummary(),
    refetchInterval: 30_000,
  });

  if (isLoading) {
    return (
      <div
        className="grid gap-4 sm:grid-cols-2 lg:grid-cols-4"
        data-testid="portfolio-summary-loading"
      >
        {Array.from({ length: 4 }).map((_, i) => (
          <Card key={i}>
            <CardHeader className="flex flex-row items-center justify-between gap-3 pb-3">
              <div className="h-4 w-24 animate-pulse rounded bg-muted" />
              <div className="size-4 animate-pulse rounded bg-muted" />
            </CardHeader>
            <CardContent>
              <div className="h-7 w-28 animate-pulse rounded bg-muted" />
            </CardContent>
          </Card>
        ))}
      </div>
    );
  }

  if (isError || !data) {
    return (
      <Card data-testid="portfolio-summary-error">
        <CardContent className="p-4 text-sm text-muted-foreground">
          Unable to load portfolio summary. Start the API server to see live data.
        </CardContent>
      </Card>
    );
  }

  const totalPnl = data.unrealized_pnl + data.realized_pnl;

  const metrics = [
    {
      label: 'Total P&L',
      value: formatCurrency(totalPnl),
      icon: DollarSign,
      positive: totalPnl >= 0,
    },
    {
      label: 'Unrealized P&L',
      value: formatCurrency(data.unrealized_pnl),
      icon: data.unrealized_pnl >= 0 ? TrendingUp : TrendingDown,
      positive: data.unrealized_pnl >= 0,
    },
    {
      label: 'Realized P&L',
      value: formatCurrency(data.realized_pnl),
      icon: data.realized_pnl >= 0 ? TrendingUp : TrendingDown,
      positive: data.realized_pnl >= 0,
    },
    {
      label: 'Open positions',
      value: String(data.open_positions),
      icon: Wallet,
      positive: null,
    },
  ];

  return (
    <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-4" data-testid="portfolio-summary">
      {metrics.map(({ label, value, icon: Icon, positive }) => (
        <Card key={label}>
          <CardHeader className="flex flex-row items-start justify-between gap-3 pb-3">
            <CardTitle className="font-mono text-[11px] font-medium uppercase tracking-[0.16em] text-muted-foreground">
              {label}
            </CardTitle>
            <div className="flex size-8 items-center justify-center rounded-md border border-white/8 bg-background/55 text-muted-foreground">
              <Icon className="size-4" />
            </div>
          </CardHeader>
          <CardContent className="space-y-1">
            <p
              className={cn(
                'font-mono text-2xl font-semibold tracking-tight',
                positive === true && 'text-success',
                positive === false && 'text-destructive',
              )}
            >
              {value}
            </p>
            <p className="text-xs text-muted-foreground">
              Updated from the current portfolio snapshot.
            </p>
          </CardContent>
        </Card>
      ))}
    </div>
  );
}
