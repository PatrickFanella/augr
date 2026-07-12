import { useEffect, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { Command } from 'cmdk'
import {
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
  Settings,
  Landmark,
} from 'lucide-react'
import { useTheme } from '@/app/providers/theme-context'

type CommandItem = {
  id: string
  label: string
  icon: React.ReactNode
  group: string
  onSelect: () => void
  keywords?: string
}

export function CommandPalette() {
  const [open, setOpen] = useState(false)
  const navigate = useNavigate()
  const { theme, toggleTheme } = useTheme()

  // Global Cmd/Ctrl+K shortcut
  useEffect(() => {
    const handler = (event: KeyboardEvent) => {
      if ((event.metaKey || event.ctrlKey) && event.key === 'k') {
        event.preventDefault()
        setOpen((prev) => !prev)
      }
      if (event.key === 'Escape') {
        setOpen(false)
      }
    }
    window.addEventListener('keydown', handler)
    return () => window.removeEventListener('keydown', handler)
  }, [])

  const navItems = [
    { to: '/cockpit', label: 'Cockpit', icon: <LayoutDashboard size={16} /> },
    { to: '/automation', label: 'Automation', icon: <Bot size={16} /> },
    { to: '/strategies', label: 'Strategies', icon: <Lightbulb size={16} /> },
    { to: '/runs', label: 'Runs', icon: <Play size={16} /> },
    { to: '/events', label: 'Events', icon: <Clock size={16} /> },
    { to: '/orders', label: 'Orders', icon: <ShoppingCart size={16} /> },
    { to: '/trades', label: 'Trades', icon: <ArrowLeftRight size={16} /> },
    { to: '/portfolio', label: 'Portfolio', icon: <PieChart size={16} /> },
    { to: '/event-markets', label: 'Event markets', icon: <Landmark size={16} /> },
    { to: '/risk', label: 'Risk', icon: <ShieldAlert size={16} /> },
    { to: '/settings', label: 'Settings', icon: <Settings size={16} /> },
  ]

  const items: CommandItem[] = [
    ...navItems.map((item) => ({
      id: `nav-${item.to}`,
      label: `Go to ${item.label}`,
      icon: item.icon,
      group: 'Navigation',
      onSelect: () => {
        navigate(item.to)
        setOpen(false)
      },
      keywords: item.label.toLowerCase(),
    })),
    {
      id: 'theme-toggle',
      label: theme === 'dark' ? 'Switch to light theme' : 'Switch to dark theme',
      icon: theme === 'dark' ? <Sun size={16} /> : <Moon size={16} />,
      group: 'Actions',
      onSelect: () => {
        toggleTheme()
        setOpen(false)
      },
      keywords: 'theme dark light toggle',
    },
  ]

  if (!open) return null

  return (
    <div
      className="fixed inset-0 z-50 grid place-items-start bg-[rgba(0,0,0,0.7)] p-4 pt-[15vh] backdrop-blur"
      onClick={() => setOpen(false)}
    >
      <Command
        loop
        label="Command palette"
        className="mx-auto w-full max-w-2xl overflow-hidden rounded-lg border-2 border-[var(--color-accent-primary)] bg-[var(--color-surface-raised)] shadow-[var(--shadow-brutal)]"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="border-b border-[var(--color-border-subtle)] px-4 py-3">
          <Command.Input
            autoFocus
            placeholder="Type a command…"
            className="w-full bg-transparent font-mono text-sm text-[var(--color-text-primary)] outline-none placeholder:text-[var(--color-text-muted)]"
          />
        </div>
        <Command.List className="max-h-[420px] overflow-auto p-2">
          <Command.Empty className="px-3 py-4 text-center text-sm text-[var(--color-text-muted)]">
            No results found.
          </Command.Empty>

          {['Navigation', 'Actions'].map((group) => {
            const groupItems = items.filter((item) => item.group === group)
            if (groupItems.length === 0) return null
            return (
              <Command.Group key={group} heading={group} className="mb-2">
                {groupItems.map((item) => (
                  <Command.Item
                    key={item.id}
                    value={`${item.label} ${item.keywords ?? ''}`}
                    onSelect={item.onSelect}
                    className="flex w-full cursor-pointer items-center gap-3 rounded-md px-3 py-2 text-left font-mono text-sm text-[var(--color-text-secondary)] data-[selected=true]:bg-[var(--color-surface-overlay)] data-[selected=true]:text-[var(--color-text-primary)]"
                  >
                    <span aria-hidden="true" className="text-[var(--color-accent-primary)]">
                      {item.icon}
                    </span>
                    <span className="flex-1">{item.label}</span>
                    <span className="font-mono text-xs text-[var(--color-text-muted)]">↵</span>
                  </Command.Item>
                ))}
              </Command.Group>
            )
          })}
        </Command.List>
      </Command>
    </div>
  )
}
