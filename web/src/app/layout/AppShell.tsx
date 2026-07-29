import { useEffect, useRef, useState } from 'react';
import { NavLink, Outlet } from 'react-router-dom';
import {
  ChevronLeft,
  LayoutDashboard,
  Bot,
  Lightbulb,
  Play,
  Clock,
  ShoppingCart,
  ArrowLeftRight,
  PieChart,
  ShieldAlert,
  Sun,
  Moon,
  ChevronRight,
  Activity,
  Settings,
  Landmark,
  ChartCandlestick,
  FlaskConical,
  BookOpenText,
} from 'lucide-react';

import { useTheme } from '@/app/providers/theme-context';
import { CommandPalette } from '@/components/CommandPalette';
import { getSettings } from '@/shared/api/endpoints';
import { useAuth } from '@/shared/auth/AuthProvider';
import { EntityLink } from '@/shared/components/EntityLinks';
import { queryKeys } from '@/shared/query/keys';
import { useRealtime } from '@/shared/websocket/RealtimeProvider';
import { useQuery } from '@tanstack/react-query';

const STORAGE_KEY = 'augr-sidebar-collapsed';

const navGroups = [
  { label: 'Monitor', items: [
    { to: '/cockpit', label: 'Cockpit', icon: LayoutDashboard },
    { to: '/portfolio', label: 'Portfolio', icon: PieChart },
    { to: '/risk', label: 'Risk', icon: ShieldAlert },
  ] },
  { label: 'Operate', items: [
    { to: '/automation', label: 'Automation', icon: Bot },
    { to: '/strategies', label: 'Strategies', icon: Lightbulb },
    { to: '/runs', label: 'Runs', icon: Play },
    { to: '/orders', label: 'Orders', icon: ShoppingCart },
    { to: '/trades', label: 'Trades', icon: ArrowLeftRight },
  ] },
  { label: 'Research', items: [
    { to: '/event-markets', label: 'Event markets', icon: Landmark },
    { to: '/options', label: 'Options', icon: ChartCandlestick },
    { to: '/backtests', label: 'Backtests', icon: FlaskConical },
    { to: '/journal', label: 'Journal', icon: BookOpenText },
    { to: '/events', label: 'Events', icon: Clock },
  ] },
  { label: 'System', items: [
    { to: '/settings', label: 'Settings', icon: Settings },
  ] },
];

const MOBILE_QUERY = '(max-width: 840px)';

function getInitialCollapsedState(): boolean {
  if (typeof window === 'undefined') return false;
  const stored = localStorage.getItem(STORAGE_KEY);
  if (stored !== null) return stored === 'true';
  // Auto-collapse on mobile by default
  return window.innerWidth <= 840;
}

function getInitialMobileState(): boolean {
  if (typeof window === 'undefined') return false;
  if (typeof window.matchMedia !== 'function') return window.innerWidth <= 840;
  return window.matchMedia(MOBILE_QUERY).matches;
}

