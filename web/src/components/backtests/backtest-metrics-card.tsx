import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { cn } from '@/lib/utils'
import type { BacktestMetrics } from '@/lib/api/types'

interface BacktestMetricsCardProps {
  metrics: BacktestMetrics
}

function toNumber(value: number | string): number | null {
  if (typeof value === 'number') return value
  const parsed = Number(value)
  return Number.isFinite(parsed) ? parsed : null
}

function formatPct(value: number | string): string {
  const n = toNumber(value)
  if (n == null) return '—'
  return `${(n * 100).toFixed(2)}%`
}

function formatRatio(value: number | string): string {
  const n = toNumber(value)
  if (n == null) return '—'
  return n.toFixed(2)
}

function formatCurrency(value: number | string): string {
  const n = toNumber(value)
  if (n == null) return '—'
  return `$${n.toLocaleString(undefined, { minimumFractionDigits: 2, maximumFractionDigits: 2 })}`
}

function formatInt(value: number | string): string {
  const n = toNumber(value)
  if (n == null) return '—'
  return n.toLocaleString()
}

function valueColor(value: number | string, invert = false): string {
  const n = toNumber(value)
  if (n == null) return 'text-muted-foreground'
  if (invert) return n < 0 ? 'text-green-500' : n > 0 ? 'text-red-500' : 'text-foreground'
  return n > 0 ? 'text-green-500' : n < 0 ? 'text-red-500' : 'text-foreground'
}

interface MetricItemProps {
  label: string
  value: string
  colorClass: string
}

function MetricItem({ label, value, colorClass }: MetricItemProps) {
  return (
    <div>
      <dt className="font-mono text-[11px] uppercase tracking-[0.16em] text-muted-foreground">
        {label}
      </dt>
      <dd className={cn('mt-1 text-sm font-medium', colorClass)}>{value}</dd>
    </div>
  )
}

function MetricSection({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <div className="space-y-3">
      <h4 className="font-mono text-[11px] font-medium uppercase tracking-[0.16em] text-muted-foreground/70">
        {title}
      </h4>
      <dl className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
        {children}
      </dl>
    </div>
  )
}

export function BacktestMetricsCard({ metrics }: BacktestMetricsCardProps) {
  return (
    <Card data-testid="backtest-metrics-card">
      <CardHeader>
        <CardTitle>Metrics</CardTitle>
        <CardDescription>Full performance breakdown for this backtest run</CardDescription>
      </CardHeader>
      <CardContent className="space-y-6">
        <MetricSection title="Returns">
          <MetricItem
            label="Total Return"
            value={formatPct(metrics.total_return)}
            colorClass={valueColor(metrics.total_return)}
          />
          <MetricItem
            label="Buy & Hold Return"
            value={formatPct(metrics.buy_and_hold_return)}
            colorClass={valueColor(metrics.buy_and_hold_return)}
          />
        </MetricSection>

        <MetricSection title="Risk-Adjusted">
          <MetricItem
            label="Sharpe Ratio"
            value={formatRatio(metrics.sharpe_ratio)}
            colorClass={valueColor(metrics.sharpe_ratio)}
          />
          <MetricItem
            label="Sortino Ratio"
            value={formatRatio(metrics.sortino_ratio)}
            colorClass={valueColor(metrics.sortino_ratio)}
          />
          <MetricItem
            label="Calmar Ratio"
            value={formatRatio(metrics.calmar_ratio)}
            colorClass={valueColor(metrics.calmar_ratio)}
          />
        </MetricSection>

        <MetricSection title="Risk">
          <MetricItem
            label="Max Drawdown"
            value={formatPct(metrics.max_drawdown)}
            colorClass={valueColor(metrics.max_drawdown, true)}
          />
          <MetricItem
            label="Volatility"
            value={formatPct(metrics.volatility)}
            colorClass={valueColor(metrics.volatility, true)}
          />
        </MetricSection>

        <MetricSection title="Alpha / Beta">
          <MetricItem
            label="Alpha"
            value={formatRatio(metrics.alpha)}
            colorClass={valueColor(metrics.alpha)}
          />
          <MetricItem
            label="Beta"
            value={formatRatio(metrics.beta)}
            colorClass="text-foreground"
          />
        </MetricSection>

        <MetricSection title="Trade Quality">
          <MetricItem
            label="Win Rate"
            value={formatPct(metrics.win_rate)}
            colorClass={valueColor(metrics.win_rate)}
          />
          <MetricItem
            label="Profit Factor"
            value={formatRatio(metrics.profit_factor)}
            colorClass={valueColor(metrics.profit_factor)}
          />
          <MetricItem
            label="Avg Win/Loss Ratio"
            value={formatRatio(metrics.avg_win_loss_ratio)}
            colorClass={valueColor(metrics.avg_win_loss_ratio)}
          />
        </MetricSection>

        <MetricSection title="P&L">
          <MetricItem
            label="Realized P&L"
            value={formatCurrency(metrics.realized_pnl)}
            colorClass={valueColor(metrics.realized_pnl)}
          />
          <MetricItem
            label="Unrealized P&L"
            value={formatCurrency(metrics.unrealized_pnl)}
            colorClass={valueColor(metrics.unrealized_pnl)}
          />
          <MetricItem
            label="Start Equity"
            value={formatCurrency(metrics.start_equity)}
            colorClass="text-foreground"
          />
          <MetricItem
            label="End Equity"
            value={formatCurrency(metrics.end_equity)}
            colorClass="text-foreground"
          />
        </MetricSection>

        <MetricSection title="Meta">
          <MetricItem
            label="Total Bars"
            value={formatInt(metrics.total_bars)}
            colorClass="text-foreground"
          />
        </MetricSection>
      </CardContent>
    </Card>
  )
}
