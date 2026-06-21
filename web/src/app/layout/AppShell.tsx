import { NavLink, Outlet } from 'react-router-dom'

import { getSettings } from '@/shared/api/endpoints'
import { useAuth } from '@/shared/auth/AuthProvider'
import { EntityLink } from '@/shared/components/EntityLinks'
import { queryKeys } from '@/shared/query/keys'
import { useRealtime } from '@/shared/websocket/RealtimeProvider'
import { useQuery } from '@tanstack/react-query'

export function AppShell() {
  const auth = useAuth()
  const realtime = useRealtime()
  const settings = useQuery({ queryKey: queryKeys.settings, queryFn: ({ signal }) => getSettings(signal), enabled: auth.status === 'authenticated' })
  const broker = settings.data?.system.connected_brokers.find((item) => item.configured)
  const mode = broker?.paper_mode === false ? 'Live' : 'Paper'

  return (
    <div className="app-layout">
      <a className="skip-link" href="#main-content">Skip to main content</a>
      <aside className="sidebar" aria-label="Primary navigation">
        <div className="brand">Augr</div>
        <nav aria-label="Primary">
          <NavLink to="/cockpit">Cockpit</NavLink>
          <NavLink to="/strategies">Strategies</NavLink>
          <NavLink to="/runs">Runs</NavLink>
          <NavLink to="/events">Events</NavLink>
          <NavLink to="/orders">Orders</NavLink>
          <NavLink to="/trades">Trades</NavLink>
          <NavLink to="/portfolio">Portfolio</NavLink>
          <NavLink to="/risk">Risk</NavLink>
        </nav>
      </aside>
      <div className="workspace">
        <header className="topbar">
          <div>
            <p className="eyebrow">{settings.data?.system.environment ?? 'Environment unknown'}</p>
            <strong>{mode} mode</strong>
          </div>
          <div className="header-cluster">
            <span className={`status-pill ${realtime.status}`}>Realtime: {realtime.status}</span>
            <span>{auth.session?.user.username}</span>
            <button type="button" onClick={auth.logout}>Logout</button>
          </div>
        </header>
        <main className="content" id="main-content" tabIndex={-1}>
          <Outlet />
        </main>
        <aside className="activity-drawer" aria-label="Global realtime activity">
          <h2>Activity</h2>
          {realtime.events.length === 0 ? <p>No realtime events yet.</p> : null}
          <ul aria-live="polite">
            {realtime.events.slice(0, 12).map((event, index) => (
              <li key={`${event.timestamp}-${index}`}>
                <strong>{event.type}</strong>
                <time>{new Date(event.timestamp).toLocaleTimeString()}</time>
                <div className="header-cluster">
                  {event.strategy_id ? <EntityLink kind="strategy" id={event.strategy_id} label="Strategy" copy={false} /> : null}
                  {event.run_id ? <EntityLink kind="run" id={event.run_id} label="Run" copy={false} /> : null}
                </div>
              </li>
            ))}
          </ul>
        </aside>
      </div>
    </div>
  )
}
