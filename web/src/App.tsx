import { BrowserRouter, Route, Routes } from 'react-router-dom'

import { AppShell } from '@/components/layout/app-shell'
import { AppProviders } from '@/lib/providers'
import { DashboardPage } from '@/pages/dashboard-page'
import { PlaceholderPage } from '@/pages/placeholder-page'

function App() {
  return (
    <AppProviders>
      <BrowserRouter>
        <Routes>
          <Route element={<AppShell />}>
            <Route index element={<DashboardPage />} />
            <Route
              path="strategies"
              element={
                <PlaceholderPage
                  title="Strategies"
                  description="Strategy configuration, scheduling, and execution management will land here in a follow-up issue."
                  bullets={[
                    'Typed endpoints are available for list, create, update, delete, and manual runs.',
                    'Navigation and route structure are ready for page-specific UI work.',
                  ]}
                />
              }
            />
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
            <Route
              path="portfolio"
              element={
                <PlaceholderPage
                  title="Portfolio"
                  description="Portfolio, positions, orders, and trades have typed API client coverage and a reserved route."
                  bullets={[
                    'The client models mirror the current Go JSON field names and list envelopes.',
                    'TanStack Query is configured so these pages can opt into caching incrementally.',
                  ]}
                />
              }
            />
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
