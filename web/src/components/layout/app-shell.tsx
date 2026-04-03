import {
  Activity,
  Brain,
  BriefcaseBusiness,
  LayoutDashboard,
  RadioTower,
  Settings2,
  ShieldAlert,
} from 'lucide-react';
import { NavLink, Outlet, useLocation } from 'react-router-dom';

import { cn } from '@/lib/utils';

const navigationItems = [
  { to: '/', label: 'Overview', icon: LayoutDashboard },
  { to: '/strategies', label: 'Strategies', icon: BriefcaseBusiness },
  { to: '/runs', label: 'Runs', icon: Activity },
  { to: '/portfolio', label: 'Portfolio', icon: BriefcaseBusiness },
  { to: '/memories', label: 'Memories', icon: Brain },
  { to: '/settings', label: 'Settings', icon: Settings2 },
  { to: '/risk', label: 'Risk', icon: ShieldAlert },
  { to: '/realtime', label: 'Realtime', icon: RadioTower },
];

export function AppShell() {
  const location = useLocation();

  return (
    <div className="mx-auto flex min-h-screen w-full max-w-396 gap-3 px-3 py-3 sm:px-4 lg:px-6">
      <aside className="hidden w-56 shrink-0 lg:block">
        <div className="sticky top-3 flex h-[calc(100vh-1.5rem)] flex-col rounded-lg border border-border bg-card p-3">
          <div className="border-b border-border pb-3">
            <p className="font-mono text-[11px] font-medium uppercase tracking-[0.18em] text-primary">
              Get Rich Quick
            </p>
          </div>

          <nav aria-label="Primary" className="mt-3 flex flex-1 flex-col gap-1">
            {navigationItems.map(({ to, label, icon: Icon }) => (
              <NavLink
                key={to}
                to={to}
                end={to === '/'}
                className={({ isActive }) =>
                  cn(
                    'inline-flex items-center gap-2.5 rounded-md px-2.5 py-2 text-sm font-medium transition-colors',
                    isActive
                      ? 'bg-primary/14 text-foreground'
                      : 'text-muted-foreground hover:bg-accent/70 hover:text-foreground',
                  )
                }
              >
                <Icon className="size-4" />
                <span>{label}</span>
              </NavLink>
            ))}
          </nav>
        </div>
      </aside>

      <div className="flex min-h-screen min-w-0 flex-1 flex-col gap-3">
        <header className="sticky top-3 z-20 flex items-center justify-between rounded-lg border border-border bg-card px-4 py-2.5">
          <div className="flex items-center gap-2 text-sm">
            <span className="font-semibold text-foreground">Get Rich Quick</span>
            <span className="text-muted-foreground">/</span>
            <span className="text-muted-foreground">{location.pathname === '/' ? 'overview' : location.pathname.slice(1)}</span>
          </div>

          <nav
            aria-label="Primary mobile"
            className="flex gap-1.5 overflow-x-auto lg:hidden"
          >
            {navigationItems.map(({ to, label, icon: Icon }) => (
              <NavLink
                key={to}
                to={to}
                end={to === '/'}
                className={({ isActive }) =>
                  cn(
                    'inline-flex shrink-0 items-center gap-1.5 rounded-md px-2.5 py-1.5 text-xs font-medium transition-colors',
                    isActive
                      ? 'bg-primary/14 text-foreground'
                      : 'text-muted-foreground hover:bg-accent/70 hover:text-foreground',
                  )
                }
              >
                <Icon className="size-3.5" />
                {label}
              </NavLink>
            ))}
          </nav>
        </header>

        <main className="flex-1 pb-4">
          <Outlet />
        </main>
      </div>
    </div>
  );
}
