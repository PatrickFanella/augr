import { useMemo, useState, type ReactNode } from 'react'
import { Link, useLocation } from 'react-router-dom'

type EntityKind = 'strategy' | 'run' | 'order' | 'trade' | 'position' | 'decision' | 'event' | 'opportunity' | 'risk'

type Crumb = {
  label: ReactNode
  to?: string
}

function shortId(value: string) {
  if (value.length <= 16) return value
  return `${value.slice(0, 8)}…${value.slice(-4)}`
}

function defaultEntityLabel(kind: EntityKind, id: string, label?: string) {
  if (label) return label
  return `${kind[0]!.toUpperCase()}${kind.slice(1)} ${shortId(id)}`
}

function hrefFor(kind: EntityKind, id: string) {
  switch (kind) {
    case 'strategy': return `/strategies/${id}`
    case 'run': return `/runs/${id}`
    case 'order': return `/orders/${id}`
    case 'position': return `/trades?position_id=${encodeURIComponent(id)}`
    case 'trade': return `/trades?trade_id=${encodeURIComponent(id)}`
    case 'decision': return `/events?decision_id=${encodeURIComponent(id)}`
    case 'event': return `/events?event_id=${encodeURIComponent(id)}`
    case 'opportunity': return `/portfolio?tab=allocator&opportunity_id=${encodeURIComponent(id)}`
    case 'risk': return '/risk'
  }
}

function withSourceContext(href: string, from: string) {
  if (!from.includes('?')) return href
  const [path, query = ''] = href.split('?')
  const params = new URLSearchParams(query)
  if (!params.has('from')) params.set('from', from)
  return `${path}?${params.toString()}`
}

export function CopyButton({ value, label = 'Copy ID' }: { value: string; label?: string }) {
  const [copied, setCopied] = useState(false)
  return (
    <button
      type="button"
      className="secondary-button"
      onClick={() => {
        void navigator.clipboard?.writeText(value)
        setCopied(true)
        window.setTimeout(() => setCopied(false), 1200)
      }}
      aria-label={label}
    >
      {copied ? 'Copied' : 'Copy'}
    </button>
  )
}

export function EntityLink({ kind, id, label, preserveContext = true, copy = true }: { kind: EntityKind; id?: string; label?: string; preserveContext?: boolean; copy?: boolean }) {
  const location = useLocation()
  const from = `${location.pathname}${location.search}`
  const href = useMemo(() => id ? withSourceContext(hrefFor(kind, id), from) : undefined, [from, id, kind])
  if (!id) return <span className="muted">No {kind} ID recorded</span>
  const text = defaultEntityLabel(kind, id, label)
  return (
    <span className="entity-link">
      {preserveContext && href ? <Link to={href}>{text}</Link> : <Link to={hrefFor(kind, id)}>{text}</Link>}
      {copy ? <> <CopyButton value={id} label={`Copy ${kind} ID`} /></> : null}
    </span>
  )
}

export function EntityId({ kind, id, label }: { kind: EntityKind; id?: string; label?: string }) {
  if (!id) return <span className="muted">No {kind} ID recorded</span>
  return (
    <span className="entity-id">
      <code title={id}>{label ?? shortId(id)}</code> <CopyButton value={id} label={`Copy ${kind} ID`} />
    </span>
  )
}

export function Breadcrumbs({ items }: { items: Crumb[] }) {
  return (
    <nav className="breadcrumbs" aria-label="Breadcrumbs">
      {items.map((item, index) => (
        <span key={index} className="breadcrumb-item">
          {index > 0 ? <span aria-hidden="true">/</span> : null}
          {item.to ? <Link to={item.to}>{item.label}</Link> : <span>{item.label}</span>}
        </span>
      ))}
    </nav>
  )
}
