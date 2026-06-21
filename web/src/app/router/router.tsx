import { createBrowserRouter, Navigate } from 'react-router-dom'

import { AppShell } from '@/app/layout/AppShell'
import { ProtectedRoute } from '@/app/router/ProtectedRoute'
import { CockpitPage } from '@/features/cockpit/CockpitPage'
import { EventsPage } from '@/features/events/EventsPage'
import { LoginPage } from '@/features/auth-login/LoginPage'
import { OrderDetailPage } from '@/features/orders/OrderDetailPage'
import { OrdersListPage } from '@/features/orders/OrdersListPage'
import { PortfolioPage } from '@/features/portfolio/PortfolioPage'
import { RiskPage } from '@/features/risk/RiskPage'
import { RunDetailPage } from '@/features/runs/RunDetailPage'
import { RunsListPage } from '@/features/runs/RunsListPage'
import { StrategyCreatePage } from '@/features/strategies/StrategyCreatePage'
import { StrategyDetailPage } from '@/features/strategies/StrategyDetailPage'
import { StrategyEditPage } from '@/features/strategies/StrategyEditPage'
import { StrategiesListPage } from '@/features/strategies/StrategiesListPage'
import { TradesListPage } from '@/features/trades/TradesListPage'

export function createAppRouter() {
  return createBrowserRouter([
    { path: '/login', element: <LoginPage /> },
    {
      element: <ProtectedRoute />,
      children: [
        {
          element: <AppShell />,
          children: [
            { path: '/', element: <Navigate to="/cockpit" replace /> },
            { path: '/cockpit', element: <CockpitPage /> },
            { path: '/events', element: <EventsPage /> },
            { path: '/orders', element: <OrdersListPage /> },
            { path: '/orders/:id', element: <OrderDetailPage /> },
            { path: '/portfolio', element: <PortfolioPage /> },
            { path: '/risk', element: <RiskPage /> },
            { path: '/runs', element: <RunsListPage /> },
            { path: '/runs/:id', element: <RunDetailPage /> },
            { path: '/strategies', element: <StrategiesListPage /> },
            { path: '/strategies/new', element: <StrategyCreatePage /> },
            { path: '/strategies/:id/edit', element: <StrategyEditPage /> },
            { path: '/strategies/:id', element: <StrategyDetailPage /> },
            { path: '/trades', element: <TradesListPage /> },
          ],
        },
      ],
    },
    { path: '*', element: <Navigate to="/cockpit" replace /> },
  ])
}
