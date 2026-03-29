import { Badge } from '@/components/ui/badge'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import type { AgentDecision, PipelineSignal } from '@/lib/api/types'

interface FinalSignalProps {
  signal?: PipelineSignal
  judgeDecision: AgentDecision | undefined
  onSelectDecision: (decision: AgentDecision) => void
}

function signalVariant(signal: PipelineSignal) {
  switch (signal) {
    case 'buy':
      return 'success' as const
    case 'sell':
      return 'destructive' as const
    default:
      return 'secondary' as const
  }
}

function extractConfidence(decision: AgentDecision): number | null {
  if (decision.output_structured && typeof decision.output_structured === 'object') {
    const structured = decision.output_structured as Record<string, unknown>
    if (typeof structured.confidence === 'number') return structured.confidence
  }
  const match = decision.output_text.match(/confidence[:\s]*(\d+(?:\.\d+)?)\s*%?/i)
  if (match) {
    const value = parseFloat(match[1])
    return value > 1 ? value : value * 100
  }
  return null
}

export function FinalSignal({ signal, judgeDecision, onSelectDecision }: FinalSignalProps) {
  const confidence = judgeDecision ? extractConfidence(judgeDecision) : null

  return (
    <div data-testid="final-signal">
      <h3 className="mb-3 text-sm font-semibold text-muted-foreground">Phase 5 — Final Signal</h3>
      <Card
        className={judgeDecision ? 'cursor-pointer transition-shadow hover:shadow-md' : ''}
        onClick={() => judgeDecision && onSelectDecision(judgeDecision)}
        role={judgeDecision ? 'button' : undefined}
        tabIndex={judgeDecision ? 0 : -1}
        onKeyDown={(event) => {
          if (!judgeDecision) return
          if (event.key === 'Enter' || event.key === ' ') {
            event.preventDefault()
            onSelectDecision(judgeDecision)
          }
        }}
      >
        <CardHeader className="pb-2">
          <CardTitle className="text-sm">Investment Judge</CardTitle>
        </CardHeader>
        <CardContent>
          {signal ? (
            <div className="flex flex-col gap-3">
              <div className="flex items-center gap-3">
                <Badge variant={signalVariant(signal)} className="text-base uppercase">
                  {signal}
                </Badge>
                {confidence !== null && (
                  <span className="text-sm text-muted-foreground" data-testid="confidence-score">
                    {confidence.toFixed(0)}% confidence
                  </span>
                )}
              </div>
              {judgeDecision && (
                <p className="line-clamp-4 text-xs text-muted-foreground">
                  {judgeDecision.output_text.slice(0, 400)}
                </p>
              )}
            </div>
          ) : (
            <p className="text-xs text-muted-foreground">Waiting for final signal…</p>
          )}
        </CardContent>
      </Card>
    </div>
  )
}
