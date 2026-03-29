import { CheckCircle2, Loader2 } from 'lucide-react'

import { Badge } from '@/components/ui/badge'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import type { AgentDecision, AgentRole } from '@/lib/api/types'

const analystRoles: AgentRole[] = [
  'market_analyst',
  'fundamentals_analyst',
  'news_analyst',
  'social_media_analyst',
]

const analystLabels: Record<string, string> = {
  market_analyst: 'Market Analyst',
  fundamentals_analyst: 'Fundamentals Analyst',
  news_analyst: 'News Analyst',
  social_media_analyst: 'Social Media Analyst',
}

interface AnalystCardsProps {
  decisions: AgentDecision[]
  onSelectDecision: (decision: AgentDecision) => void
}

export function AnalystCards({ decisions, onSelectDecision }: AnalystCardsProps) {
  const decisionsByRole = new Map<AgentRole, AgentDecision>()
  for (const d of decisions) {
    if (analystRoles.includes(d.agent_role)) {
      decisionsByRole.set(d.agent_role, d)
    }
  }

  return (
    <div data-testid="analyst-cards">
      <h3 className="mb-3 text-sm font-semibold text-muted-foreground">Phase 1 — Analysis</h3>
      <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
        {analystRoles.map((role) => {
          const decision = decisionsByRole.get(role)
          return (
            <Card
              key={role}
              className={decision ? 'cursor-pointer transition-shadow hover:shadow-md' : ''}
              onClick={() => decision && onSelectDecision(decision)}
              role={decision ? 'button' : undefined}
              tabIndex={decision ? 0 : -1}
              aria-disabled={!decision}
              onKeyDown={(event) => {
                if (!decision) return
                if (event.key === 'Enter' || event.key === ' ') {
                  event.preventDefault()
                  onSelectDecision(decision)
                }
              }}
              data-testid={`analyst-card-${role}`}
            >
              <CardHeader className="pb-2">
                <CardTitle className="flex items-center justify-between text-sm">
                  {analystLabels[role]}
                  {decision ? (
                    <CheckCircle2 className="size-4 text-primary" />
                  ) : (
                    <Loader2 className="size-4 animate-spin text-muted-foreground" />
                  )}
                </CardTitle>
              </CardHeader>
              <CardContent>
                {decision ? (
                  <>
                    <p className="line-clamp-3 text-xs text-muted-foreground">
                      {decision.output_text.slice(0, 200)}
                    </p>
                    {decision.latency_ms !== undefined && (
                      <Badge variant="outline" className="mt-2 text-[10px]">
                        {decision.latency_ms}ms
                      </Badge>
                    )}
                  </>
                ) : (
                  <p className="text-xs text-muted-foreground">Waiting for result…</p>
                )}
              </CardContent>
            </Card>
          )
        })}
      </div>
    </div>
  )
}
