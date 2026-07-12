import { lazy, Suspense } from 'react'
import { createBrowserRouter, Navigate } from 'react-router-dom'

import { AppShell } from '@/app/layout/AppShell'
import { ProtectedRoute } from '@/app/router/ProtectedRoute'
import { NotFoundPage, RouteErrorPage } from '@/app/router/RouteStatePages'
import { LoadingState } from '@/shared/components/QueryStates'
import { LoginPage } from '@/features/auth-login/LoginPage'

// Lazy-load all route components for code splitting.
// LoginPage stays eager — it's the first paint for unauthenticated users.
const AutomationDetailPage = lazy(() =>
  import('@/features/automation/AutomationDetailPage').then((m) => ({ default: m.AutomationDetailPage })),
)
const AutomationPage = lazy(() =>
  import('@/features/automation/AutomationPage').then((m) => ({ default: m.AutomationPage })),
)
const CockpitPage = lazy(() =>
  import('@/features/cockpit/CockpitPage').then((m) => ({ default: m.CockpitPage })),
)
const EventsPage = lazy(() =>
  import('@/features/events/EventsPage').then((m) => ({ default: m.EventsPage })),
)
const OrderDetailPage = lazy(() =>
  import('@/features/orders/OrderDetailPage').then((m) => ({ default: m.OrderDetailPage })),
)
const OrdersListPage = lazy(() =>
  import('@/features/orders/OrdersListPage').then((m) => ({ default: m.OrdersListPage })),
)
const PortfolioPage = lazy(() =>
  import('@/features/portfolio/PortfolioPage').then((m) => ({ default: m.PortfolioPage })),
)
const StockPage = lazy(() =>
  import('@/features/stock/StockPage').then((m) => ({ default: m.StockPage })),
)
const RiskPage = lazy(() =>
  import('@/features/risk/RiskPage').then((m) => ({ default: m.RiskPage })),
)
const RunDetailPage = lazy(() =>
  import('@/features/runs/RunDetailPage').then((m) => ({ default: m.RunDetailPage })),
)
const RunsListPage = lazy(() =>
  import('@/features/runs/RunsListPage').then((m) => ({ default: m.RunsListPage })),
)
const StrategyCreatePage = lazy(() =>
  import('@/features/strategies/StrategyCreatePage').then((m) => ({ default: m.StrategyCreatePage })),
)
const StrategyDetailPage = lazy(() =>
  import('@/features/strategies/StrategyDetailPage').then((m) => ({ default: m.StrategyDetailPage })),
)
const StrategyEditPage = lazy(() =>
  import('@/features/strategies/StrategyEditPage').then((m) => ({ default: m.StrategyEditPage })),
)
const StrategiesListPage = lazy(() =>
  import('@/features/strategies/StrategiesListPage').then((m) => ({ default: m.StrategiesListPage })),
)
const TradesListPage = lazy(() =>
  import('@/features/trades/TradesListPage').then((m) => ({ default: m.TradesListPage })),
)
const SettingsPage = lazy(() =>
  import('@/features/settings/SettingsPage').then((m) => ({ default: m.SettingsPage })),
)
const EventMarketsPage = lazy(() =>
  import('@/features/event-markets/EventMarketsPage').then((m) => ({ default: m.EventMarketsPage })),
)
const OptionsPage = lazy(() =>
  import('@/features/options/OptionsPage').then((m) => ({ default: m.OptionsPage })),
)
const BacktestsPage = lazy(() =>
  import('@/features/backtests/BacktestsPage').then((m) => ({ default: m.BacktestsPage })),
)

const routeFallback = <LoadingState />

function withSuspense(element: React.ReactElement) {
  return <Suspense fallback={routeFallback}>{element}</Suspense>
}

export function createAppRouter() {
  return createBrowserRouter([
    { path: '/login', element: <LoginPage /> },
    {
      element: <ProtectedRoute />,
      children: [
        {
          element: <AppShell />,
          errorElement: <RouteErrorPage />,
          children: [
            { path: '/', element: <Navigate to="/cockpit" replace /> },
            { path: '/automation', element: withSuspense(<AutomationPage />) },
            { path: '/automation/:name', element: withSuspense(<AutomationDetailPage />) },
            { path: '/cockpit', element: withSuspense(<CockpitPage />) },
            { path: '/events', element: withSuspense(<EventsPage />) },
            { path: '/orders', element: withSuspense(<OrdersListPage />) },
            { path: '/orders/:id', element: withSuspense(<OrderDetailPage />) },
            { path: '/portfolio', element: withSuspense(<PortfolioPage />) },
            { path: '/stock/:ticker', element: withSuspense(<StockPage />) },
            { path: '/risk', element: withSuspense(<RiskPage />) },
            { path: '/runs', element: withSuspense(<RunsListPage />) },
            { path: '/runs/:id', element: withSuspense(<RunDetailPage />) },
            { path: '/strategies', element: withSuspense(<StrategiesListPage />) },
            { path: '/strategies/new', element: withSuspense(<StrategyCreatePage />) },
            { path: '/strategies/:id/edit', element: withSuspense(<StrategyEditPage />) },
            { path: '/strategies/:id', element: withSuspense(<StrategyDetailPage />) },
            { path: '/trades', element: withSuspense(<TradesListPage />) },
            { path: '/settings', element: withSuspense(<SettingsPage />) },
            { path: '/event-markets', element: withSuspense(<EventMarketsPage />) },
            { path: '/options', element: withSuspense(<OptionsPage />) },
            { path: '/backtests', element: withSuspense(<BacktestsPage />) },
            { path: '*', element: <NotFoundPage /> },
          ],
        },
      ],
    },
  ])
}
