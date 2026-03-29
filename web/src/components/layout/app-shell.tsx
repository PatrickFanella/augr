import { Activity, Brain, BriefcaseBusiness, LayoutDashboard, RadioTower, ShieldAlert } from 'lucide-react'
import { NavLink, Outlet } from 'react-router-dom'

import { cn } from '@/lib/utils'

const navigationItems = [
  { to: '/', label: 'Overview', icon: LayoutDashboard },
  { to: '/strategies', label: 'Strategies', icon: BriefcaseBusiness },
  { to: '/runs', label: 'Runs', icon: Activity },
  { to: '/portfolio', label: 'Portfolio', icon: BriefcaseBusiness },
  { to: '/memories', label: 'Memories', icon: Brain },
  { to: '/risk', label: 'Risk', icon: ShieldAlert },
  { to: '/realtime', label: 'Realtime', icon: RadioTower },
]

export function AppShell() {
  return (
    <div className="mx-auto flex min-h-screen w-full max-w-7xl flex-col px-4 py-6 sm:px-6 lg:px-8">
      <header className="rounded-2xl border bg-card/90 p-4 shadow-sm backdrop-blur sm:p-6">
        <div className="flex flex-col gap-4 lg:flex-row lg:items-center lg:justify-between">
          <div className="space-y-1">
            <p className="text-sm font-medium uppercase tracking-[0.18em] text-primary">Get Rich Quick</p>
            <h1 className="text-2xl font-semibold tracking-tight sm:text-3xl">Frontend scaffold</h1>
            <p className="max-w-2xl text-sm text-muted-foreground sm:text-base">
              React Router, TanStack Query, Tailwind CSS 4, and shadcn-compatible UI primitives are ready for follow-up page work.
            </p>
          </div>
          <div className="rounded-full border bg-secondary px-3 py-1 text-xs font-medium text-secondary-foreground">
            React 19 + Vite
          </div>
        </div>
        <nav aria-label="Primary" className="mt-6 flex flex-wrap gap-2">
          {navigationItems.map(({ to, label, icon: Icon }) => (
            <NavLink
              key={to}
              to={to}
              end={to === '/'}
              className={({ isActive }) =>
                cn(
                  'inline-flex items-center gap-2 rounded-full border px-4 py-2 text-sm font-medium transition-colors',
                  isActive
                    ? 'border-primary bg-primary text-primary-foreground shadow-sm'
                    : 'border-border bg-background/80 text-muted-foreground hover:bg-accent hover:text-accent-foreground',
                )
              }
            >
              <Icon className="size-4" />
              {label}
            </NavLink>
          ))}
        </nav>
      </header>
      <main className="flex-1 py-6 sm:py-8">
        <Outlet />
      </main>
    </div>
  )
}
