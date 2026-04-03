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
    <div className="mx-auto flex min-h-screen w-full max-w-396 gap-4 px-4 py-4 sm:px-6 lg:px-8">
      <aside className="hidden w-72 shrink-0 lg:block">
        <div className="sticky top-4 flex h-[calc(100vh-2rem)] flex-col rounded-lg border border-white/10 bg-card/90 p-4 shadow-[0_0_0_1px_rgba(255,255,255,0.02),0_20px_48px_rgba(2,6,23,0.34)] backdrop-blur-sm">
          <div className="border-b border-white/6 pb-4">
            <p className="font-mono text-[11px] font-medium uppercase tracking-[0.24em] text-primary/85">
              Get Rich Quick
            </p>
            <p className="mt-2 text-lg font-semibold tracking-tight text-foreground">
              Trading command center
            </p>
            <p className="mt-1 text-sm leading-6 text-muted-foreground">
              Permanent dark operator UI for strategies, runs, positions, and risk.
            </p>
          </div>

          <nav aria-label="Primary" className="mt-4 flex flex-1 flex-col gap-1.5">
            {navigationItems.map(({ to, label, icon: Icon }) => (
              <NavLink
                key={to}
                to={to}
                end={to === '/'}
                className={({ isActive }) =>
                  cn(
                    'inline-flex items-center gap-3 rounded-md border px-3 py-2.5 text-sm font-medium transition-all',
                    isActive
                      ? 'border-primary/30 bg-primary/14 text-foreground shadow-[inset_0_1px_0_rgba(255,255,255,0.03)]'
                      : 'border-transparent text-muted-foreground hover:border-white/10 hover:bg-accent/70 hover:text-foreground',
                  )
                }
              >
                <Icon className="size-4" />
                <span>{label}</span>
              </NavLink>
            ))}
          </nav>

          <div className="grid gap-2 border-t border-white/6 pt-4 text-xs text-muted-foreground">
            <div className="rounded-md border border-white/8 bg-background/60 px-3 py-2">
              <span className="font-mono uppercase tracking-[0.18em] text-primary/75">Build</span>
              <p className="mt-1 text-sm text-foreground">React 19 · Vite · Tailwind 4</p>
            </div>
            <div className="rounded-md border border-white/8 bg-background/60 px-3 py-2">
              <span className="font-mono uppercase tracking-[0.18em] text-primary/75">Mode</span>
              <p className="mt-1 text-sm text-foreground">Dark only · operator density</p>
            </div>
          </div>
        </div>
      </aside>

      <div className="flex min-h-screen min-w-0 flex-1 flex-col gap-4">
        <header className="sticky top-4 z-20 rounded-lg border border-white/10 bg-card/88 px-4 py-3 shadow-[0_0_0_1px_rgba(255,255,255,0.02),0_16px_34px_rgba(2,6,23,0.3)] backdrop-blur-md">
          <div className="flex flex-col gap-3 lg:flex-row lg:items-center lg:justify-between">
            <div className="space-y-1">
              <p className="font-mono text-[11px] uppercase tracking-[0.2em] text-primary/80">
                Operator shell
              </p>
              <div className="flex flex-wrap items-center gap-2 text-sm text-muted-foreground">
                <span className="font-semibold text-foreground">Get Rich Quick</span>
                <span className="hidden text-white/20 sm:inline">/</span>
                <span>{location.pathname === '/' ? 'overview' : location.pathname.slice(1)}</span>
              </div>
            </div>
            <div className="flex flex-wrap items-center gap-2 text-[11px] font-medium uppercase tracking-[0.16em]">
              <span className="rounded-full border border-primary/25 bg-primary/10 px-2.5 py-1 text-primary">
                Dark mode locked
              </span>
              <span className="rounded-full border border-white/10 bg-background/80 px-2.5 py-1 text-muted-foreground">
                Info dense
              </span>
            </div>
          </div>

          <nav
            aria-label="Primary mobile"
            className="mt-3 flex gap-2 overflow-x-auto pb-1 lg:hidden"
          >
            {navigationItems.map(({ to, label, icon: Icon }) => (
              <NavLink
                key={to}
                to={to}
                end={to === '/'}
                className={({ isActive }) =>
                  cn(
                    'inline-flex shrink-0 items-center gap-2 rounded-full border px-3 py-1.5 text-xs font-medium transition-colors',
                    isActive
                      ? 'border-primary/30 bg-primary/14 text-foreground'
                      : 'border-white/10 bg-background/80 text-muted-foreground hover:bg-accent/70 hover:text-foreground',
                  )
                }
              >
                <Icon className="size-3.5" />
                {label}
              </NavLink>
            ))}
          </nav>
        </header>

        <main className="flex-1 pb-6">
          <Outlet />
        </main>
      </div>
    </div>
  );
}
