import {
  Activity,
  BadgeDollarSign,
  BookOpen,
  Brain,
  BriefcaseBusiness,
  CalendarDays,
  FlaskConical,
  Globe,
  LayoutDashboard,
  FileText,
  Receipt,
  ShieldCheck,
  Signal,
  Zap,
  Settings2,
  ShieldAlert,
  RadioTower,
  Sparkles,
  TrendingUp,
} from 'lucide-react';
import type { LucideIcon } from 'lucide-react';
import { NavLink, Outlet, useLocation } from 'react-router-dom';

import { HudBadge, StatusLed } from '@/components/ui/hud';
import { getApiBaseUrl } from '@/lib/config';
import { isAuthenticated } from '@/lib/auth';
import { cn } from '@/lib/utils';

type FlatNavItem = { to: string; label: string; icon: LucideIcon; authRequired?: boolean };
type NavGroup = { label: string; items: FlatNavItem[] };
type NavItem = FlatNavItem | NavGroup;

const navigationItems: NavItem[] = [
  {
    label: 'Control',
    items: [
      { to: '/', label: 'Overview', icon: LayoutDashboard, authRequired: true },
      { to: '/settings', label: 'Settings', icon: Settings2, authRequired: true },
      { to: '/risk', label: 'Risk', icon: ShieldAlert, authRequired: true },
      { to: '/realtime', label: 'Realtime', icon: RadioTower, authRequired: true },
      { to: '/reliability', label: 'Reliability', icon: ShieldCheck, authRequired: true },
    ],
  },
  {
    label: 'Trading',
    items: [
      { to: '/strategies', label: 'Strategies', icon: BriefcaseBusiness, authRequired: true },
      { to: '/runs', label: 'Runs', icon: Activity, authRequired: true },
      { to: '/orders', label: 'Orders', icon: Receipt, authRequired: true },
      { to: '/journal', label: 'Journal', icon: FileText, authRequired: true },
      { to: '/backtests', label: 'Backtests', icon: FlaskConical, authRequired: true },
      { to: '/portfolio', label: 'Portfolio', icon: BriefcaseBusiness, authRequired: true },
      { to: '/signals', label: 'Signals', icon: Signal, authRequired: true },
    ],
  },
  {
    label: 'Research',
    items: [
      { to: '/discovery', label: 'Discovery', icon: Sparkles, authRequired: true },
      { to: '/options', label: 'Options', icon: TrendingUp },
      { to: '/calendar', label: 'Calendar', icon: CalendarDays },
      { to: '/universe', label: 'Universe', icon: Globe },
    ],
  },
  {
    label: 'Automation',
    items: [{ to: '/automation', label: 'Automation', icon: Zap, authRequired: true }],
  },
  {
    label: 'Event Markets',
    items: [
      { to: '/polymarket', label: 'Polymarket', icon: TrendingUp, authRequired: true },
      { to: '/kalshi', label: 'Kalshi', icon: BadgeDollarSign, authRequired: true },
      { to: '/surfers/ops', label: 'Surfers Ops', icon: ShieldCheck, authRequired: true },
    ],
  },
  {
    label: 'Intelligence',
    items: [
      { to: '/memories', label: 'Memories', icon: Brain, authRequired: true },
      { to: '/prompts', label: 'Prompts', icon: FileText, authRequired: true },
    ],
  },
  {
    label: 'System / Knowledge',
    items: [{ to: '/glossary', label: 'Glossary', icon: BookOpen }],
  },
];

