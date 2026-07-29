import { Navigate, Outlet, useLocation } from 'react-router-dom'

import { useAuth } from '@/shared/auth/AuthProvider'

export function ProtectedRoute() {
  const auth = useAuth()
  const location = useLocation()
  if (auth.status === 'checking') return <main className="center-panel">Checking session…</main>
  if (auth.status === 'unavailable') return <main className="center-panel"><h1>Unable to verify session</h1><p>Your session has been retained. Check the connection and try again.</p><button type="button" onClick={auth.retry}>Retry session check</button></main>
  if (auth.status !== 'authenticated') {
    const next = encodeURIComponent(`${location.pathname}${location.search}`)
    const reason = auth.reason ? `&reason=${auth.reason}` : ''
    return <Navigate to={`/login?next=${next}${reason}`} replace />
  }
  return <Outlet />
}
