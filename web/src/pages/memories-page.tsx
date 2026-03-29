import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Brain, ChevronLeft, ChevronRight, Search, Trash2 } from 'lucide-react'
import { useMemo, useState } from 'react'

import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from '@/components/ui/card'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { apiClient } from '@/lib/api/client'
import type { AgentMemory, AgentRole } from '@/lib/api/types'

const PAGE_SIZE = 10
const PAGE_REQUEST_SIZE = PAGE_SIZE + 1
const AGENT_ROLE_OPTIONS: AgentRole[] = [
  'market_analyst',
  'fundamentals_analyst',
  'news_analyst',
  'social_media_analyst',
  'bull_researcher',
  'bear_researcher',
  'trader',
  'invest_judge',
  'risk_manager',
  'aggressive_analyst',
  'conservative_analyst',
  'neutral_analyst',
  'aggressive_risk',
  'conservative_risk',
  'neutral_risk',
]

function formatAgentRole(role: AgentRole) {
  return role.replaceAll('_', ' ')
}

function formatDate(date: string) {
  return new Date(date).toLocaleString(undefined, {
    month: 'short',
    day: 'numeric',
    year: 'numeric',
    hour: '2-digit',
    minute: '2-digit',
  })
}

function summarize(text: string, maxLength = 160) {
  if (text.length <= maxLength) {
    return text
  }

  return `${text.slice(0, maxLength - 1).trimEnd()}…`
}

