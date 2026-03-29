import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import type { AgentDecision } from '@/lib/api/types'

interface TraderPlanProps {
  decision: AgentDecision | undefined
  onSelectDecision: (decision: AgentDecision) => void
}

export function TraderPlan({ decision, onSelectDecision }: TraderPlanProps) {
  return (
    <div data-testid="trader-plan">
      <h3 className="mb-3 text-sm font-semibold text-muted-foreground">Phase 3 — Trading Plan</h3>
      <Card
        className={decision ? 'cursor-pointer transition-shadow hover:shadow-md' : ''}
        role={decision ? 'button' : undefined}
        tabIndex={decision ? 0 : undefined}
        onClick={() => decision && onSelectDecision(decision)}
        onKeyDown={(event) => {
          if (!decision) return
          if (event.key === 'Enter' || event.key === ' ') {
            event.preventDefault()
            onSelectDecision(decision)
          }
        }}
      >
        <CardHeader className="pb-2">
          <CardTitle className="text-sm">Trader</CardTitle>
        </CardHeader>
        <CardContent>
          {decision ? (
            <p className="whitespace-pre-wrap text-xs text-muted-foreground">
              {decision.output_text.slice(0, 600)}
            </p>
          ) : (
            <p className="text-xs text-muted-foreground">Waiting for trading plan…</p>
          )}
        </CardContent>
      </Card>
    </div>
  )
}
