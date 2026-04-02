import { useQuery } from '@tanstack/react-query'
import {
  Activity,
  AlertCircle,
  ArrowDown,
  Brain,
  CandlestickChart,
  CircleDot,
  FileWarning,
  Layers3,
  ShieldAlert,
  ShoppingBag,
} from 'lucide-react'
import { useCallback, useEffect, useMemo, useRef, useState } from 'react'

import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { useWebSocketClient } from '@/hooks/use-websocket-client'
import { apiClient } from '@/lib/api/client'
import type { AgentEvent, WebSocketMessage, WebSocketServerMessage } from '@/lib/api/types'

const MAX_LIVE_EVENTS = 100

type FeedItem = AgentEvent & { live?: boolean }

function isWebSocketMessage(message: WebSocketServerMessage): message is WebSocketMessage {
  return 'type' in message && !('error' in message) && !('status' in message)
}

function eventIcon(kind: string) {
  const icons: Record<string, typeof Activity> = {
    pipeline_start: Layers3,
    pipeline_started: Layers3,
    phase_started: Layers3,
    agent_started: Brain,
    agent_completed: Brain,
    agent_decision: Brain,
    debate_round: CandlestickChart,
    debate_round_completed: CandlestickChart,
    signal: CircleDot,
    signal_emitted: CircleDot,
    order_submitted: ShoppingBag,
    order_filled: ShoppingBag,
    position_update: CandlestickChart,
    circuit_breaker: ShieldAlert,
    error: AlertCircle,
  }

  return icons[kind] ?? FileWarning
}

function eventVariant(kind: string): 'default' | 'secondary' | 'destructive' | 'success' | 'warning' {
  switch (kind) {
    case 'signal':
    case 'signal_emitted':
    case 'order_filled':
      return 'success'
    case 'circuit_breaker':
    case 'error':
      return 'destructive'
    case 'order_submitted':
    case 'position_update':
      return 'warning'
    default:
      return 'secondary'
  }
}

function eventLabel(kind: string) {
  return kind
    .replace(/_/g, ' ')
    .replace(/\b\w/g, (char) => char.toUpperCase())
}

function summarizeLiveData(data: unknown) {
  if (typeof data === 'string') {
    return data
  }

  if (data && typeof data === 'object') {
    if ('summary' in data && typeof data.summary === 'string') {
      return data.summary
    }
    if ('signal' in data && typeof data.signal === 'string') {
      return `Signal ${String(data.signal).toUpperCase()}`
    }
    if ('ticker' in data && typeof data.ticker === 'string') {
      return `Ticker ${data.ticker}`
    }
  }

  return undefined
}

function toFeedItem(message: WebSocketMessage): FeedItem {
  const now = message.timestamp ?? new Date().toISOString()
  const data = message.data as Record<string, unknown> | undefined

  return {
    id: `${message.type}-${message.run_id ?? 'none'}-${now}-${Math.random().toString(36).slice(2, 8)}`,
    pipeline_run_id: message.run_id,
    strategy_id: message.strategy_id,
    agent_role: typeof data?.agent_role === 'string' ? (data.agent_role as AgentEvent['agent_role']) : undefined,
    event_kind: message.type,
    title: eventLabel(message.type),
    summary: summarizeLiveData(message.data),
    metadata: message.data,
    created_at: now,
    live: true,
  }
}

function sortEvents(events: FeedItem[]) {
  return [...events].sort((a, b) => new Date(a.created_at).getTime() - new Date(b.created_at).getTime())
}

function relativeTime(value: string) {
  const deltaSeconds = Math.max(0, Math.floor((Date.now() - new Date(value).getTime()) / 1000))
  if (deltaSeconds < 60) return `${deltaSeconds}s ago`
  const minutes = Math.floor(deltaSeconds / 60)
  if (minutes < 60) return `${minutes}m ago`
  const hours = Math.floor(minutes / 60)
  if (hours < 24) return `${hours}h ago`
  const days = Math.floor(hours / 24)
  return `${days}d ago`
}