export function MemoriesPage() {
  const queryClient = useQueryClient()
  const [draftQuery, setDraftQuery] = useState('')
  const [draftRole, setDraftRole] = useState<AgentRole | ''>('')
  const [query, setQuery] = useState('')
  const [agentRole, setAgentRole] = useState<AgentRole | ''>('')
  const [offset, setOffset] = useState(0)
  const [selectedMemory, setSelectedMemory] = useState<AgentMemory | null>(null)

  const { data, isLoading, isError } = useQuery({
    queryKey: ['memories', query, agentRole, offset],
    queryFn: () =>
      apiClient.listMemories({
        q: query || undefined,
        agent_role: agentRole || undefined,
        limit: PAGE_REQUEST_SIZE,
        offset,
      }),
  })

  const deleteMutation = useMutation({
    mutationFn: (id: string) => apiClient.deleteMemory(id),
    onSuccess: (_, id) => {
      queryClient.invalidateQueries({ queryKey: ['memories'] })
      if (selectedMemory?.id === id) {
        setSelectedMemory(null)
      }
    },
  })

  const visibleMemories = data?.data.slice(0, PAGE_SIZE) ?? []
  const visibleCount = visibleMemories.length
  const hasNextPage = (data?.data.length ?? 0) > PAGE_SIZE
  const pageLabel = useMemo(() => Math.floor(offset / PAGE_SIZE) + 1, [offset])

  function applyFilters() {
    setOffset(0)
    setQuery(draftQuery.trim())
    setAgentRole(draftRole)
  }

  function clearFilters() {
    setDraftQuery('')
    setDraftRole('')
    setQuery('')
    setAgentRole('')
    setOffset(0)
  }

  return (
    <div className="space-y-6" data-testid="memories-page">
      <div className="space-y-1">
        <h2 className="text-2xl font-semibold tracking-tight">Memories</h2>
        <p className="text-sm text-muted-foreground">
          Search the shared memory store by situation text and filter by agent role.
        </p>
      </div>

      <Card>
        <CardHeader>
          <CardTitle>Search memories</CardTitle>
          <CardDescription>
            Full-text search runs through the API and returns the most relevant stored situations first.
          </CardDescription>
        </CardHeader>
        <CardContent>
          <form
            className="grid gap-3 md:grid-cols-[minmax(0,1fr)_220px_auto]"
            onSubmit={(event) => {
              event.preventDefault()
              applyFilters()
            }}
          >
            <Input
              value={draftQuery}
              onChange={(event) => setDraftQuery(event.target.value)}
              placeholder="Search situations"
              aria-label="Search memories"
            />
            <select
              value={draftRole}
              onChange={(event) => setDraftRole(event.target.value as AgentRole | '')}
              aria-label="Agent role"
              className="flex h-9 w-full rounded-md border border-input bg-transparent px-3 py-1 text-sm shadow-xs transition-colors focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring"
            >
              <option value="">All agents</option>
              {AGENT_ROLE_OPTIONS.map((role) => (
                <option key={role} value={role}>
                  {formatAgentRole(role)}
                </option>
              ))}
            </select>
            <div className="flex gap-2">
              <Button type="submit" data-testid="apply-memory-filters">
                <Search className="size-4" />
                Search
              </Button>
              <Button type="button" variant="outline" onClick={clearFilters}>
                Clear
              </Button>
            </div>
          </form>
        </CardContent>
      </Card>

      <Card>
        <CardHeader className="gap-3 sm:flex-row sm:items-end sm:justify-between">
          <div className="space-y-1.5">
            <CardTitle>Memory browser</CardTitle>
            <CardDescription>
              {visibleCount
                ? `Showing ${offset + 1}-${offset + visibleCount} on page ${pageLabel}`
                : 'No memories on this page'}
            </CardDescription>
          </div>
          <div className="flex items-center gap-2">
            <Button
              type="button"
              variant="outline"
              size="sm"
              onClick={() => setOffset((current) => Math.max(0, current - PAGE_SIZE))}
              disabled={offset === 0}
            >
              <ChevronLeft className="size-4" />
              Previous
            </Button>
            <Button
              type="button"
              variant="outline"
              size="sm"
              onClick={() => setOffset((current) => current + PAGE_SIZE)}
              disabled={!hasNextPage}
            >
              Next
              <ChevronRight className="size-4" />
            </Button>
          </div>
        </CardHeader>
        <CardContent>
          {isLoading ? (
            <div className="space-y-3" data-testid="memories-loading">
              {Array.from({ length: 3 }).map((_, index) => (
                <div key={index} className="space-y-2 rounded-lg border p-4">
                  <div className="h-4 w-32 animate-pulse rounded bg-muted" />
                  <div className="h-4 w-full animate-pulse rounded bg-muted" />
                  <div className="h-4 w-4/5 animate-pulse rounded bg-muted" />
                </div>
              ))}
            </div>
          ) : isError ? (
            <p className="text-sm text-muted-foreground" data-testid="memories-error">
              Unable to load memories right now. Start the API server to browse stored agent context.
            </p>
          ) : !visibleMemories.length ? (
            <div
              className="flex flex-col items-center gap-2 py-8 text-center"
              data-testid="memories-empty"
            >
              <Brain className="size-8 text-muted-foreground" />
              <p className="text-sm text-muted-foreground">
                No memories matched the current search.
              </p>
            </div>
          ) : (
            <ul className="space-y-3" data-testid="memories-list">
              {visibleMemories.map((memory) => (
                <li key={memory.id}>
                  <article className="rounded-lg border p-4">
                    <div className="flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between">
                      <div className="space-y-3">
                        <div className="flex flex-wrap items-center gap-2">
                          <Badge variant="outline">{formatAgentRole(memory.agent_role)}</Badge>
                          {memory.relevance_score !== undefined ? (
                            <Badge variant="secondary">
                              relevance {memory.relevance_score.toFixed(2)}
                            </Badge>
                          ) : null}
                          <span className="text-xs text-muted-foreground">
                            {formatDate(memory.created_at)}
                          </span>
                        </div>
                        <div className="space-y-2">
                          <div>
                            <p className="text-xs font-medium uppercase tracking-[0.16em] text-muted-foreground">
                              Situation
                            </p>
                            <p className="text-sm">{summarize(memory.situation)}</p>
                          </div>
                          <div>
                            <p className="text-xs font-medium uppercase tracking-[0.16em] text-muted-foreground">
                              Recommendation
                            </p>
                            <p className="text-sm text-muted-foreground">
                              {summarize(memory.recommendation)}
                            </p>
                          </div>
                          <div>
                            <p className="text-xs font-medium uppercase tracking-[0.16em] text-muted-foreground">
                              Outcome
                            </p>
                            <p className="text-sm text-muted-foreground">
                              {memory.outcome ? summarize(memory.outcome) : 'Pending outcome'}
                            </p>
                          </div>
                        </div>
                      </div>
                      <div className="flex shrink-0 gap-2">
                        <Button
                          type="button"
                          variant="outline"
                          size="sm"
                          onClick={() => setSelectedMemory(memory)}
                        >
                          View details
                        </Button>
                        <Button
                          type="button"
                          variant="outline"
                          size="sm"
                          className="text-destructive hover:text-destructive"
                          onClick={() => deleteMutation.mutate(memory.id)}
                          disabled={deleteMutation.isPending}
                          data-testid={`delete-memory-${memory.id}`}
                        >
                          <Trash2 className="size-4" />
                          Delete
                        </Button>
                      </div>
                    </div>
                  </article>
                </li>
              ))}
            </ul>
          )}
        </CardContent>
      </Card>

      <Dialog open={selectedMemory !== null} onOpenChange={(open) => !open && setSelectedMemory(null)}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Memory detail</DialogTitle>
            <DialogDescription>
              Review the full memory contents before acting on or deleting it.
            </DialogDescription>
          </DialogHeader>
          {selectedMemory ? (
            <div className="space-y-4" data-testid="memory-detail-dialog">
              <div className="flex flex-wrap items-center gap-2">
                <Badge variant="outline">{formatAgentRole(selectedMemory.agent_role)}</Badge>
                <span className="text-xs text-muted-foreground">
                  {formatDate(selectedMemory.created_at)}
                </span>
              </div>
              <section className="space-y-1">
                <h3 className="text-sm font-medium">Situation</h3>
                <p className="text-sm text-muted-foreground">{selectedMemory.situation}</p>
              </section>
              <section className="space-y-1">
                <h3 className="text-sm font-medium">Recommendation</h3>
                <p className="text-sm text-muted-foreground">{selectedMemory.recommendation}</p>
              </section>
              <section className="space-y-1">
                <h3 className="text-sm font-medium">Outcome</h3>
                <p className="text-sm text-muted-foreground">
                  {selectedMemory.outcome || 'Pending outcome'}
                </p>
              </section>
              {selectedMemory.pipeline_run_id ? (
                <section className="space-y-1">
                  <h3 className="text-sm font-medium">Pipeline run</h3>
                  <p className="text-sm text-muted-foreground">{selectedMemory.pipeline_run_id}</p>
                </section>
              ) : null}
              <DialogFooter>
                <Button type="button" variant="outline" onClick={() => setSelectedMemory(null)}>
                  Close
                </Button>
                <Button
                  type="button"
                  variant="outline"
                  className="text-destructive hover:text-destructive"
                  onClick={() => deleteMutation.mutate(selectedMemory.id)}
                  disabled={deleteMutation.isPending}
                >
                  <Trash2 className="size-4" />
                  Delete memory
                </Button>
              </DialogFooter>
            </div>
          ) : null}
        </DialogContent>
      </Dialog>
    </div>
  )
}
