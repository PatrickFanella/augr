import { useQuery } from '@tanstack/react-query'
import { ChevronLeft, ChevronRight, Receipt, Search } from 'lucide-react'
import { useMemo, useState } from 'react'
import { Link, useNavigate } from 'react-router-dom'

import { PageHeader } from '@/components/layout/page-header'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { apiClient } from '@/lib/api/client'
import type { Order, OrderSide, OrderStatus } from '@/lib/api/types'

const PAGE_SIZE = 20
const PAGE_REQUEST_SIZE = PAGE_SIZE + 1
const STATUS_OPTIONS: OrderStatus[] = ['pending', 'submitted', 'partial', 'filled', 'cancelled', 'rejected']
const SIDE_OPTIONS: OrderSide[] = ['buy', 'sell']
const INTERACTIVE_SELECTOR = 'a, button, input, select, textarea, [role="button"], [role="link"]'

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

export function OrdersPage() {
  const navigate = useNavigate()
  const [draftTicker, setDraftTicker] = useState('')
  const [draftStatus, setDraftStatus] = useState<OrderStatus | ''>('')
  const [draftSide, setDraftSide] = useState<OrderSide | ''>('')
  const [ticker, setTicker] = useState('')
  const [status, setStatus] = useState<OrderStatus | ''>('')
  const [side, setSide] = useState<OrderSide | ''>('')
  const [offset, setOffset] = useState(0)

  const { data, isLoading, isError, refetch } = useQuery({
    queryKey: ['orders', ticker, status, side, offset],
    queryFn: () =>
      apiClient.listOrders({
        ticker: ticker || undefined,
        status: status || undefined,
        side: side || undefined,
        limit: PAGE_REQUEST_SIZE,
        offset,
      }),
    refetchInterval: 15_000,
    refetchIntervalInBackground: false,
  })

  const visibleOrders = (data?.data ?? []).slice(0, PAGE_SIZE)
  const visibleCount = visibleOrders.length
  const hasNextPage = (data?.data?.length ?? 0) > PAGE_SIZE
  const pageLabel = useMemo(() => Math.floor(offset / PAGE_SIZE) + 1, [offset])
  const hasActiveFilters = Boolean(ticker || status || side)

  function applyFilters() {
    setOffset(0)
    setTicker(draftTicker)
    setStatus(draftStatus)
    setSide(draftSide)
  }

  function clearFilters() {
    setDraftTicker('')
    setDraftStatus('')
    setDraftSide('')
    setTicker('')
    setStatus('')
    setSide('')
    setOffset(0)
  }

  return (
    <div className="space-y-4" data-testid="orders-page">
      <PageHeader
        eyebrow="Trading"
        title="Orders"
        description="View and filter order history across all strategies and brokers."
      />

      <Card>
        <CardHeader>
          <CardTitle>Filter orders</CardTitle>
          <CardDescription>Narrow down orders by ticker, status, or side.</CardDescription>
        </CardHeader>
        <CardContent>
          <form
            className="grid gap-3 lg:grid-cols-[minmax(0,1fr)_180px_140px_auto]"
            onSubmit={(event) => {
              event.preventDefault()
              applyFilters()
            }}
          >
            <Input
              value={draftTicker}
              onChange={(event) => setDraftTicker(event.target.value)}
              placeholder="Search ticker…"
              aria-label="Ticker"
            />
            <select
              value={draftStatus}
              onChange={(event) => setDraftStatus(event.target.value as OrderStatus | '')}
              aria-label="Status"
              className="flex h-9 w-full rounded-md border border-input bg-card px-3 py-1 text-sm text-foreground transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 focus-visible:ring-offset-background"
            >
              <option value="">All statuses</option>
              {STATUS_OPTIONS.map((option) => (
                <option key={option} value={option}>
                  {formatStatusLabel(option)}
                </option>
              ))}
            </select>
            <select
              value={draftSide}
              onChange={(event) => setDraftSide(event.target.value as OrderSide | '')}
              aria-label="Side"
              className="flex h-9 w-full rounded-md border border-input bg-card px-3 py-1 text-sm text-foreground transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 focus-visible:ring-offset-background"
            >
              <option value="">All sides</option>
              {SIDE_OPTIONS.map((option) => (
                <option key={option} value={option}>
                  {formatStatusLabel(option)}
                </option>
              ))}
            </select>
            <div className="flex gap-2">
              <Button type="submit" data-testid="apply-order-filters">
                <Search className="size-4" />
                Apply
              </Button>
              <Button type="button" variant="outline" onClick={clearFilters}>
                Clear
              </Button>
            </div>
          </form>
        </CardContent>
      </Card>

      <Card>
        <CardHeader className="gap-3 sm:flex-row sm:items-end sm:justify-between">
          <div className="space-y-1.5">
            <CardTitle>Order history</CardTitle>
            <CardDescription>
              {isLoading
                ? 'Loading…'
                : isError
                  ? 'Unable to load orders'
                  : visibleCount
                    ? `Showing ${offset + 1}-${offset + visibleCount} on page ${pageLabel}`
                    : 'No orders on this page'}
            </CardDescription>
          </div>
          <div className="flex items-center gap-2">
            <Button
              type="button"
              variant="outline"
              size="sm"
              onClick={() => setOffset((current) => Math.max(0, current - PAGE_SIZE))}
              disabled={offset === 0}
            >
              <ChevronLeft className="size-4" />
              Previous
            </Button>
            <Button
              type="button"
              variant="outline"
              size="sm"
              onClick={() => setOffset((current) => current + PAGE_SIZE)}
              disabled={!hasNextPage}
            >
              Next
              <ChevronRight className="size-4" />
            </Button>
          </div>
        </CardHeader>
        <CardContent>
          {isLoading ? (
            <div className="space-y-3" data-testid="orders-loading">
              {Array.from({ length: 5 }).map((_, index) => (
                <div key={index} className="flex items-center gap-3 rounded-lg border p-3">
                  <div className="h-4 w-32 animate-pulse rounded bg-muted" />
                  <div className="h-4 w-24 animate-pulse rounded bg-muted" />
                  <div className="ml-auto h-5 w-20 animate-pulse rounded-full bg-muted" />
                </div>
              ))}
            </div>
          ) : isError ? (
            <div className="space-y-3" data-testid="orders-error">
              <p className="text-sm text-muted-foreground">
                Unable to load orders. Start the API server to see live data.
              </p>
              <Button type="button" variant="outline" size="sm" onClick={() => void refetch()}>
                Retry
              </Button>
            </div>
          ) : !visibleOrders.length ? (
            <div className="flex flex-col items-center gap-2 py-8 text-center" data-testid="orders-empty">
              <Receipt className="size-8 text-muted-foreground" />
              <p className="text-sm text-muted-foreground">
                {hasActiveFilters ? 'No orders matched the current filters.' : 'No orders yet.'}
              </p>
            </div>
          ) : (
            <div className="overflow-x-auto">
              <table className="w-full text-sm" data-testid="orders-table">
                <thead>
                  <tr className="border-b border-border text-left font-mono text-[11px] uppercase tracking-[0.16em] text-muted-foreground">
                    <th className="pb-2 font-medium">Ticker</th>
                    <th className="pb-2 font-medium">Side</th>
                    <th className="pb-2 font-medium">Type</th>
                    <th className="pb-2 font-medium">Qty</th>
                    <th className="pb-2 font-medium">Status</th>
                    <th className="pb-2 font-medium">Fill price</th>
                    <th className="pb-2 font-medium">Broker</th>
                    <th className="pb-2 font-medium">Submitted</th>
                  </tr>
                </thead>
                <tbody>
                  {visibleOrders.map((order: Order) => (
                    <tr
                      key={order.id}
                      className="cursor-pointer border-b border-border transition-colors hover:bg-accent/45 focus-within:bg-accent/45 last:border-0"
                      data-testid={`order-row-${order.id}`}
                      tabIndex={0}
                      onClick={(event) => {
                        if ((event.target as HTMLElement).closest(INTERACTIVE_SELECTOR)) return
                        navigate(`/orders/${order.id}`)
                      }}
                      onKeyDown={(event) => {
                        if (event.key !== 'Enter' && event.key !== ' ') return
                        const interactiveElement = (event.target as HTMLElement).closest(INTERACTIVE_SELECTOR)
                        if (interactiveElement) {
                          if (event.key === ' ') event.preventDefault()
                          return
                        }
                        event.preventDefault()
                        navigate(`/orders/${order.id}`)
                      }}
                    >
                      <td className="py-0 font-medium">
                        <Link
                          to={`/stocks/${order.ticker}`}
                          className="block w-full cursor-pointer py-3 font-mono text-[13px] tracking-[0.02em] text-primary hover:underline focus-visible:text-primary"
                          data-testid={`order-ticker-link-${order.id}`}
                          onClick={(e) => e.stopPropagation()}
                        >
                          {order.ticker}
                        </Link>
                      </td>
                      <td className="py-3">
                        <Badge variant={SIDE_VARIANTS[order.side]}>{order.side}</Badge>
                      </td>
                      <td className="py-3 text-muted-foreground">{order.order_type}</td>
                      <td className="py-3 font-mono text-[13px]">{order.quantity}</td>
                      <td className="py-3">
                        <Badge variant={STATUS_VARIANTS[order.status]}>{formatStatusLabel(order.status)}</Badge>
                      </td>
                      <td className="py-3 font-mono text-[13px]">
                        {order.filled_avg_price != null ? `$${Number(order.filled_avg_price).toFixed(2)}` : '—'}
                      </td>
                      <td className="py-3 text-muted-foreground">{order.broker}</td>
                      <td className="py-3 font-mono text-[13px] text-muted-foreground">
                        {formatDate(order.submitted_at)}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </CardContent>
      </Card>
    </div>
  )
}