export function RealtimePage() {
  const [liveEvents, setLiveEvents] = useState<FeedItem[]>([])
  const [selectedEventId, setSelectedEventId] = useState<string | null>(null)
  const [autoScroll, setAutoScroll] = useState(true)
  const subscribedRef = useRef(false)
  const feedRef = useRef<HTMLDivElement>(null)

  const { data, isLoading } = useQuery({
    queryKey: ['events', 'realtime-page'],
    queryFn: () => apiClient.listEvents({ limit: 50 }),
  })

  const handleMessage = useCallback((message: WebSocketServerMessage) => {
    if (!isWebSocketMessage(message)) {
      return
    }

    setLiveEvents((prev) => {
      const next = [...prev, toFeedItem(message)]
      return next.length > MAX_LIVE_EVENTS ? next.slice(next.length - MAX_LIVE_EVENTS) : next
    })
  }, [])

  const { status, subscribeAll } = useWebSocketClient({
    enabled: true,
    onMessage: handleMessage,
  })

  useEffect(() => {
    if (status === 'open' && !subscribedRef.current) {
      subscribeAll()
      subscribedRef.current = true
    }

    if (status !== 'open') {
      subscribedRef.current = false
    }
  }, [status, subscribeAll])

  const events = useMemo(() => {
    const historical = (data?.data ?? []).map((event) => ({ ...event, live: false as const }))
    const merged = [...historical, ...liveEvents]
    const byId = new Map<string, FeedItem>()
    for (const event of merged) {
      byId.set(event.id, event)
    }
    return sortEvents(Array.from(byId.values()))
  }, [data?.data, liveEvents])

  const selectedEvent = useMemo(
    () => events.find((event) => event.id === selectedEventId) ?? null,
    [events, selectedEventId],
  )

  useEffect(() => {
    if (!selectedEventId && events.length > 0) {
      setSelectedEventId(events[events.length - 1]?.id ?? null)
    }
  }, [events, selectedEventId])

  useEffect(() => {
    if (!autoScroll || !feedRef.current) {
      return
    }

    feedRef.current.scrollTop = feedRef.current.scrollHeight
  }, [autoScroll, events])

  function handleFeedScroll() {
    const element = feedRef.current
    if (!element) {
      return
    }

    const nearBottom = element.scrollHeight - element.scrollTop - element.clientHeight < 24
    setAutoScroll(nearBottom)
  }

  function resumeScroll() {
    setAutoScroll(true)
    if (feedRef.current) {
      feedRef.current.scrollTop = feedRef.current.scrollHeight
    }
  }

  return (
    <div
      className="flex h-auto flex-col gap-4 md:h-[calc(100vh-12rem)] md:flex-row"
      data-testid="realtime-page"
    >
      <div className="flex w-full min-w-0 flex-col rounded-lg border bg-card p-4 md:w-2/5">
        <div className="mb-4 flex items-center justify-between gap-3">
          <div>
            <h3 className="text-lg font-semibold">Event Feed</h3>
            <p className="text-sm text-muted-foreground">
              {status === 'open' ? 'Live via WebSocket' : 'Connecting to WebSocket…'}
            </p>
          </div>
          <Badge variant={status === 'open' ? 'success' : 'outline'}>{status}</Badge>
        </div>

        <div ref={feedRef} onScroll={handleFeedScroll} className="flex-1 space-y-2 overflow-y-auto pr-1" data-testid="realtime-feed">
          {isLoading && events.length === 0 ? (
            <div className="space-y-2" data-testid="realtime-loading">
              {Array.from({ length: 4 }).map((_, index) => (
                <div key={index} className="h-20 animate-pulse rounded-lg border bg-muted" />
              ))}
            </div>
          ) : events.length === 0 ? (
            <p className="text-sm text-muted-foreground" data-testid="realtime-empty">No events yet.</p>
          ) : (
            events.map((event) => {
              const Icon = eventIcon(event.event_kind)
              const isSelected = selectedEventId === event.id

              return (
                <button
                  key={event.id}
                  type="button"
                  className={`flex w-full flex-col gap-2 rounded-lg border p-3 text-left transition-colors hover:bg-secondary/40 ${
                    isSelected ? 'bg-secondary/60 ring-1 ring-primary' : ''
                  }`}
                  onClick={() => setSelectedEventId(event.id)}
                  data-testid={`event-card-${event.id}`}
                >
                  <div className="flex items-start justify-between gap-3">
                    <div className="flex min-w-0 items-center gap-2">
                      <Icon className="size-4 shrink-0 text-muted-foreground" />
                      <Badge variant={eventVariant(event.event_kind)}>{eventLabel(event.event_kind)}</Badge>
                      {event.agent_role ? <Badge variant="outline">{event.agent_role}</Badge> : null}
                    </div>
                    <time className="shrink-0 text-xs text-muted-foreground" dateTime={event.created_at}>
                      {relativeTime(event.created_at)}
                    </time>
                  </div>
                  <div>
                    <p className="font-medium">{event.title}</p>
                    {event.summary ? (
                      <p className="line-clamp-2 text-sm text-muted-foreground">{event.summary}</p>
                    ) : null}
                  </div>
                </button>
              )
            })
          )}
        </div>

        {!autoScroll && events.length > 0 ? (
          <div className="pt-3">
            <Button type="button" variant="outline" size="sm" onClick={resumeScroll} data-testid="realtime-resume-scroll">
              <ArrowDown className="mr-2 size-4" />
              Resume live scroll
            </Button>
          </div>
        ) : null}
      </div>

      <div className="flex w-full flex-1 flex-col rounded-lg border bg-card p-4">
        {selectedEvent ? (
          <div className="space-y-4" data-testid="selected-event-panel">
            <div className="space-y-2">
              <div className="flex flex-wrap items-center gap-2">
                <Badge variant={eventVariant(selectedEvent.event_kind)}>{eventLabel(selectedEvent.event_kind)}</Badge>
                {selectedEvent.agent_role ? <Badge variant="outline">{selectedEvent.agent_role}</Badge> : null}
                {selectedEvent.live ? <Badge variant="success">live</Badge> : null}
              </div>
              <h3 className="text-lg font-semibold">{selectedEvent.title}</h3>
              <p className="text-sm text-muted-foreground">
                {new Date(selectedEvent.created_at).toLocaleString()}
              </p>
            </div>

            {selectedEvent.summary ? (
              <div>
                <h4 className="mb-2 text-sm font-medium">Summary</h4>
                <p className="text-sm text-muted-foreground">{selectedEvent.summary}</p>
              </div>
            ) : null}

            <dl className="grid gap-3 sm:grid-cols-2">
              <div>
                <dt className="text-xs text-muted-foreground">Pipeline run</dt>
                <dd className="text-sm font-medium">{selectedEvent.pipeline_run_id ?? '—'}</dd>
              </div>
              <div>
                <dt className="text-xs text-muted-foreground">Strategy</dt>
                <dd className="text-sm font-medium">{selectedEvent.strategy_id ?? '—'}</dd>
              </div>
            </dl>

            <div>
              <h4 className="mb-2 text-sm font-medium">Event payload</h4>
              <pre className="overflow-x-auto rounded-md bg-muted p-3 text-xs" data-testid="selected-event-metadata">
                {JSON.stringify(selectedEvent.metadata ?? selectedEvent, null, 2)}
              </pre>
            </div>
          </div>
        ) : (
          <div className="flex h-full items-center justify-center">
            <p className="text-sm text-muted-foreground">Select an event to start a conversation</p>
          </div>
        )}
      </div>
    </div>
  )
}
