import { X } from 'lucide-react'

import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import type { AgentDecision } from '@/lib/api/types'

const roleLabels: Record<string, string> = {
  market_analyst: 'Market Analyst',
  fundamentals_analyst: 'Fundamentals Analyst',
  news_analyst: 'News Analyst',
  social_media_analyst: 'Social Media Analyst',
  bull_researcher: 'Bull Researcher',
  bear_researcher: 'Bear Researcher',
  trader: 'Trader',
  invest_judge: 'Investment Judge',
  risk_manager: 'Risk Manager',
  aggressive_analyst: 'Aggressive Analyst',
  conservative_analyst: 'Conservative Analyst',
  neutral_analyst: 'Neutral Analyst',
  aggressive_risk: 'Aggressive Risk',
  conservative_risk: 'Conservative Risk',
  neutral_risk: 'Neutral Risk',
}

interface DecisionInspectorProps {
  decision: AgentDecision
  onClose: () => void
}

export function DecisionInspector({ decision, onClose }: DecisionInspectorProps) {
  const totalTokens = (decision.prompt_tokens ?? 0) + (decision.completion_tokens ?? 0)

  return (
    <Card data-testid="decision-inspector">
      <CardHeader className="flex flex-row items-start justify-between space-y-0 pb-3">
        <div>
          <CardTitle className="text-base">
            {roleLabels[decision.agent_role] ?? decision.agent_role}
          </CardTitle>
          <p className="text-xs text-muted-foreground">
            Phase: {decision.phase}
            {decision.round_number ? ` · Round ${decision.round_number}` : ''}
          </p>
        </div>
        <Button
          variant="outline"
          size="sm"
          onClick={onClose}
          data-testid="inspector-close"
          aria-label="Close decision inspector"
        >
          <X className="size-4" />
        </Button>
      </CardHeader>
      <CardContent className="space-y-4">
        <div className="flex flex-wrap gap-2">
          {decision.llm_model && (
            <Badge variant="outline">{decision.llm_model}</Badge>
          )}
          {decision.latency_ms !== undefined && (
            <Badge variant="outline">{decision.latency_ms}ms</Badge>
          )}
          {totalTokens > 0 && (
            <Badge variant="outline" data-testid="inspector-tokens">
              {totalTokens} tokens
            </Badge>
          )}
          {decision.prompt_tokens !== undefined && (
            <Badge variant="secondary">Prompt: {decision.prompt_tokens}</Badge>
          )}
          {decision.completion_tokens !== undefined && (
            <Badge variant="secondary">Completion: {decision.completion_tokens}</Badge>
          )}
        </div>

        {decision.input_summary && (
          <div>
            <h4 className="mb-1 text-xs font-semibold text-muted-foreground">Prompt Summary</h4>
            <p className="whitespace-pre-wrap rounded-md border bg-muted/50 p-3 text-xs" data-testid="inspector-prompt">
              {decision.input_summary}
            </p>
          </div>
        )}

        <div>
          <h4 className="mb-1 text-xs font-semibold text-muted-foreground">Response</h4>
          <p className="whitespace-pre-wrap rounded-md border bg-muted/50 p-3 text-xs" data-testid="inspector-response">
            {decision.output_text}
          </p>
        </div>
      </CardContent>
    </Card>
  )
}