export function AppShell() {
  const auth = useAuth();
  const realtime = useRealtime();
  const { theme, toggleTheme } = useTheme();
  const settings = useQuery({
    queryKey: queryKeys.settings,
    queryFn: ({ signal }) => getSettings(signal),
    enabled: auth.status === 'authenticated',
  });
  const configuredBrokers = settings.data?.system.connected_brokers.filter((item) => item.configured) ?? [];
  const brokerModes = new Set(configuredBrokers.map((broker) => broker.paper_mode === false ? 'live' : 'paper'));
  const mode = configuredBrokers.length === 0 ? 'Mode unknown' : brokerModes.size > 1 ? 'Mixed paper/live' : brokerModes.has('live') ? 'Live' : 'Paper';
  const kalshiFeed = settings.data?.system.connected_brokers.find((broker) => broker.name === 'kalshi');

  const [isCollapsed, setIsCollapsed] = useState(getInitialCollapsedState);
  const [isMobileOpen, setIsMobileOpen] = useState(false);
  const [isMobile, setIsMobile] = useState(getInitialMobileState);
  const [isActivityOpen, setIsActivityOpen] = useState(false);
  const navigationToggleRef = useRef<HTMLButtonElement>(null);
  const navigationRef = useRef<HTMLElement>(null);
  const navigationWasOpen = useRef(false);
  const activityToggleRef = useRef<HTMLButtonElement>(null);
  const activityCloseRef = useRef<HTMLButtonElement>(null);
  const activityDrawerRef = useRef<HTMLElement>(null);
  const activityWasOpen = useRef(false);

  useEffect(() => {
    localStorage.setItem(STORAGE_KEY, String(isCollapsed));
  }, [isCollapsed]);

  useEffect(() => {
    const closeOverlays = (event: KeyboardEvent) => {
      if (event.key !== 'Escape') return;
      setIsMobileOpen(false);
      setIsActivityOpen(false);
    };
    window.addEventListener('keydown', closeOverlays);
    return () => window.removeEventListener('keydown', closeOverlays);
  }, []);

  useEffect(() => {
    if (isActivityOpen) {
      activityWasOpen.current = true;
      activityCloseRef.current?.focus();
    } else if (activityWasOpen.current) {
      activityWasOpen.current = false;
      activityToggleRef.current?.focus();
    }
  }, [isActivityOpen]);

  useEffect(() => {
    if (!isMobile) return;
    if (isMobileOpen) {
      navigationWasOpen.current = true;
      navigationRef.current?.querySelector<HTMLElement>('a[href]')?.focus();
    } else if (navigationWasOpen.current) {
      navigationWasOpen.current = false;
      navigationToggleRef.current?.focus();
    }
  }, [isMobile, isMobileOpen]);

  useEffect(() => {
    if (!isMobile || (!isMobileOpen && !isActivityOpen)) return;
    const previousOverflow = document.body.style.overflow;
    document.body.style.overflow = 'hidden';
    return () => { document.body.style.overflow = previousOverflow };
  }, [isActivityOpen, isMobile, isMobileOpen]);

  const trapNavigationFocus = (event: React.KeyboardEvent<HTMLElement>) => {
    if (!isMobileOpen || event.key !== 'Tab') return;
    const focusable = Array.from(navigationRef.current?.querySelectorAll<HTMLElement>('a[href], button:not(:disabled)') ?? []);
    if (focusable.length === 0) return;
    const first = focusable[0];
    const last = focusable.at(-1)!;
    if (event.shiftKey && document.activeElement === first) {
      event.preventDefault();
      last.focus();
    } else if (!event.shiftKey && document.activeElement === last) {
      event.preventDefault();
      first.focus();
    }
  };

  const trapActivityFocus = (event: React.KeyboardEvent<HTMLElement>) => {
    if (!isActivityOpen || event.key !== 'Tab') return;
    const focusable = Array.from(activityDrawerRef.current?.querySelectorAll<HTMLElement>('button:not(:disabled), a[href], [tabindex]:not([tabindex="-1"])') ?? []);
    if (focusable.length === 0) return;
    const first = focusable[0];
    const last = focusable.at(-1)!;
    if (event.shiftKey && document.activeElement === first) {
      event.preventDefault();
      last.focus();
    } else if (!event.shiftKey && document.activeElement === last) {
      event.preventDefault();
      first.focus();
    }
  };

  useEffect(() => {
    if (typeof window.matchMedia !== 'function') {
      const handleResize = () => {
        const nextIsMobile = window.innerWidth <= 840;
        setIsMobile(nextIsMobile);
        if (nextIsMobile && !isCollapsed) {
          setIsCollapsed(true);
        }
        if (!nextIsMobile) {
          setIsMobileOpen(false);
        }
      };
      handleResize();
      window.addEventListener('resize', handleResize);
      return () => window.removeEventListener('resize', handleResize);
    }

    const media = window.matchMedia(MOBILE_QUERY);
    const handleResize = () => {
      setIsMobile(media.matches);
      if (media.matches && !isCollapsed) {
        setIsCollapsed(true);
      }
      if (!media.matches) {
        setIsMobileOpen(false);
      }
    };
    handleResize();
    media.addEventListener('change', handleResize);
    return () => media.removeEventListener('change', handleResize);
  }, [isCollapsed]);

  const toggleSidebar = () => {
    if (isMobile) {
      setIsMobileOpen(!isMobileOpen);
    } else {
      setIsCollapsed(!isCollapsed);
    }
  };

  const handleSidebarClose = () => {
    setIsMobileOpen(false);
  };

  const sidebarCollapsed = isMobile ? false : isCollapsed;

  return (
    <div className="app-layout" data-sidebar-collapsed={String(sidebarCollapsed)}>
      <a className="skip-link" href="#main-content">
        Skip to main content
      </a>
      <CommandPalette />

      {/* Mobile backdrop */}
      {isMobile && isMobileOpen && (
        <button type="button" className="sidebar-backdrop" onClick={handleSidebarClose} aria-label="Dismiss navigation overlay" />
      )}

      <aside
        ref={navigationRef}
        id="primary-navigation"
        className={`sidebar ${isMobile ? 'mobile' : ''} ${isMobileOpen ? 'open' : ''}`}
        aria-label="Primary navigation"
        aria-hidden={isMobile && !isMobileOpen ? true : undefined}
        inert={isMobile && !isMobileOpen ? true : undefined}
        onKeyDown={trapNavigationFocus}
      >
        <div className="brand">{sidebarCollapsed ? '' : 'Augr'}</div>
        <nav aria-label="Primary">
          {navGroups.map((group) => (
            <section className="nav-group" aria-labelledby={`nav-${group.label.toLowerCase()}`} key={group.label}>
              <h2 id={`nav-${group.label.toLowerCase()}`}>{group.label}</h2>
              {group.items.map(({ to, label, icon: Icon }) => (
                <NavLink key={to} to={to} data-label={label} onClick={isMobile ? handleSidebarClose : undefined}>
                  <Icon size={18} aria-hidden="true" />
                  <span className="nav-label">{label}</span>
                </NavLink>
              ))}
            </section>
          ))}
        </nav>
      </aside>

      <div className="workspace">
        <header className="topbar">
          <div className="topbar-left">
            <button
              ref={navigationToggleRef}
              type="button"
              className="sidebar-toggle"
              onClick={toggleSidebar}
              aria-label={
                isMobile
                  ? isMobileOpen ? 'Close navigation' : 'Open navigation'
                  : sidebarCollapsed
                    ? 'Expand sidebar'
                    : 'Collapse sidebar'
              }
              aria-expanded={isMobile ? isMobileOpen : !sidebarCollapsed}
              aria-controls="primary-navigation"
            >
              {isMobile ? (
                isMobileOpen ? (
                  <ChevronLeft size={15} />
                ) : (
                  <ChevronRight size={15} />
                )
              ) : sidebarCollapsed ? (
                <ChevronRight size={15} />
              ) : (
                <ChevronLeft size={15} />
              )}
            </button>
            <div className="topbar-info">
              <p className="eyebrow">
                ~/augr/{settings.data?.system.environment ?? 'environment-unknown'}
              </p>
              <strong title={configuredBrokers.map((broker) => `${broker.name}: ${broker.paper_mode === false ? 'live' : 'paper'}`).join(', ')}>{mode} command center</strong>
            </div>
          </div>
          <div className="header-cluster">
            {kalshiFeed?.configured ? <span className={`status-pill ${kalshiFeed.data_environment === 'live' ? 'success' : 'warning'}`} title={kalshiFeed.data_source_url}>Kalshi {kalshiFeed.data_environment ?? 'unknown'}</span> : null}
            <span className={`status-pill ${realtime.status}`}>Realtime {realtime.status}</span>
            <button
              ref={activityToggleRef}
              type="button"
              className="sidebar-toggle activity-toggle"
              onClick={() => setIsActivityOpen((open) => !open)}
              aria-label={isActivityOpen ? 'Realtime activity is open' : 'Open realtime activity'}
              aria-expanded={isActivityOpen}
              aria-controls="global-activity-drawer"
            >
              <Activity size={18} />
            </button>
            <button
              type="button"
              className="sidebar-toggle"
              onClick={toggleTheme}
              aria-label={theme === 'dark' ? 'Switch to light theme' : 'Switch to dark theme'}
              title={theme === 'dark' ? 'Switch to light theme' : 'Switch to dark theme'}
            >
              {theme === 'dark' ? <Sun size={18} /> : <Moon size={18} />}
            </button>
            <span className="session-user" title={auth.session?.user.username}>Signed in as <span>{auth.session?.user.username}</span></span>
            <button type="button" onClick={auth.logout} aria-label={`Logout ${auth.session?.user.username ?? ''}`.trim()}>
              Logout
            </button>
          </div>
        </header>
        <main className="content" id="main-content" tabIndex={-1}>
          <Outlet />
        </main>
        {isActivityOpen ? <button type="button" className="activity-backdrop" onClick={() => setIsActivityOpen(false)} aria-label="Dismiss realtime activity overlay" /> : null}
        <aside ref={activityDrawerRef} id="global-activity-drawer" className={`activity-drawer ${isActivityOpen ? 'open' : ''}`} aria-label="Global realtime activity" onKeyDown={trapActivityFocus}>
          <div className="panel-header">
            <h2>Activity</h2>
            {isActivityOpen ? <button ref={activityCloseRef} type="button" className="btn-icon" onClick={() => setIsActivityOpen(false)} aria-label="Close realtime activity"><ChevronRight size={16} /></button> : null}
          </div>
          {realtime.events.length === 0 ? <p>No realtime events yet.</p> : null}
          <ul aria-live="polite">
            {realtime.events.slice(0, 12).map((event, index) => (
              <li key={`${event.timestamp}-${index}`}>
                <strong>{event.type}</strong>
                <time>{new Date(event.timestamp).toLocaleTimeString()}</time>
                <div className="header-cluster">
                  {event.strategy_id ? (
                    <EntityLink kind="strategy" id={event.strategy_id} copy={false} />
                  ) : null}
                  {event.run_id ? <EntityLink kind="run" id={event.run_id} copy={false} /> : null}
                </div>
              </li>
            ))}
          </ul>
        </aside>
      </div>
    </div>
  );
}
