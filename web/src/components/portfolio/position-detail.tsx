import { X } from 'lucide-react'

import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import type { Position } from '@/lib/api/types'
import { formatCurrency } from '@/lib/format'
import { cn } from '@/lib/utils'

interface PositionDetailProps {
  position: Position
  onClose: () => void
}

export function PositionDetail({ position, onClose }: PositionDetailProps) {
  return (
    <Card data-testid="position-detail">
      <CardHeader className="flex flex-row items-center justify-between">
        <div className="flex items-center gap-2">
          <CardTitle>{position.ticker}</CardTitle>
          <Badge variant={position.side === 'long' ? 'success' : 'destructive'}>
            {position.side}
          </Badge>
        </div>
        <Button variant="ghost" size="sm" onClick={onClose} aria-label="Close position details">
          <X className="size-4" />
        </Button>
      </CardHeader>
      <CardContent>
        <dl className="grid gap-3 sm:grid-cols-2 text-sm">
          <div>
            <dt className="text-muted-foreground">Entry price</dt>
            <dd className="font-medium">{formatCurrency(position.avg_entry)}</dd>
          </div>
          <div>
            <dt className="text-muted-foreground">Current price</dt>
            <dd className="font-medium">
              {position.current_price != null ? formatCurrency(position.current_price) : '—'}
            </dd>
          </div>
          <div>
            <dt className="text-muted-foreground">Quantity</dt>
            <dd className="font-medium">{position.quantity}</dd>
          </div>
          <div>
            <dt className="text-muted-foreground">Unrealized P&L</dt>
            <dd
              className={cn(
                'font-medium',
                position.unrealized_pnl != null && position.unrealized_pnl >= 0 && 'text-emerald-600',
                position.unrealized_pnl != null && position.unrealized_pnl < 0 && 'text-red-600',
              )}
            >
              {position.unrealized_pnl != null ? formatCurrency(position.unrealized_pnl) : '—'}
            </dd>
          </div>
          <div>
            <dt className="text-muted-foreground">Realized P&L</dt>
            <dd
              className={cn(
                'font-medium',
                position.realized_pnl >= 0 && 'text-emerald-600',
                position.realized_pnl < 0 && 'text-red-600',
              )}
            >
              {formatCurrency(position.realized_pnl)}
            </dd>
          </div>
          <div>
            <dt className="text-muted-foreground">Stop loss</dt>
            <dd className="font-medium">
              {position.stop_loss != null ? formatCurrency(position.stop_loss) : '—'}
            </dd>
          </div>
          <div>
            <dt className="text-muted-foreground">Take profit</dt>
            <dd className="font-medium">
              {position.take_profit != null ? formatCurrency(position.take_profit) : '—'}
            </dd>
          </div>
          <div>
            <dt className="text-muted-foreground">Opened at</dt>
            <dd className="font-medium">
              {new Date(position.opened_at).toLocaleDateString()}{' '}
              {new Date(position.opened_at).toLocaleTimeString()}
            </dd>
          </div>
          {position.closed_at ? (
            <div>
              <dt className="text-muted-foreground">Closed at</dt>
              <dd className="font-medium">
                {new Date(position.closed_at).toLocaleDateString()}{' '}
                {new Date(position.closed_at).toLocaleTimeString()}
              </dd>
            </div>
          ) : null}
          {position.strategy_id ? (
            <div>
              <dt className="text-muted-foreground">Strategy ID</dt>
              <dd className="font-medium font-mono text-xs">{position.strategy_id}</dd>
            </div>
          ) : null}
        </dl>
      </CardContent>
    </Card>
  )
}
