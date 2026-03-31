import { useQuery } from '@tanstack/react-query'
import { Receipt } from 'lucide-react'

import { Badge } from '@/components/ui/badge'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { apiClient } from '@/lib/api/client'
import { formatCurrency } from '@/lib/format'

function formatExecutedAt(value?: string | null) {
  if (!value) {
    return '—'
  }

  const date = new Date(value)

  if (Number.isNaN(date.getTime())) {
    return '—'
  }

  return `${date.toLocaleDateString()} ${date.toLocaleTimeString()}`
}

function formatOptionalCurrency(value?: number | null) {
  if (typeof value !== 'number') {
    return '—'
  }

  return formatCurrency(value)
}

function formatNetTotal(price?: number | null, quantity?: number | null, fee?: number | null) {
  if (typeof price !== 'number' || typeof quantity !== 'number' || typeof fee !== 'number') {
    return '—'
  }

  return formatCurrency(price * quantity - fee)
}

function getTradeKey(
  trade: {
  id?: string | null
  order_id?: string | null
  position_id?: string | null
  executed_at?: string | null
  created_at?: string | null
  ticker?: string | null
  side?: string | null
  quantity?: number | null
  price?: number | null
  fee?: number | null
  },
  index: number,
) {
  if (trade.id) {
    return `trade-id-${trade.id}`
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
  ].join(':')
}

export function TradeHistory() {
  const { data, isLoading, isError } = useQuery({
    queryKey: ['portfolio', 'trades'],
    queryFn: () => apiClient.listTrades({ limit: 50 }),
    refetchInterval: 30_000,
  })
  const trades = data?.data ?? []

  return (
    <Card data-testid="trade-history">
      <CardHeader>
        <CardTitle>Trade history</CardTitle>
        <CardDescription>Recent trade executions</CardDescription>
      </CardHeader>
      <CardContent>
        {isLoading ? (
          <div className="space-y-3" data-testid="trade-history-loading">
            {Array.from({ length: 5 }).map((_, i) => (
              <div key={i} className="flex items-center gap-3 rounded-lg border p-3">
                <div className="h-4 w-20 animate-pulse rounded bg-muted" />
                <div className="h-5 w-12 animate-pulse rounded-full bg-muted" />
                <div className="h-4 w-12 animate-pulse rounded bg-muted" />
                <div className="ml-auto h-4 w-20 animate-pulse rounded bg-muted" />
              </div>
            ))}
          </div>
        ) : isError ? (
          <p className="text-sm text-muted-foreground" data-testid="trade-history-error">
            Unable to load trades. Start the API server to see live data.
          </p>
        ) : !trades.length ? (
          <div className="flex flex-col items-center gap-2 py-8 text-center" data-testid="trade-history-empty">
            <Receipt className="size-8 text-muted-foreground" />
            <p className="text-sm text-muted-foreground">No trades yet</p>
          </div>
        ) : (
          <div className="overflow-x-auto">
            <table className="w-full text-sm">
              <thead>
                <tr className="border-b text-left text-muted-foreground">
                  <th className="pb-2 font-medium">Date</th>
                  <th className="pb-2 font-medium">Ticker</th>
                  <th className="pb-2 font-medium">Side</th>
                  <th className="pb-2 font-medium text-right">Qty</th>
                  <th className="pb-2 font-medium text-right">Price</th>
                  <th className="pb-2 font-medium text-right">Fee</th>
                  <th className="pb-2 font-medium text-right">Net Total</th>
                </tr>
              </thead>
              <tbody>
                {trades.map((trade, index) => {
                  const sideVariant =
                    trade.side === 'buy'
                      ? 'success'
                      : trade.side === 'sell'
                        ? 'destructive'
                        : 'secondary'

                  return (
                    <tr
                      key={getTradeKey(trade, index)}
                      className="border-b last:border-0 transition-colors hover:bg-secondary/40"
                    >
                      <td className="py-2 text-muted-foreground">
                        {formatExecutedAt(trade.executed_at)}
                      </td>
                      <td className="py-2 font-medium">{trade.ticker ?? '—'}</td>
                      <td className="py-2">
                        <Badge variant={sideVariant}>{trade.side ?? '—'}</Badge>
                      </td>
                      <td className="py-2 text-right">{trade.quantity ?? '—'}</td>
                      <td className="py-2 text-right">{formatOptionalCurrency(trade.price)}</td>
                      <td className="py-2 text-right">{formatOptionalCurrency(trade.fee)}</td>
                      <td className="py-2 text-right font-medium">
                        {formatNetTotal(trade.price, trade.quantity, trade.fee)}
                      </td>
                    </tr>
                  )
                })}
              </tbody>
            </table>
          </div>
        )}
      </CardContent>
    </Card>
  )
}
