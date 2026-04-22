import { render, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import { BacktestEquityChart } from '@/components/backtests/backtest-equity-chart'

vi.mock('recharts', () => ({
  ResponsiveContainer: ({ children }: { children: React.ReactNode }) => <div data-testid="rc">{children}</div>,
  AreaChart: ({ children }: { children: React.ReactNode }) => <div data-testid="area-chart">{children}</div>,
  Area: () => <div data-testid="area" />,
  XAxis: () => <div data-testid="x-axis" />,
  YAxis: () => <div data-testid="y-axis" />,
  Tooltip: () => <div data-testid="tooltip" />,
}))

describe('BacktestEquityChart', () => {
  it('renders non-empty chart data without crashing', () => {
    render(
      <BacktestEquityChart
        data={[
          {
            timestamp: '2026-04-20T00:00:00Z',
            cash: 100000,
            market_value: 0,
            portfolio_value: 100000,
            realized_pnl: 0,
            unrealized_pnl: 0,
            total_pnl: 0,
            peak_equity: 100000,
            drawdown_pct: 0,
          },
          {
            timestamp: '2026-04-21T00:00:00Z',
            cash: 100500,
            market_value: 750,
            portfolio_value: 101250,
            realized_pnl: 500,
            unrealized_pnl: 750,
            total_pnl: 1250,
            peak_equity: 101250,
            drawdown_pct: 0.01,
          },
        ]}
      />,
    )

    expect(screen.getByTestId('equity-chart')).toBeInTheDocument()
    expect(screen.getByTestId('area-chart')).toBeInTheDocument()
  })

  it('renders empty state when there is no data', () => {
    render(<BacktestEquityChart data={[]} />)
    expect(screen.getByTestId('equity-chart-empty')).toBeInTheDocument()
  })
})
