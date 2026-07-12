import { useEffect, useState } from 'react';
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

const navItems = [
  { to: '/cockpit', label: 'Cockpit', icon: LayoutDashboard },
  { to: '/automation', label: 'Automation', icon: Bot },
  { to: '/strategies', label: 'Strategies', icon: Lightbulb },
  { to: '/runs', label: 'Runs', icon: Play },
  { to: '/events', label: 'Events', icon: Clock },
  { to: '/orders', label: 'Orders', icon: ShoppingCart },
  { to: '/trades', label: 'Trades', icon: ArrowLeftRight },
  { to: '/portfolio', label: 'Portfolio', icon: PieChart },
  { to: '/risk', label: 'Risk', icon: ShieldAlert },
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

  const [isCollapsed, setIsCollapsed] = useState(getInitialCollapsedState);
  const [isMobileOpen, setIsMobileOpen] = useState(false);
  const [isMobile, setIsMobile] = useState(getInitialMobileState);
  const [isActivityOpen, setIsActivityOpen] = useState(false);

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
        <button type="button" className="sidebar-backdrop" onClick={handleSidebarClose} aria-label="Close navigation" />
      )}

      <aside
        className={`sidebar ${isMobile ? 'mobile' : ''} ${isMobileOpen ? 'open' : ''}`}
        aria-label="Primary navigation"
      >
        <div className="brand">{sidebarCollapsed ? '' : 'Augr'}</div>
        <nav aria-label="Primary">
          {navItems.map(({ to, label, icon: Icon }) => (
            <NavLink
              key={to}
              to={to}
              data-label={label}
              onClick={isMobile ? handleSidebarClose : undefined}
            >
              <Icon size={18} />
              <span className="nav-label">{label}</span>
            </NavLink>
          ))}
        </nav>
      </aside>

      <div className="workspace">
        <header className="topbar">
          <div className="topbar-left">
            <button
              type="button"
              className="sidebar-toggle"
              onClick={toggleSidebar}
              aria-label={
                isMobileOpen
                  ? 'Close sidebar'
                  : sidebarCollapsed
                    ? 'Expand sidebar'
                    : 'Collapse sidebar'
              }
              aria-expanded={!sidebarCollapsed}
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
            <span className={`status-pill ${realtime.status}`}>Realtime {realtime.status}</span>
            <button
              type="button"
              className="sidebar-toggle activity-toggle"
              onClick={() => setIsActivityOpen((open) => !open)}
              aria-label={isActivityOpen ? 'Close realtime activity' : 'Open realtime activity'}
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
            <span>{auth.session?.user.username}</span>
            <button type="button" onClick={auth.logout}>
              Logout
            </button>
          </div>
        </header>
        <main className="content" id="main-content" tabIndex={-1}>
          <Outlet />
        </main>
        {isActivityOpen ? <button type="button" className="activity-backdrop" onClick={() => setIsActivityOpen(false)} aria-label="Close realtime activity" /> : null}
        <aside id="global-activity-drawer" className={`activity-drawer ${isActivityOpen ? 'open' : ''}`} aria-label="Global realtime activity">
          <h2>Activity</h2>
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
