import { PageHeader } from '@/components/layout/page-header'
import { PortfolioSummary } from '@/components/dashboard/portfolio-summary'
import { PortfolioChart } from '@/components/portfolio/portfolio-chart'
import { PositionsTable } from '@/components/portfolio/positions-table'
import { TradeHistory } from '@/components/portfolio/trade-history'

export function PortfolioPage() {
  return (
    <div className="space-y-4" data-testid="portfolio-page">
      <PageHeader
        eyebrow="Portfolio"
        title="Portfolio state"
        description="Track current exposure, realized performance, open positions, and recent trade executions."
      />

      <PortfolioSummary />

      <PortfolioChart />

      <div className="grid gap-4 xl:grid-cols-[minmax(0,1.25fr)_minmax(0,1fr)]">
        <PositionsTable />
        <TradeHistory />
      </div>
    </div>
  )
}
