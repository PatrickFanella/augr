import { useQuery } from '@tanstack/react-query'
import { ArrowLeft } from 'lucide-react'
import { Link, useParams } from 'react-router-dom'

import { PageHeader } from '@/components/layout/page-header'
import { Badge } from '@/components/ui/badge'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { apiClient } from '@/lib/api/client'
import type { Order, OrderSide, OrderStatus } from '@/lib/api/types'

const STATUS_VARIANTS: Record<OrderStatus, 'default' | 'success' | 'destructive' | 'warning' | 'secondary'> = {
  pending: 'default',
  submitted: 'default',
  partial: 'warning',
  filled: 'success',
  cancelled: 'secondary',
  rejected: 'destructive',
}

const SIDE_VARIANTS: Record<OrderSide, 'success' | 'destructive'> = {
  buy: 'success',
  sell: 'destructive',
}

function formatStatusLabel(status: string) {
  return status.charAt(0).toUpperCase() + status.slice(1)
}

function formatDate(dateStr?: string) {
  if (!dateStr) return '—'
  return new Date(dateStr).toLocaleString(undefined, {
    month: 'short',
    day: 'numeric',
    year: 'numeric',
    hour: '2-digit',
    minute: '2-digit',
  })
}

function getOptionalStringField(record: Record<string, unknown>, key: string): string | undefined {
  const value = record[key]
  return typeof value === 'string' && value.length > 0 ? value : undefined
}

function getOptionalNumberField(record: Record<string, unknown>, key: string): number | undefined {
  const value = record[key]
  return typeof value === 'number' ? value : undefined
}

function getOrderOptionFields(order: Order): {
  optionType?: string
  underlyingTicker?: string
  strike?: number
  expiry?: string
  contractMultiplier?: number
  positionIntent?: string
} {
  const record = order as unknown as Record<string, unknown>
  return {
    optionType: getOptionalStringField(record, 'option_type'),
    underlyingTicker: getOptionalStringField(record, 'underlying_ticker') ?? getOptionalStringField(record, 'underlying'),
    strike: getOptionalNumberField(record, 'strike'),
    expiry: getOptionalStringField(record, 'expiry'),
    contractMultiplier: getOptionalNumberField(record, 'contract_multiplier'),
    positionIntent: getOptionalStringField(record, 'position_intent'),
  }
}

