import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Activity, Radio, Shield, Trash2 } from 'lucide-react'
import { useState } from 'react'

import { PageHeader } from '@/components/layout/page-header'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { apiClient } from '@/lib/api/client'
import type { StoredSignal, StoredTrigger, WatchTerm } from '@/lib/api/types'

type Tab = 'evaluated' | 'triggers' | 'watchlist'

function urgencyVariant(u: number): 'destructive' | 'warning' | 'secondary' | 'outline' {
  if (u >= 5) return 'destructive'
  if (u >= 4) return 'warning'
  if (u >= 3) return 'secondary'
  return 'outline'
}

function actionBadge(action: string) {
  switch (action) {
    case 'execute_thesis': return <Badge variant="destructive">execute thesis</Badge>
    case 'run_pipeline':
    case 're-evaluate': return <Badge variant="warning">run pipeline</Badge>
    default: return <Badge variant="outline">monitor</Badge>
  }
}

function EvaluatedTab() {
  const [minUrgency, setMinUrgency] = useState(1)
  const { data, isLoading } = useQuery({
    queryKey: ['signal-events', minUrgency],
    queryFn: () => apiClient.listEvaluatedSignals({ min_urgency: minUrgency, limit: 100 }),
    refetchInterval: 10_000,
  })

  const events: StoredSignal[] = data?.data ?? []

  return (
    <div className="space-y-4">
      <div className="flex items-center gap-3">
        <span className="text-sm text-muted-foreground">Min urgency:</span>
        {[1, 2, 3, 4, 5].map((u) => (
          <button
            key={u}
            onClick={() => setMinUrgency(u)}
            className={`rounded-full px-3 py-1 text-xs font-medium transition-colors ${
              minUrgency === u
                ? 'bg-primary text-primary-foreground'
                : 'bg-muted text-muted-foreground hover:bg-muted/80'
            }`}
          >
            {u}+
          </button>
        ))}
      </div>

      {isLoading ? (
        <div className="h-32 animate-pulse rounded-lg border bg-muted" />
      ) : events.length === 0 ? (
        <Card>
          <CardContent className="py-8 text-center text-sm text-muted-foreground">
            No evaluated signals yet. Signals appear here when the SignalHub processes events with urgency ≥ {minUrgency}.
          </CardContent>
        </Card>
      ) : (
        <div className="space-y-2">
          {events.map((evt) => (
            <Card key={evt.id}>
              <CardContent className="py-3">
                <div className="flex items-start justify-between gap-4">
                  <div className="min-w-0 flex-1">
                    <div className="flex flex-wrap items-center gap-2">
                      <Badge variant={urgencyVariant(evt.urgency)}>U{evt.urgency}</Badge>
                      <Badge variant="outline" className="font-mono text-[11px]">{evt.source}</Badge>
                      {actionBadge(evt.recommended_action)}
                      <span className="text-xs text-muted-foreground">
                        {new Date(evt.received_at).toLocaleTimeString()}
                      </span>
                    </div>
                    <p className="mt-1 text-sm font-medium">{evt.title}</p>
                    {evt.summary && evt.summary !== evt.title && (
                      <p className="mt-0.5 text-xs text-muted-foreground">{evt.summary}</p>
                    )}
                    {(evt.affected_strategy_ids?.length ?? 0) > 0 && (
                      <p className="mt-1 text-[11px] text-muted-foreground">
                        Affects {evt.affected_strategy_ids.length} strateg{evt.affected_strategy_ids.length === 1 ? 'y' : 'ies'}
                      </p>
                    )}
                  </div>
                </div>
              </CardContent>
            </Card>
          ))}
        </div>
      )}
    </div>
  )
}

function TriggersTab() {
  const { data, isLoading } = useQuery({
    queryKey: ['trigger-log'],
    queryFn: () => apiClient.listTriggerLog({ limit: 100 }),
    refetchInterval: 10_000,
  })

  const triggers: StoredTrigger[] = data?.data ?? []

  return (
    <div className="space-y-2">
      {isLoading ? (
        <div className="h-32 animate-pulse rounded-lg border bg-muted" />
      ) : triggers.length === 0 ? (
        <Card>
          <CardContent className="py-8 text-center text-sm text-muted-foreground">
            No triggers fired yet. Triggers fire when a signal exceeds urgency 3.
          </CardContent>
        </Card>
      ) : (
        triggers.map((t) => (
          <Card key={t.id}>
            <CardContent className="py-3">
              <div className="flex flex-wrap items-center gap-2">
                <Badge variant={
                  t.action === 'execute_thesis' ? 'destructive' :
                  t.action === 'run_pipeline' ? 'warning' : 'outline'
                }>
                  {t.action.replace(/_/g, ' ')}
                </Badge>
                <Badge variant="outline" className="font-mono text-[11px]">{t.source}</Badge>
                <span className="font-mono text-xs text-muted-foreground">P{t.priority}</span>
                <span className="text-xs text-muted-foreground">{new Date(t.fired_at).toLocaleTimeString()}</span>
              </div>
              <p className="mt-1 text-sm font-medium">{t.signal_title}</p>
              {t.signal_summary && t.signal_summary !== t.signal_title && (
                <p className="mt-0.5 text-xs text-muted-foreground">{t.signal_summary}</p>
              )}
              <p className="mt-1 font-mono text-[11px] text-muted-foreground">Strategy: {t.strategy_id}</p>
            </CardContent>
          </Card>
        ))
      )}
    </div>
  )
}

