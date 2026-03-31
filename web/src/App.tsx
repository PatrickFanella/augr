import { BrowserRouter, Route, Routes } from 'react-router-dom'

import { AppShell } from '@/components/layout/app-shell'
import { ProtectedRoute, PublicOnlyRoute } from '@/components/routes/route-guards'
import { AppProviders } from '@/lib/providers'
import { DashboardPage } from '@/pages/dashboard-page'
import { LoginPage } from '@/pages/login-page'
import { PipelineRunPage } from '@/pages/pipeline-run-page'
import { MemoriesPage } from '@/pages/memories-page'
import { PlaceholderPage } from '@/pages/placeholder-page'
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
          <Route
            path="risk"
            element={
              <PlaceholderPage
                title="Risk controls"
                description="Risk engine status and kill-switch actions have typed request and response models ready to consume."
                bullets={[
                  'Engine status mirrors the backend circuit breaker, kill switch, and position limit structures.',
                  'This placeholder keeps the route visible without prematurely defining the full page UX.',
                ]}
              />
            }
          />
          <Route
            path="realtime"
            element={
              <PlaceholderPage
                title="Realtime events"
                description="A reusable WebSocket hook is wired for subscription, reconnection, and message parsing."
                bullets={[
                  'Subscribe by strategy ID, run ID, or to all events using the backend command format.',
                  'Client code exposes the last parsed message and connection state for future widgets.',
                ]}
              />
            }
          />
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
