import { BrowserRouter, Route, Routes } from 'react-router-dom'

import { AppShell } from '@/components/layout/app-shell'
import { ProtectedRoute, PublicOnlyRoute } from '@/components/routes/route-guards'
import { AppProviders } from '@/lib/providers'
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
import { PortfolioPage } from '@/pages/portfolio-page'

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
