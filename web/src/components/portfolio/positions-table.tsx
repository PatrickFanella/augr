import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { BarChart3 } from 'lucide-react'

import { Badge } from '@/components/ui/badge'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { apiClient } from '@/lib/api/client'
import type { Position } from '@/lib/api/types'
import { formatCurrency } from '@/lib/format'
import { cn } from '@/lib/utils'

import { PositionDetail } from '@/components/portfolio/position-detail'

export function PositionsTable() {
  const [selectedPosition, setSelectedPosition] = useState<Position | null>(null)

  const { data, isLoading, isError } = useQuery({
    queryKey: ['portfolio', 'positions', 'open'],
    queryFn: () => apiClient.getOpenPositions({ limit: 50 }),
    refetchInterval: 15_000,
  })

  if (selectedPosition) {
    return (
      <PositionDetail
        position={selectedPosition}
        onClose={() => setSelectedPosition(null)}
      />
    )
  }

  return (
    <Card data-testid="positions-table">
      <CardHeader>
        <CardTitle>Open positions</CardTitle>
        <CardDescription>Currently held positions across all strategies</CardDescription>
      </CardHeader>
      <CardContent>
        {isLoading ? (
          <div className="space-y-3" data-testid="positions-table-loading">
            {Array.from({ length: 5 }).map((_, i) => (
              <div key={i} className="flex items-center gap-3 rounded-lg border p-3">
                <div className="h-4 w-16 animate-pulse rounded bg-muted" />
                <div className="h-5 w-14 animate-pulse rounded-full bg-muted" />
                <div className="h-4 w-12 animate-pulse rounded bg-muted" />
                <div className="h-4 w-20 animate-pulse rounded bg-muted" />
                <div className="ml-auto h-4 w-24 animate-pulse rounded bg-muted" />
              </div>
            ))}
          </div>
        ) : isError ? (
          <p className="text-sm text-muted-foreground" data-testid="positions-table-error">
            Unable to load positions. Start the API server to see live data.
          </p>
        ) : !data?.data.length ? (
          <div className="flex flex-col items-center gap-2 py-8 text-center" data-testid="positions-table-empty">
            <BarChart3 className="size-8 text-muted-foreground" />
            <p className="text-sm text-muted-foreground">No open positions</p>
          </div>
        ) : (
          <div className="overflow-x-auto">
            <table className="w-full text-sm">
              <thead>
                <tr className="border-b text-left text-muted-foreground">
                  <th className="pb-2 font-medium">Ticker</th>
                  <th className="pb-2 font-medium">Side</th>
                  <th className="pb-2 font-medium text-right">Qty</th>
                  <th className="pb-2 font-medium text-right">Entry</th>
                  <th className="pb-2 font-medium text-right">Current</th>
                  <th className="pb-2 font-medium text-right">Unrealized P&L</th>
                  <th className="pb-2 font-medium text-right">Stop Loss</th>
                  <th className="pb-2 font-medium text-right">Take Profit</th>
                </tr>
              </thead>
              <tbody>
                {data.data.map((position) => (
                  <tr
                    key={position.id}
                    className="border-b last:border-0 transition-colors hover:bg-secondary/40 cursor-pointer"
                    tabIndex={0}
                    role="button"
                    onClick={() => setSelectedPosition(position)}
                    onKeyDown={(e) => {
                      if (e.key === 'Enter' || e.key === ' ') {
                        e.preventDefault()
                        setSelectedPosition(position)
                      }
                    }}
                  >
                    <td className="py-2 font-medium">{position.ticker}</td>
                    <td className="py-2">
                      <Badge variant={position.side === 'long' ? 'success' : 'destructive'}>
                        {position.side}
                      </Badge>
                    </td>
                    <td className="py-2 text-right">{position.quantity}</td>
                    <td className="py-2 text-right">{formatCurrency(position.avg_entry)}</td>
                    <td className="py-2 text-right">
                      {position.current_price != null ? formatCurrency(position.current_price) : '—'}
                    </td>
                    <td
                      className={cn(
                        'py-2 text-right font-medium',
                        position.unrealized_pnl != null && position.unrealized_pnl >= 0 && 'text-emerald-600',
                        position.unrealized_pnl != null && position.unrealized_pnl < 0 && 'text-red-600',
                      )}
                    >
                      {position.unrealized_pnl != null ? formatCurrency(position.unrealized_pnl) : '—'}
                    </td>
                    <td className="py-2 text-right">
                      {position.stop_loss != null ? formatCurrency(position.stop_loss) : '—'}
                    </td>
                    <td className="py-2 text-right">
                      {position.take_profit != null ? formatCurrency(position.take_profit) : '—'}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </CardContent>
    </Card>
  )
}
