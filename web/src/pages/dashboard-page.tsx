import { ActiveStrategies } from '@/components/dashboard/active-strategies'
import { ActivityFeed } from '@/components/dashboard/activity-feed'
import { PortfolioSummary } from '@/components/dashboard/portfolio-summary'
import { PageHeader } from '@/components/layout/page-header'
import { RiskStatusBar } from '@/components/dashboard/risk-status-bar'

export function DashboardPage() {
  return (
    <div className="space-y-4" data-testid="dashboard-page">
      <PageHeader
        eyebrow="Overview"
        title="Trading overview"
        description="Live portfolio, strategy, and risk telemetry for the current operating session."
      />

      <PortfolioSummary />

      <div className="grid gap-4 xl:grid-cols-[minmax(0,1.4fr)_minmax(320px,0.9fr)]">
        <div className="space-y-4">
          <ActiveStrategies />
          <ActivityFeed />
        </div>

        <div className="space-y-4">
          <RiskStatusBar />
        </div>
      </div>
    </div>
  )
}
