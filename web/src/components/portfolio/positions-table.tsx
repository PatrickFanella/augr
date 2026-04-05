import { useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { BarChart3 } from 'lucide-react';
import { Link } from 'react-router-dom';

import { Badge } from '@/components/ui/badge';
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card';
import { apiClient } from '@/lib/api/client';
import type { Position } from '@/lib/api/types';
import { formatCurrency } from '@/lib/format';
import { cn } from '@/lib/utils';

import { PositionDetail } from '@/components/portfolio/position-detail';

export function PositionsTable() {
  const [selectedPosition, setSelectedPosition] = useState<Position | null>(null);

  const { data, isLoading, isError } = useQuery({
    queryKey: ['portfolio', 'positions', 'open'],
    queryFn: () => apiClient.getOpenPositions({ limit: 50 }),
    refetchInterval: 15_000,
  });
  const positions = data?.data ?? [];

  if (selectedPosition) {
    return <PositionDetail position={selectedPosition} onClose={() => setSelectedPosition(null)} />;
  }

  return (
    <Card data-testid="positions-table">
      <CardHeader>
        <CardTitle>Open positions</CardTitle>
        <CardDescription>Currently held positions across all strategies</CardDescription>
      </CardHeader>
      <CardContent className="overflow-hidden p-0">
        {isLoading ? (
          <div className="space-y-3 p-4" data-testid="positions-table-loading">
            {Array.from({ length: 5 }).map((_, i) => (
              <div
                key={i}
                className="flex items-center gap-3 rounded-lg border border-border bg-background p-3"
              >
                <div className="h-4 w-16 animate-pulse rounded bg-muted" />
                <div className="h-5 w-14 animate-pulse rounded-full bg-muted" />
                <div className="h-4 w-12 animate-pulse rounded bg-muted" />
                <div className="h-4 w-20 animate-pulse rounded bg-muted" />
                <div className="ml-auto h-4 w-24 animate-pulse rounded bg-muted" />
              </div>
            ))}
          </div>
        ) : isError ? (
          <p className="p-4 text-sm text-muted-foreground" data-testid="positions-table-error">
            Unable to load positions. Start the API server to see live data.
          </p>
        ) : !positions.length ? (
          <div
            className="flex flex-col items-center gap-2 p-8 text-center"
            data-testid="positions-table-empty"
          >
            <BarChart3 className="size-8 text-muted-foreground" />
            <p className="text-sm text-muted-foreground">No open positions</p>
          </div>
        ) : (
          <div className="overflow-x-auto">
            <table className="w-full text-sm">
              <thead>
                <tr className="border-b border-border text-left font-mono text-[11px] uppercase tracking-[0.16em] text-muted-foreground">
                  <th className="px-4 py-3 font-medium">Ticker</th>
                  <th className="px-4 py-3 font-medium">Side</th>
                  <th className="px-4 py-3 font-medium text-right">Qty</th>
                  <th className="px-4 py-3 font-medium text-right">Entry</th>
                  <th className="px-4 py-3 font-medium text-right">Current</th>
                  <th className="px-4 py-3 font-medium text-right">Unrealized P&L</th>
                  <th className="px-4 py-3 font-medium text-right">Stop Loss</th>
                  <th className="px-4 py-3 font-medium text-right">Take Profit</th>
                  <th className="px-4 py-3 font-medium" />
                </tr>
              </thead>
              <tbody>
                {positions.map((position) => (
                  <tr
                    key={position.id}
                    className="cursor-pointer border-b border-border transition-colors hover:bg-accent/45 last:border-0"
                    tabIndex={0}
                    role="button"
                    onClick={() => setSelectedPosition(position)}
                    onKeyDown={(e) => {
                      if (e.key === 'Enter' || e.key === ' ') {
                        e.preventDefault();
                        setSelectedPosition(position);
                      }
                    }}
                  >
                    <td className="px-4 py-3 font-mono text-[13px] font-medium tracking-[0.02em]">
                      <Link
                        to={`/stocks/${position.ticker}`}
                        className="text-primary hover:underline"
                        onClick={(e) => e.stopPropagation()}
                      >
                        {position.ticker}
                      </Link>
                    </td>
                    <td className="px-4 py-3">
                      <Badge variant={position.side === 'long' ? 'success' : 'destructive'}>
                        {position.side}
                      </Badge>
                    </td>
                    <td className="px-4 py-3 text-right font-mono text-[13px]">
                      {position.quantity}
                    </td>
                    <td className="px-4 py-3 text-right font-mono text-[13px]">
                      {formatCurrency(position.avg_entry)}
                    </td>
                    <td className="px-4 py-3 text-right font-mono text-[13px]">
                      {position.current_price != null
                        ? formatCurrency(position.current_price)
                        : '—'}
                    </td>
                    <td
                      className={cn(
                        'px-4 py-3 text-right font-mono text-[13px] font-medium',
                        position.unrealized_pnl != null &&
                          position.unrealized_pnl >= 0 &&
                          'text-success',
                        position.unrealized_pnl != null &&
                          position.unrealized_pnl < 0 &&
                          'text-destructive',
                      )}
                    >
                      {position.unrealized_pnl != null
                        ? formatCurrency(position.unrealized_pnl)
                        : '—'}
                    </td>
                    <td className="px-4 py-3 text-right font-mono text-[13px]">
                      {position.stop_loss != null ? formatCurrency(position.stop_loss) : '—'}
                    </td>
                    <td className="px-4 py-3 text-right font-mono text-[13px]">
                      {position.take_profit != null ? formatCurrency(position.take_profit) : '—'}
                    </td>
                    <td className="px-4 py-3">
                      {position.strategy_id ? (
                        <Link
                          to={`/strategies/${position.strategy_id}`}
                          className="text-primary hover:underline text-xs"
                          onClick={(e) => e.stopPropagation()}
                        >
                          Strategy
                        </Link>
                      ) : null}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </CardContent>
    </Card>
  );
}
