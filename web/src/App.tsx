import { BrowserRouter, Route, Routes } from 'react-router-dom'

import { AppShell } from '@/components/layout/app-shell'
import { ProtectedRoute, PublicOnlyRoute } from '@/components/routes/route-guards'
import { AppProviders } from '@/lib/providers'
import { BacktestDetailPage } from '@/pages/backtest-detail-page'
import { BacktestsPage } from '@/pages/backtests-page'
import { DashboardPage } from '@/pages/dashboard-page'
import { LoginPage } from '@/pages/login-page'
import { PipelineRunPage } from '@/pages/pipeline-run-page'
import { MemoriesPage } from '@/pages/memories-page'
import { RealtimePage } from '@/pages/realtime-page'
import { RiskPage } from '@/pages/risk-page'
import { RunsPage } from '@/pages/runs-page'
import { SettingsPage } from '@/pages/settings-page'
import { StrategiesPage } from '@/pages/strategies-page'
import { StrategyDetailPage } from '@/pages/strategy-detail-page'
import { OrdersPage } from '@/pages/orders-page'
import { OrderDetailPage } from '@/pages/order-detail-page'
import { OptionsPage } from '@/pages/options-page'
import { PortfolioPage } from '@/pages/portfolio-page'
import { DiscoveryPage } from '@/pages/discovery-page'
import { AutomationPage } from '@/pages/automation-page'
import { AutomationDetailPage } from '@/pages/automation-detail-page'
import { CalendarPage } from '@/pages/calendar-page'
import { StockDetailPage } from '@/pages/stock-detail-page'
import { UniversePage } from '@/pages/universe-page'

export function AppRoutes() {
  return (
    <Routes>
      <Route element={<PublicOnlyRoute />}>
        <Route path="login" element={<LoginPage />} />
      </Route>

      <Route element={<ProtectedRoute />}>
        <Route element={<AppShell />}>
          <Route index element={<DashboardPage />} />
          <Route path="strategies" element={<StrategiesPage />} />
          <Route path="strategies/:id" element={<StrategyDetailPage />} />
          <Route path="runs" element={<RunsPage />} />
          <Route path="runs/:id" element={<PipelineRunPage />} />
          <Route path="backtests" element={<BacktestsPage />} />
          <Route path="backtests/:id" element={<BacktestDetailPage />} />
          <Route path="stocks/:ticker" element={<StockDetailPage />} />
          <Route path="orders" element={<OrdersPage />} />
          <Route path="orders/:id" element={<OrderDetailPage />} />
          <Route path="options" element={<OptionsPage />} />
          <Route path="discovery" element={<DiscoveryPage />} />
          <Route path="calendar" element={<CalendarPage />} />
          <Route path="universe" element={<UniversePage />} />
          <Route path="automation" element={<AutomationPage />} />
          <Route path="automation/:name" element={<AutomationDetailPage />} />
          <Route path="portfolio" element={<PortfolioPage />} />
          <Route path="memories" element={<MemoriesPage />} />
          <Route path="settings" element={<SettingsPage />} />
          <Route path="risk" element={<RiskPage />} />
          <Route path="realtime" element={<RealtimePage />} />
        </Route>
      </Route>
    </Routes>
  )
}

function App() {
  return (
    <AppProviders>
      <BrowserRouter>
        <AppRoutes />
      </BrowserRouter>
    </AppProviders>
  )
}

export default App