function WatchlistTab() {
  const queryClient = useQueryClient()
  const [newTerm, setNewTerm] = useState('')

  const { data, isLoading } = useQuery({
    queryKey: ['watchlist'],
    queryFn: () => apiClient.listWatchTerms(),
  })

  const addMutation = useMutation({
    mutationFn: (term: string) => apiClient.addWatchTerm({ term }),
    onSuccess: () => {
      setNewTerm('')
      queryClient.invalidateQueries({ queryKey: ['watchlist'] })
    },
  })

  const deleteMutation = useMutation({
    mutationFn: (term: string) => apiClient.deleteWatchTerm(term),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ['watchlist'] }),
  })

  const terms: WatchTerm[] = data?.data ?? []
  const manual = terms.filter((t) => t.source === 'manual')
  const auto = terms.filter((t) => t.source === 'auto')

  return (
    <div className="space-y-4">
      {/* Add manual term */}
      <Card>
        <CardHeader className="pb-3">
          <CardTitle className="text-base">Add manual term</CardTitle>
          <CardDescription>
            Manual terms match across all strategies. Signals containing the term will be evaluated.
          </CardDescription>
        </CardHeader>
        <CardContent>
          <form
            className="flex gap-2"
            onSubmit={(e) => {
              e.preventDefault()
              if (newTerm.trim()) addMutation.mutate(newTerm.trim())
            }}
          >
            <Input
              value={newTerm}
              onChange={(e) => setNewTerm(e.target.value)}
              placeholder="e.g. earnings, fed rate, bitcoin"
              className="flex-1"
            />
            <Button type="submit" disabled={!newTerm.trim() || addMutation.isPending}>
              Add
            </Button>
          </form>
        </CardContent>
      </Card>

      {/* Manual terms */}
      {manual.length > 0 && (
        <Card>
          <CardHeader className="pb-3">
            <CardTitle className="text-base">Manual terms ({manual.length})</CardTitle>
          </CardHeader>
          <CardContent>
            <div className="flex flex-wrap gap-2">
              {manual.map((t) => (
                <div
                  key={t.term}
                  className="flex items-center gap-1 rounded-full border border-border bg-background px-3 py-1 text-sm"
                >
                  <span className="font-mono">{t.term}</span>
                  <button
                    onClick={() => deleteMutation.mutate(t.term)}
                    className="ml-1 text-muted-foreground hover:text-destructive"
                    aria-label={`Remove ${t.term}`}
                  >
                    <Trash2 className="size-3" />
                  </button>
                </div>
              ))}
            </div>
          </CardContent>
        </Card>
      )}

      {/* Auto-derived terms */}
      {isLoading ? (
        <div className="h-32 animate-pulse rounded-lg border bg-muted" />
      ) : auto.length > 0 ? (
        <Card>
          <CardHeader className="pb-3">
            <CardTitle className="text-base">Auto-derived terms ({auto.length})</CardTitle>
            <CardDescription>From strategy tickers and active thesis watch terms. Read-only.</CardDescription>
          </CardHeader>
          <CardContent>
            <div className="flex flex-wrap gap-2">
              {auto.map((t) => (
                <div
                  key={t.term}
                  className="rounded-full border border-dashed border-border bg-muted/40 px-3 py-1 font-mono text-sm text-muted-foreground"
                >
                  {t.term}
                  <span className="ml-1 text-[10px]">({t.strategy_ids.length})</span>
                </div>
              ))}
            </div>
          </CardContent>
        </Card>
      ) : null}

      {!isLoading && terms.length === 0 && (
        <Card>
          <CardContent className="py-8 text-center text-sm text-muted-foreground">
            No watch terms yet. Add a manual term above, or create strategies with active theses.
          </CardContent>
        </Card>
      )}
    </div>
  )
}

export function SignalsPage() {
  const [tab, setTab] = useState<Tab>('evaluated')

  const tabs: { id: Tab; label: string; icon: React.ReactNode }[] = [
    { id: 'evaluated', label: 'Evaluated events', icon: <Activity className="size-4" /> },
    { id: 'triggers', label: 'Trigger log', icon: <Radio className="size-4" /> },
    { id: 'watchlist', label: 'Watchlist', icon: <Shield className="size-4" /> },
  ]

  return (
    <div className="space-y-4">
      <PageHeader
        eyebrow="Signal intelligence"
        title="Signal monitor"
        description="Real-time event evaluation, trigger dispatch log, and keyword watchlist management."
      />

      <div className="flex gap-1 rounded-lg border border-border bg-muted/40 p-1">
        {tabs.map((t) => (
          <button
            key={t.id}
            onClick={() => setTab(t.id)}
            className={`flex flex-1 items-center justify-center gap-2 rounded-md px-3 py-2 text-sm font-medium transition-colors ${
              tab === t.id
                ? 'bg-background text-foreground shadow-sm'
                : 'text-muted-foreground hover:text-foreground'
            }`}
          >
            {t.icon}
            {t.label}
          </button>
        ))}
      </div>

      {tab === 'evaluated' && <EvaluatedTab />}
      {tab === 'triggers' && <TriggersTab />}
      {tab === 'watchlist' && <WatchlistTab />}
    </div>
  )
}