export function AppShell() {
  const location = useLocation();
  const authenticated = isAuthenticated();
  const runtimeMode = import.meta.env.MODE;
  const apiBaseUrl = getApiBaseUrl();
  const apiHost = (() => {
    try {
      return new URL(apiBaseUrl).host;
    } catch {
      return apiBaseUrl;
    }
  })();
  const locationLabel = (() => {
    if (location.pathname === '/') return 'overview';

    const segments = location.pathname.split('/').filter(Boolean);
    if (segments[0] === 'replay' && segments[1] === 'decisions') return 'replay / decision';

    return segments.join(' / ');
  })();
  const visibleNavigationItems = navigationItems
    .map((item) => ('items' in item ? { ...item, items: item.items.filter((nav) => authenticated || !nav.authRequired) } : item))
    .filter((item) => ('items' in item ? item.items.length > 0 : authenticated || !item.authRequired));

  const renderNavItem = ({ to, label, icon: Icon }: FlatNavItem, mobile = false) => (
    <NavLink
      key={to}
      to={to}
      end={to === '/'}
      className={({ isActive }) =>
        cn(
          'inline-flex items-center gap-2 border font-medium uppercase tracking-[0.1em] transition-colors focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-pulse focus-visible:ring-offset-0',
          mobile ? 'w-full justify-start px-3 py-2 text-hud' : 'w-full justify-start px-2.5 py-1.5 text-[11px] leading-4',
          isActive
            ? 'border-pulse bg-panel-raised text-ink shadow-[6px_6px_0_0_rgb(0_0_0_/_0.88)]'
            : 'border-border bg-panel text-ink-dim hover:border-border-strong hover:bg-panel-raised hover:text-ink',
        )
      }
    >
      <Icon className={mobile ? 'size-3.5' : 'size-4'} />
      <span>{label}</span>
    </NavLink>
  );

  return (
    <div className="relative mx-auto flex h-dvh w-full max-w-396 gap-3 overflow-hidden bg-void px-3 py-3 text-ink sm:px-4 lg:px-6 hud-scan">
      <aside className="hidden h-full w-56 shrink-0 lg:block">
        <div className="hud-panel flex h-full flex-col rounded-none p-3">
          <div className="border-b border-border-faint pb-3">
            <div className="flex items-center justify-between gap-2">
              <p className="font-mono text-[11px] font-bold uppercase tracking-[0.2em] text-signal">
                Augr
              </p>
              <HudBadge tone={authenticated ? 'confirm' : 'caution'}>{authenticated ? 'secured' : 'guest'}</HudBadge>
            </div>
          </div>

          <nav aria-label="Primary" className="hud-scrollbar mt-3 flex flex-1 flex-col gap-2 overflow-y-auto pr-1.5">
            {visibleNavigationItems.map((item) =>
              'items' in item ? (
                <div key={item.label} className="space-y-1 border-t border-border-faint pt-2.5 first:border-t-0 first:pt-0">
                  <div className="px-1 text-[11px] font-bold uppercase tracking-[0.24em] text-ink-dim">
                    {item.label}
                  </div>
                  {item.items.map((nav) => renderNavItem(nav))}
                </div>
              ) : renderNavItem(item),
            )}
          </nav>
        </div>
      </aside>

      <div className="flex min-h-0 min-w-0 flex-1 flex-col gap-3 overflow-hidden">
        <header className="hud-statusbar flex-none justify-between rounded-none">
          <div className="flex min-w-0 items-center gap-2 text-sm">
            <span className="font-semibold text-ink">Augr</span>
            <span className="text-ink-dim">/</span>
            <span className="truncate text-ink-dim">{locationLabel}</span>
          </div>

          <div className="flex items-center gap-2">
            {authenticated ? (
              <>
                <StatusLed state="ok" label="Signed in" />
                <HudBadge tone="confirm">Auth</HudBadge>
              </>
            ) : (
              <>
                <StatusLed state="warn" label="Guest" />
                <HudBadge tone="caution">Guest mode</HudBadge>
                <NavLink className="text-sm font-medium uppercase tracking-[0.1em] text-signal hover:underline" to="/login">
                  Sign in
                </NavLink>
              </>
            )}
          </div>

        </header>

        <nav aria-label="Primary mobile" className="hud-panel hud-scrollbar max-h-[35dvh] flex-none space-y-3 overflow-y-auto rounded-none px-3 py-2.5 lg:hidden">
          {visibleNavigationItems.map((item) =>
            'items' in item ? (
              <div key={item.label} className="space-y-1 border-t border-border-faint pt-2.5 first:border-t-0 first:pt-0">
                <div className="px-1 text-[11px] font-bold uppercase tracking-[0.24em] text-ink-dim">
                  {item.label}
                </div>
                <div className="flex flex-wrap gap-1.5">
                  {item.items.map((nav) => renderNavItem(nav, true))}
                </div>
              </div>
            ) : (
              renderNavItem(item, true)
            ),
          )}
        </nav>

        <div className="flex min-h-0 flex-1 flex-col overflow-hidden">
          <main className="hud-scrollbar min-h-0 flex-1 overflow-y-auto pb-4 pr-1">
            <Outlet />
          </main>

          <footer
            aria-label="Shell status"
            className="hud-statusbar flex-none flex-wrap justify-between gap-x-4 gap-y-1 rounded-none border-t border-border-faint border-b-0 px-3 py-2 text-[10px]"
          >
            <div className="flex flex-wrap items-center gap-x-4 gap-y-1">
              <span className="text-ink-dim">
                env <span className="text-ink">{runtimeMode}</span>
              </span>
              <span className="text-ink-dim">
                auth <span className="text-ink">{authenticated ? 'signed in' : 'guest'}</span>
              </span>
              <span className="text-ink-dim">
                route <span className="text-ink">{locationLabel}</span>
              </span>
            </div>
            <div className="flex flex-wrap items-center gap-x-4 gap-y-1">
              <span className="text-ink-dim">
                api <span className="text-ink">{apiHost}</span>
              </span>
              <span className="text-ink-dim">
                runtime <span className="text-ink">{import.meta.env.DEV ? 'dev' : 'prod'}</span>
              </span>
            </div>
          </footer>
        </div>
      </div>
    </div>
  );
}
