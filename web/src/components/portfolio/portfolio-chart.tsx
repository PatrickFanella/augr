import { useMemo } from 'react'
import { useQuery } from '@tanstack/react-query'
import { LineChart } from 'lucide-react'
import { Area, AreaChart, CartesianGrid, ResponsiveContainer, Tooltip, XAxis, YAxis } from 'recharts'

import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { apiClient } from '@/lib/api/client'

const currencyFormatter = new Intl.NumberFormat('en-US', {
  style: 'currency',
  currency: 'USD',
  minimumFractionDigits: 2,
})

interface EquityPoint {
  date: string
  pnl: number
}

export function PortfolioChart() {
  const { data, isLoading, isError } = useQuery({
    queryKey: ['portfolio', 'positions', 'all'],
    queryFn: () => apiClient.listPositions({ limit: 100 }),
    refetchInterval: 30_000,
  })

  const chartData = useMemo<EquityPoint[]>(() => {
    if (!data?.data.length) return []

    const closedPositions = data.data
      .filter((p) => p.closed_at)
      .sort((a, b) => new Date(a.closed_at!).getTime() - new Date(b.closed_at!).getTime())

    if (!closedPositions.length) return []

    let cumulative = 0
    return closedPositions.map((p) => {
      cumulative += p.realized_pnl
      return {
        date: new Date(p.closed_at!).toLocaleDateString(),
        pnl: cumulative,
      }
    })
  }, [data])

  return (
    <Card data-testid="portfolio-chart">
      <CardHeader>
        <CardTitle>Equity curve</CardTitle>
        <CardDescription>Cumulative realized P&L over time</CardDescription>
      </CardHeader>
      <CardContent>
        {isLoading ? (
          <div className="h-64 w-full animate-pulse rounded bg-muted" data-testid="portfolio-chart-loading" />
        ) : isError ? (
          <p className="text-sm text-muted-foreground" data-testid="portfolio-chart-error">
            Unable to load chart data. Start the API server to see live data.
          </p>
        ) : !chartData.length ? (
          <div className="flex flex-col items-center gap-2 py-8 text-center" data-testid="portfolio-chart-empty">
            <LineChart className="size-8 text-muted-foreground" />
            <p className="text-sm text-muted-foreground">No closed positions to chart</p>
          </div>
        ) : (
          <ResponsiveContainer width="100%" height={256}>
            <AreaChart data={chartData}>
              <CartesianGrid strokeDasharray="3 3" className="stroke-muted" />
              <XAxis dataKey="date" className="text-xs" tick={{ fontSize: 12 }} />
              <YAxis
                className="text-xs"
                tick={{ fontSize: 12 }}
                tickFormatter={(value: number) => currencyFormatter.format(value)}
              />
              <Tooltip
                formatter={(value) => [currencyFormatter.format(Number(value)), 'P&L']}
                contentStyle={{ borderRadius: '0.5rem', fontSize: '0.875rem' }}
              />
              <Area
                type="monotone"
                dataKey="pnl"
                stroke="#059669"
                fill="#059669"
                fillOpacity={0.15}
                strokeWidth={2}
              />
            </AreaChart>
          </ResponsiveContainer>
        )}
      </CardContent>
    </Card>
  )
}
