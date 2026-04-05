import { useQuery } from '@tanstack/react-query';
import { Receipt } from 'lucide-react';
import { useState } from 'react';
import { Link } from 'react-router-dom';

import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card';
import { Input } from '@/components/ui/input';
import { apiClient } from '@/lib/api/client';
import { formatCurrency } from '@/lib/format';

function formatExecutedAt(value?: string | null) {
  if (!value) {
    return '—';
  }

  const date = new Date(value);

  if (Number.isNaN(date.getTime())) {
    return '—';
  }

  return `${date.toLocaleDateString()} ${date.toLocaleTimeString()}`;
}

function formatOptionalCurrency(value?: number | null) {
  if (typeof value !== 'number') {
    return '—';
  }

  return formatCurrency(value);
}

function formatNetTotal(price?: number | null, quantity?: number | null, fee?: number | null) {
  if (typeof price !== 'number' || typeof quantity !== 'number' || typeof fee !== 'number') {
    return '—';
  }

  return formatCurrency(price * quantity - fee);
}

function getTradeKey(
  trade: {
    id?: string | null;
    order_id?: string | null;
    position_id?: string | null;
    executed_at?: string | null;
    created_at?: string | null;
    ticker?: string | null;
    side?: string | null;
    quantity?: number | null;
    price?: number | null;
    fee?: number | null;
  },
  index: number,
) {
  if (trade.id) {
    return `trade-id-${trade.id}`;
  }

  return [
    'trade',
    `index-${index}`,
    trade.order_id ?? 'missing-order-id',
    trade.position_id ?? 'missing-position-id',
    trade.executed_at ?? 'missing-executed-at',
    trade.created_at ?? 'missing-created-at',
    trade.ticker ?? 'missing-ticker',
    trade.side ?? 'missing-side',
    trade.quantity ?? 'missing-quantity',
    trade.price ?? 'missing-price',
    trade.fee ?? 'missing-fee',
  ].join(':');
}

const PAGE_SIZE = 20;

export function TradeHistory() {
  const [offset, setOffset] = useState(0);
  const [tickerFilter, setTickerFilter] = useState('');

  const { data, isLoading, isError } = useQuery({
    queryKey: ['portfolio', 'trades', tickerFilter, offset],
    queryFn: () =>
      apiClient.listTrades({
        ticker: tickerFilter || undefined,
        limit: PAGE_SIZE,
        offset,
      }),
    refetchInterval: 30_000,
  });
  const trades = data?.data ?? [];

  return (
    <Card data-testid="trade-history">
      <CardHeader>
        <CardTitle>Trade history</CardTitle>
        <CardDescription>Recent trade executions</CardDescription>
        <div className="pt-2">
          <Input
            placeholder="Filter by ticker..."
            value={tickerFilter}
            onChange={(e) => {
              setTickerFilter(e.target.value);
              setOffset(0);
            }}
            className="max-w-xs"
          />
        </div>
      </CardHeader>
      <CardContent className="overflow-hidden p-0">
        {isLoading ? (
          <div className="space-y-3 p-4" data-testid="trade-history-loading">
            {Array.from({ length: 5 }).map((_, i) => (
              <div
                key={i}
                className="flex items-center gap-3 rounded-lg border border-border bg-background p-3"
              >
                <div className="h-4 w-20 animate-pulse rounded bg-muted" />
                <div className="h-5 w-12 animate-pulse rounded-full bg-muted" />
                <div className="h-4 w-12 animate-pulse rounded bg-muted" />
                <div className="ml-auto h-4 w-20 animate-pulse rounded bg-muted" />
              </div>
            ))}
          </div>
        ) : isError ? (
          <p className="p-4 text-sm text-muted-foreground" data-testid="trade-history-error">
            Unable to load trades. Start the API server to see live data.
          </p>
        ) : !trades.length ? (
          <div
            className="flex flex-col items-center gap-2 p-8 text-center"
            data-testid="trade-history-empty"
          >
            <Receipt className="size-8 text-muted-foreground" />
            <p className="text-sm text-muted-foreground">No trades yet</p>
          </div>
        ) : (
          <>
            <div className="overflow-x-auto">
              <table className="w-full text-sm">
                <thead>
                  <tr className="border-b border-border text-left font-mono text-[11px] uppercase tracking-[0.16em] text-muted-foreground">
                    <th className="px-4 py-3 font-medium">Date</th>
                    <th className="px-4 py-3 font-medium">Ticker</th>
                    <th className="px-4 py-3 font-medium">Side</th>
                    <th className="px-4 py-3 font-medium text-right">Qty</th>
                    <th className="px-4 py-3 font-medium text-right">Price</th>
                    <th className="px-4 py-3 font-medium text-right">Fee</th>
                    <th className="px-4 py-3 font-medium text-right">Net Total</th>
                  </tr>
                </thead>
                <tbody>
                  {trades.map((trade, index) => {
                    const sideVariant =
                      trade.side === 'buy'
                        ? 'success'
                        : trade.side === 'sell'
                          ? 'destructive'
                          : 'secondary';

                    return (
                      <tr
                        key={getTradeKey(trade, index)}
                        className="border-b border-border transition-colors hover:bg-accent/45 last:border-0"
                      >
                        <td className="px-4 py-3 font-mono text-[13px] text-muted-foreground">
                          {formatExecutedAt(trade.executed_at)}
                        </td>
                        <td className="px-4 py-3 font-mono text-[13px] font-medium tracking-[0.02em]">
                          {trade.ticker ? (
                            <Link to={`/stocks/${trade.ticker}`} className="text-primary hover:underline">
                              {trade.ticker}
                            </Link>
                          ) : (
                            '—'
                          )}
                        </td>
                        <td className="px-4 py-3">
                          <Badge variant={sideVariant}>{trade.side ?? '—'}</Badge>
                        </td>
                        <td className="px-4 py-3 text-right font-mono text-[13px]">
                          {trade.quantity ?? '—'}
                        </td>
                        <td className="px-4 py-3 text-right font-mono text-[13px]">
                          {formatOptionalCurrency(trade.price)}
                        </td>
                        <td className="px-4 py-3 text-right font-mono text-[13px]">
                          {formatOptionalCurrency(trade.fee)}
                        </td>
                        <td className="px-4 py-3 text-right font-mono text-[13px] font-medium">
                          {formatNetTotal(trade.price, trade.quantity, trade.fee)}
                        </td>
                      </tr>
                    );
                  })}
                </tbody>
              </table>
            </div>
            <div className="flex items-center justify-between px-4 py-3">
              <span className="text-xs text-muted-foreground">
                Page {Math.floor(offset / PAGE_SIZE) + 1}
              </span>
              <div className="flex gap-1">
                <Button
                  size="sm"
                  variant="outline"
                  disabled={offset === 0}
                  onClick={() => setOffset((prev) => Math.max(0, prev - PAGE_SIZE))}
                >
                  Prev
                </Button>
                <Button
                  size="sm"
                  variant="outline"
                  disabled={trades.length < PAGE_SIZE}
                  onClick={() => setOffset((prev) => prev + PAGE_SIZE)}
                >
                  Next
                </Button>
              </div>
            </div>
          </>
        )}
      </CardContent>
    </Card>
  );
}