export function OrderDetailPage() {
  const { id } = useParams<{ id: string }>()

  const { data, isLoading, isError } = useQuery({
    queryKey: ['order', id],
    queryFn: () => apiClient.getOrder(id!),
    enabled: !!id,
  })

  if (isLoading) {
    return (
      <div className="space-y-6" data-testid="order-detail-loading">
        <div className="h-8 w-48 animate-pulse rounded bg-muted" />
        <div className="h-64 animate-pulse rounded-lg border bg-muted" />
      </div>
    )
  }

  if (isError || !data) {
    return (
      <div className="space-y-4" data-testid="order-detail-error">
        <Link
          to="/orders"
          className="inline-flex items-center gap-1 text-sm text-muted-foreground hover:text-foreground"
        >
          <ArrowLeft className="size-4" />
          Back to orders
        </Link>
        <Card>
          <CardContent className="py-8">
            <p className="text-center text-sm text-muted-foreground">
              Unable to load order. It may not exist or the API server is unavailable.
            </p>
          </CardContent>
        </Card>
      </div>
    )
  }

  const order = data.order
  const optionFields = getOrderOptionFields(order)

  return (
    <div className="space-y-4" data-testid="order-detail-page">
      <PageHeader
        eyebrow="Order detail"
        title={`${order.ticker} ${order.side} order`}
        description={`${order.order_type} order for ${order.quantity} shares via ${order.broker}`}
        meta={(
          <>
            <Badge variant={STATUS_VARIANTS[order.status]}>{formatStatusLabel(order.status)}</Badge>
            <Badge variant={SIDE_VARIANTS[order.side]}>{order.side}</Badge>
          </>
        )}
        actions={(
          <Link
            to="/orders"
            className="inline-flex items-center gap-1 rounded-md border border-border bg-background px-3 py-2 text-sm text-muted-foreground transition-colors hover:border-primary/25 hover:text-foreground"
          >
            <ArrowLeft className="size-4" />
            Back to orders
          </Link>
        )}
      />

      <Card>
        <CardHeader>
          <CardTitle>Overview</CardTitle>
          <CardDescription>Order summary and current state</CardDescription>
        </CardHeader>
        <CardContent>
          <dl className="grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
            <div>
              <dt className="font-mono text-[11px] uppercase tracking-[0.16em] text-muted-foreground">Ticker</dt>
              <dd className="mt-1 text-sm font-medium">{order.ticker}</dd>
            </div>
            <div>
              <dt className="font-mono text-[11px] uppercase tracking-[0.16em] text-muted-foreground">Side</dt>
              <dd className="mt-1">
                <Badge variant={SIDE_VARIANTS[order.side]}>{order.side}</Badge>
              </dd>
            </div>
            <div>
              <dt className="font-mono text-[11px] uppercase tracking-[0.16em] text-muted-foreground">Type</dt>
              <dd className="mt-1 text-sm font-medium">{order.order_type}</dd>
            </div>
            <div>
              <dt className="font-mono text-[11px] uppercase tracking-[0.16em] text-muted-foreground">Quantity</dt>
              <dd className="mt-1 font-mono text-[13px] font-medium">{order.quantity}</dd>
            </div>
            <div>
              <dt className="font-mono text-[11px] uppercase tracking-[0.16em] text-muted-foreground">Status</dt>
              <dd className="mt-1">
                <Badge variant={STATUS_VARIANTS[order.status]}>{formatStatusLabel(order.status)}</Badge>
              </dd>
            </div>
            <div>
              <dt className="font-mono text-[11px] uppercase tracking-[0.16em] text-muted-foreground">Broker</dt>
              <dd className="mt-1 text-sm font-medium">{order.broker}</dd>
            </div>
            {order.limit_price != null && (
              <div>
                <dt className="font-mono text-[11px] uppercase tracking-[0.16em] text-muted-foreground">Limit price</dt>
                <dd className="mt-1 font-mono text-[13px] font-medium">${Number(order.limit_price).toFixed(2)}</dd>
              </div>
            )}
            {order.stop_price != null && (
              <div>
                <dt className="font-mono text-[11px] uppercase tracking-[0.16em] text-muted-foreground">Stop price</dt>
                <dd className="mt-1 font-mono text-[13px] font-medium">${Number(order.stop_price).toFixed(2)}</dd>
              </div>
            )}
          </dl>
        </CardContent>
      </Card>

      {(order.filled_quantity > 0 || order.filled_avg_price != null || order.filled_at) && (
        <Card>
          <CardHeader>
            <CardTitle>Fill details</CardTitle>
            <CardDescription>Execution information for this order</CardDescription>
          </CardHeader>
          <CardContent>
            <dl className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
              <div>
                <dt className="font-mono text-[11px] uppercase tracking-[0.16em] text-muted-foreground">Filled quantity</dt>
                <dd className="mt-1 font-mono text-[13px] font-medium">{order.filled_quantity}</dd>
              </div>
              {order.filled_avg_price != null && (
                <div>
                  <dt className="font-mono text-[11px] uppercase tracking-[0.16em] text-muted-foreground">Avg fill price</dt>
                  <dd className="mt-1 font-mono text-[13px] font-medium">${Number(order.filled_avg_price).toFixed(2)}</dd>
                </div>
              )}
              {order.filled_at && (
                <div>
                  <dt className="font-mono text-[11px] uppercase tracking-[0.16em] text-muted-foreground">Filled at</dt>
                  <dd className="mt-1 font-mono text-[13px] font-medium">{formatDate(order.filled_at)}</dd>
                </div>
              )}
            </dl>
          </CardContent>
        </Card>
      )}

      {optionFields.optionType && (
        <Card>
          <CardHeader>
            <CardTitle>Options info</CardTitle>
            <CardDescription>Option contract details for this order</CardDescription>
          </CardHeader>
          <CardContent>
            <dl className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
              {optionFields.underlyingTicker && (
                <div>
                  <dt className="font-mono text-[11px] uppercase tracking-[0.16em] text-muted-foreground">Underlying</dt>
                  <dd className="mt-1 text-sm font-medium">{optionFields.underlyingTicker}</dd>
                </div>
              )}
              <div>
                <dt className="font-mono text-[11px] uppercase tracking-[0.16em] text-muted-foreground">Option type</dt>
                <dd className="mt-1 text-sm font-medium">{optionFields.optionType}</dd>
              </div>
              {optionFields.strike != null && (
                <div>
                  <dt className="font-mono text-[11px] uppercase tracking-[0.16em] text-muted-foreground">Strike</dt>
                  <dd className="mt-1 font-mono text-[13px] font-medium">${optionFields.strike.toFixed(2)}</dd>
                </div>
              )}
              {optionFields.expiry && (
                <div>
                  <dt className="font-mono text-[11px] uppercase tracking-[0.16em] text-muted-foreground">Expiry</dt>
                  <dd className="mt-1 font-mono text-[13px] font-medium">{formatDate(optionFields.expiry)}</dd>
                </div>
              )}
              {optionFields.contractMultiplier != null && (
                <div>
                  <dt className="font-mono text-[11px] uppercase tracking-[0.16em] text-muted-foreground">Contract multiplier</dt>
                  <dd className="mt-1 font-mono text-[13px] font-medium">{String(optionFields.contractMultiplier)}</dd>
                </div>
              )}
              {optionFields.positionIntent && (
                <div>
                  <dt className="font-mono text-[11px] uppercase tracking-[0.16em] text-muted-foreground">Position intent</dt>
                  <dd className="mt-1 text-sm font-medium">{optionFields.positionIntent}</dd>
                </div>
              )}
            </dl>
          </CardContent>
        </Card>
      )}

      <Card>
        <CardHeader>
          <CardTitle>Links</CardTitle>
          <CardDescription>Related resources</CardDescription>
        </CardHeader>
        <CardContent>
          <div className="flex flex-wrap gap-4">
            {order.strategy_id && (
              <Link to={`/strategies/${order.strategy_id}`} className="text-primary hover:underline">
                View Strategy
              </Link>
            )}
            {order.pipeline_run_id && (
              <Link to={`/runs/${order.pipeline_run_id}`} className="text-primary hover:underline">
                View Pipeline Run
              </Link>
            )}
          </div>
        </CardContent>
      </Card>
    </div>
  )
}
