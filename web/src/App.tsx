import { BrowserRouter, Route, Routes } from 'react-router-dom'

import { AppShell } from '@/components/layout/app-shell'
import { AppProviders } from '@/lib/providers'
import { DashboardPage } from '@/pages/dashboard-page'
import { PipelineRunPage } from '@/pages/pipeline-run-page'
import { MemoriesPage } from '@/pages/memories-page'
import { PlaceholderPage } from '@/pages/placeholder-page'
import { SettingsPage } from '@/pages/settings-page'
import { StrategiesPage } from '@/pages/strategies-page'
import { StrategyDetailPage } from '@/pages/strategy-detail-page'
import { PortfolioPage } from '@/pages/portfolio-page'

function App() {
  return (
    <AppProviders>
      <BrowserRouter>
        <Routes>
          <Route element={<AppShell />}>
            <Route index element={<DashboardPage />} />
            <Route path="strategies" element={<StrategiesPage />} />
            <Route path="strategies/:id" element={<StrategyDetailPage />} />
            <Route
              path="runs"
              element={
                <PlaceholderPage
                  title="Pipeline runs"
                  description="Run history, decisions, and cancellation controls are scaffolded behind the shared API client."
                  bullets={[
                    'Run detail and decision-list client methods already match the backend response envelopes.',
                    'WebSocket subscriptions can target individual strategy and run IDs for realtime updates.',
                  ]}
                />
              }
            />
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
        </Routes>
      </BrowserRouter>
    </AppProviders>
  )
}

export default App
