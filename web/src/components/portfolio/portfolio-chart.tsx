import { useMemo } from 'react';
import { useQuery } from '@tanstack/react-query';
import { LineChart } from 'lucide-react';
import {
  Area,
  AreaChart,
  CartesianGrid,
  ResponsiveContainer,
  Tooltip,
  XAxis,
  YAxis,
} from 'recharts';

import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card';
import { apiClient } from '@/lib/api/client';
import { formatCurrency } from '@/lib/format';

interface EquityPoint {
  date: string;
  pnl: number;
}

export function PortfolioChart() {
  const { data, isLoading, isError } = useQuery({
    queryKey: ['portfolio', 'positions', 'all'],
    queryFn: () => apiClient.listPositions({ limit: 100 }),
    refetchInterval: 30_000,
  });

  const chartData = useMemo<EquityPoint[]>(() => {
    const positions = data?.data ?? [];

    if (!positions.length) return [];

    const closedPositions = positions
      .filter((p) => p.closed_at)
      .sort((a, b) => new Date(a.closed_at!).getTime() - new Date(b.closed_at!).getTime());

    if (!closedPositions.length) return [];

    let cumulative = 0;
    return closedPositions.map((p) => {
      cumulative += p.realized_pnl;
      return {
        date: new Date(p.closed_at!).toLocaleDateString(),
        pnl: cumulative,
      };
    });
  }, [data]);

  return (
    <Card data-testid="portfolio-chart">
      <CardHeader>
        <CardTitle>Equity curve</CardTitle>
        <CardDescription>Cumulative realized P&L over time</CardDescription>
      </CardHeader>
      <CardContent className="h-72">
        {isLoading ? (
          <div
            className="h-full w-full animate-pulse rounded bg-muted"
            data-testid="portfolio-chart-loading"
          />
        ) : isError ? (
          <p className="text-sm text-muted-foreground" data-testid="portfolio-chart-error">
            Unable to load chart data. Start the API server to see live data.
          </p>
        ) : !chartData.length ? (
          <div
            className="flex flex-col items-center gap-2 py-8 text-center"
            data-testid="portfolio-chart-empty"
          >
            <LineChart className="size-8 text-muted-foreground" />
            <p className="text-sm text-muted-foreground">No closed positions to chart</p>
          </div>
        ) : (
          <ResponsiveContainer width="100%" height="100%">
            <AreaChart data={chartData}>
              <CartesianGrid stroke="var(--chart-grid)" strokeDasharray="3 3" />
              <XAxis
                dataKey="date"
                className="text-xs"
                tick={{ fill: 'var(--muted-foreground)', fontSize: 12 }}
                tickLine={false}
                axisLine={false}
              />
              <YAxis
                className="text-xs"
                tick={{ fill: 'var(--muted-foreground)', fontSize: 12 }}
                tickLine={false}
                axisLine={false}
                tickFormatter={(value: number) => formatCurrency(value)}
              />
              <Tooltip
                formatter={(value) => [formatCurrency(Number(value)), 'P&L']}
                contentStyle={{
                  backgroundColor: 'var(--card)',
                  border: '1px solid var(--border)',
                  borderRadius: '0.625rem',
                  color: 'var(--foreground)',
                  fontSize: '0.75rem',
                }}
              />
              <Area
                type="monotone"
                dataKey="pnl"
                stroke="var(--color-success)"
                fill="var(--color-success)"
                fillOpacity={0.15}
                strokeWidth={2}
              />
            </AreaChart>
          </ResponsiveContainer>
        )}
      </CardContent>
    </Card>
  );
}
