import { ChevronLeft, ChevronRight } from 'lucide-react'
import { useMemo, useState } from 'react'

import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import type { AgentDecision, AgentRole } from '@/lib/api/types'

const debateLabels: Record<string, string> = {
  bull_researcher: 'Bull',
  bear_researcher: 'Bear',
  aggressive_risk: 'Aggressive',
  conservative_risk: 'Conservative',
  neutral_risk: 'Neutral',
}

interface DebateViewProps {
  title: string
  roles: AgentRole[]
  decisions: AgentDecision[]
  onSelectDecision: (decision: AgentDecision) => void
}

export function DebateView({ title, roles, decisions, onSelectDecision }: DebateViewProps) {
  const rounds = useMemo(() => {
    const roundMap = new Map<number, AgentDecision[]>()
    for (const d of decisions) {
      if (roles.includes(d.agent_role)) {
        const round = d.round_number ?? 1
        const existing = roundMap.get(round) ?? []
        existing.push(d)
        roundMap.set(round, existing)
      }
    }
    return Array.from(roundMap.entries())
      .sort(([a], [b]) => a - b)
      .map(([round, entries]) => ({ round, entries }))
  }, [decisions, roles])

  const [currentRoundIdx, setCurrentRoundIdx] = useState(0)

  const maxIdx = Math.max(0, rounds.length - 1)
  const activeRound = rounds[currentRoundIdx] ?? null

  return (
    <div data-testid="debate-view">
      <div className="mb-3 flex items-center justify-between">
        <h3 className="text-sm font-semibold text-muted-foreground">{title}</h3>
        {rounds.length > 1 && (
          <div className="flex items-center gap-1">
            <Button
              variant="outline"
              size="sm"
              disabled={currentRoundIdx === 0}
              onClick={() => setCurrentRoundIdx((i) => Math.max(0, i - 1))}
              data-testid="debate-prev-round"
            >
              <ChevronLeft className="size-4" />
            </Button>
            <span className="px-2 text-xs text-muted-foreground" data-testid="debate-round-indicator">
              Round {activeRound?.round ?? currentRoundIdx + 1} / {rounds.length}
            </span>
            <Button
              variant="outline"
              size="sm"
              disabled={currentRoundIdx >= maxIdx}
              onClick={() => setCurrentRoundIdx((i) => Math.min(maxIdx, i + 1))}
              data-testid="debate-next-round"
            >
              <ChevronRight className="size-4" />
            </Button>
          </div>
        )}
      </div>

      {!activeRound ? (
        <Card>
          <CardContent className="py-8">
            <p className="text-center text-sm text-muted-foreground">Waiting for debate to begin…</p>
          </CardContent>
        </Card>
      ) : (
        <div className={`grid gap-3 ${roles.length <= 2 ? 'sm:grid-cols-2' : 'sm:grid-cols-3'}`}>
          {roles.map((role) => {
            const entry = activeRound.entries
              .filter((e) => e.agent_role === role)
              .sort((a, b) => new Date(b.created_at).getTime() - new Date(a.created_at).getTime())[0]
            return (
              <Card
                key={role}
                className={entry ? 'cursor-pointer transition-shadow hover:shadow-md' : ''}
                onClick={() => entry && onSelectDecision(entry)}
                role={entry ? 'button' : undefined}
                tabIndex={entry ? 0 : undefined}
                onKeyDown={(event) => {
                  if (!entry) return
                  if (event.key === 'Enter' || event.key === ' ') {
                    event.preventDefault()
                    onSelectDecision(entry)
                  }
                }}
                data-testid={`debate-card-${role}`}
              >
                <CardHeader className="pb-2">
                  <CardTitle className="text-sm">{debateLabels[role] ?? role}</CardTitle>
                </CardHeader>
                <CardContent>
                  {entry ? (
                    <p className="line-clamp-6 text-xs text-muted-foreground">
                      {entry.output_text.slice(0, 400)}
                    </p>
                  ) : (
                    <p className="text-xs text-muted-foreground">Waiting…</p>
                  )}
                </CardContent>
              </Card>
            )
          })}
        </div>
      )}
    </div>
  )
}
