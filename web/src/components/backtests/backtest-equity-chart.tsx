import { Area, AreaChart, ResponsiveContainer, Tooltip, XAxis, YAxis } from 'recharts'

import type { EquityCurvePoint } from '@/lib/api/types'

interface BacktestEquityChartProps {
  data: EquityCurvePoint[]
}

function formatDate(iso: string) {
  return new Date(iso).toLocaleDateString(undefined, { month: 'short', day: 'numeric' })
}

function formatCurrency(value: number) {
  return value.toLocaleString(undefined, { style: 'currency', currency: 'USD', maximumFractionDigits: 0 })
}

export function BacktestEquityChart({ data }: BacktestEquityChartProps) {
  if (data.length === 0) {
    return (
      <div
        className="flex items-center justify-center rounded-lg border border-dashed border-border py-12 text-sm text-muted-foreground"
        data-testid="equity-chart-empty"
      >
        No equity curve data
      </div>
    )
  }

  return (
    <div className="h-72" data-testid="equity-chart">
      <ResponsiveContainer width="100%" height="100%">
        <AreaChart data={data} margin={{ top: 4, right: 4, bottom: 0, left: 0 }}>
          <XAxis
            dataKey="timestamp"
            tickFormatter={formatDate}
            tick={{ fontSize: 11 }}
            stroke="hsl(var(--muted-foreground))"
            tickLine={false}
            axisLine={false}
          />
          <YAxis
            yAxisId="equity"
            tickFormatter={formatCurrency}
            tick={{ fontSize: 11 }}
            stroke="hsl(var(--muted-foreground))"
            tickLine={false}
            axisLine={false}
            width={80}
          />
          <YAxis
            yAxisId="drawdown"
            orientation="right"
            tickFormatter={(v: number) => `${(v * 100).toFixed(0)}%`}
            tick={{ fontSize: 11 }}
            stroke="hsl(var(--muted-foreground))"
            tickLine={false}
            axisLine={false}
            width={48}
            reversed
          />
          <Tooltip
            contentStyle={{
              backgroundColor: 'hsl(var(--card))',
              border: '1px solid hsl(var(--border))',
              borderRadius: '0.375rem',
              fontSize: '12px',
            }}
            labelFormatter={(label) => (typeof label === 'string' ? formatDate(label) : String(label ?? ''))}
            formatter={(value, name) => {
              const numericValue = typeof value === 'number' ? value : Number(value ?? 0)
              const seriesName = String(name)
              if (seriesName === 'drawdown_pct') return [`${(numericValue * 100).toFixed(2)}%`, 'Drawdown']
              return [formatCurrency(numericValue), 'Portfolio Value']
            }}
          />
          <Area
            yAxisId="equity"
            type="monotone"
            dataKey="portfolio_value"
            stroke="hsl(var(--primary))"
            fill="hsl(var(--primary) / 0.1)"
            strokeWidth={1.5}
          />
          <Area
            yAxisId="drawdown"
            type="monotone"
            dataKey="drawdown_pct"
            stroke="hsl(var(--destructive))"
            fill="hsl(var(--destructive) / 0.08)"
            strokeWidth={1}
          />
        </AreaChart>
      </ResponsiveContainer>
    </div>
  )
}
