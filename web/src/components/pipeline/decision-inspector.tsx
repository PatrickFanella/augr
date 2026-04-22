import { X } from 'lucide-react';

import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Dialog, DialogContent } from '@/components/ui/dialog';
import type { AgentDecision } from '@/lib/api/types';

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
};

interface DecisionInspectorProps {
  decision: AgentDecision | null;
  onClose: () => void;
}

export function DecisionInspector({ decision, onClose }: DecisionInspectorProps) {
  if (!decision) return null;

  const totalTokens = (decision.prompt_tokens ?? 0) + (decision.completion_tokens ?? 0);

  return (
    <Dialog open={!!decision} onOpenChange={(open) => !open && onClose()}>
      <DialogContent
        className="max-h-[85vh] w-full max-w-3xl overflow-y-auto"
        data-testid="decision-inspector"
      >
        <div className="flex items-start justify-between pb-3">
          <div>
            <h2 className="text-base font-semibold">
              {roleLabels[decision.agent_role] ?? decision.agent_role}
            </h2>
            <p className="font-mono text-[11px] uppercase tracking-[0.16em] text-muted-foreground">
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
        </div>

        <div className="space-y-4">
          <div className="flex flex-wrap gap-2">
            {decision.llm_provider && <Badge variant="outline">{decision.llm_provider}</Badge>}
            {decision.llm_model && <Badge variant="outline">{decision.llm_model}</Badge>}
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
            {decision.cost_usd != null && decision.cost_usd > 0 && (
              <Badge variant="secondary">${decision.cost_usd.toFixed(4)}</Badge>
            )}
          </div>

          {decision.prompt_text && (
            <section className="space-y-1.5">
              <h4 className="font-mono text-[11px] font-medium uppercase tracking-[0.16em] text-muted-foreground">
                Full Prompt
              </h4>
              <pre
                className="max-h-64 overflow-y-auto whitespace-pre-wrap rounded-md border border-border bg-background p-3 font-mono text-[12px] leading-5 text-muted-foreground"
                data-testid="inspector-prompt-text"
              >
                {decision.prompt_text}
              </pre>
            </section>
          )}

          {decision.input_summary && !decision.prompt_text && (
            <section className="space-y-1.5">
              <h4 className="font-mono text-[11px] font-medium uppercase tracking-[0.16em] text-muted-foreground">
                Prompt Summary
              </h4>
              <pre
                className="max-h-64 overflow-y-auto whitespace-pre-wrap rounded-md border border-border bg-background p-3 font-mono text-[12px] leading-5 text-muted-foreground"
                data-testid="inspector-prompt"
              >
                {decision.input_summary}
              </pre>
            </section>
          )}

          <section className="space-y-1.5">
            <h4 className="font-mono text-[11px] font-medium uppercase tracking-[0.16em] text-muted-foreground">
              LLM Response
            </h4>
            <pre
              className="overflow-y-auto whitespace-pre-wrap rounded-md border border-border bg-background p-3 font-mono text-[12px] leading-5 text-foreground"
              data-testid="inspector-response"
            >
              {decision.output_text}
            </pre>
          </section>

          {decision.output_structured != null && (
            <section className="space-y-1.5">
              <h4 className="font-mono text-[11px] font-medium uppercase tracking-[0.16em] text-muted-foreground">
                Structured Output
              </h4>
              <pre
                className="max-h-64 overflow-y-auto whitespace-pre-wrap rounded-md border border-border bg-background p-3 font-mono text-[12px] leading-5 text-muted-foreground"
                data-testid="inspector-structured"
              >
                {JSON.stringify(decision.output_structured, null, 2)}
              </pre>
            </section>
          )}

          <p className="text-right text-[10px] text-muted-foreground">
            {new Date(decision.created_at).toLocaleString()}
          </p>
        </div>
      </DialogContent>
    </Dialog>
  );
}
