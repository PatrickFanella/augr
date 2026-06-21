import { Navigate, Outlet, useLocation } from 'react-router-dom'

import { useAuth } from '@/shared/auth/AuthProvider'

export function ProtectedRoute() {
  const auth = useAuth()
  const location = useLocation()
  if (auth.status === 'checking') return <main className="center-panel">Checking session…</main>
  if (auth.status !== 'authenticated') {
    const next = encodeURIComponent(`${location.pathname}${location.search}`)
    return <Navigate to={`/login?next=${next}`} replace />
  }
  return <Outlet />
}
