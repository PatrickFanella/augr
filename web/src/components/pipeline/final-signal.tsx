import { Badge } from '@/components/ui/badge';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import type { AgentDecision, PipelineSignal } from '@/lib/api/types';

interface FinalSignalProps {
  signal?: PipelineSignal;
  signalDecision: AgentDecision | undefined;
  onSelectDecision: (decision: AgentDecision) => void;
}

function signalVariant(signal: PipelineSignal) {
  switch (signal) {
    case 'buy':
      return 'success' as const;
    case 'sell':
      return 'destructive' as const;
    default:
      return 'secondary' as const;
  }
}

function parseDecisionPayload(decision: AgentDecision): Record<string, unknown> | null {
  if (
    decision.output_structured &&
    typeof decision.output_structured === 'object' &&
    !Array.isArray(decision.output_structured)
  ) {
    return decision.output_structured as Record<string, unknown>;
  }

  try {
    const parsed = JSON.parse(decision.output_text);
    if (parsed && typeof parsed === 'object' && !Array.isArray(parsed)) {
      return parsed as Record<string, unknown>;
    }
  } catch {
    // Fall back to regex extraction when the payload is plain text.
  }

  return null;
}

function normalizeConfidence(value: number): number {
  return value > 1 ? value : value * 100;
}

function extractConfidence(decision: AgentDecision): number | null {
  const payload = parseDecisionPayload(decision);
  if (payload && typeof payload.confidence === 'number') {
    return normalizeConfidence(payload.confidence);
  }
  const match = decision.output_text.match(/confidence[:\s]*(\d+(?:\.\d+)?)\s*%?/i);
  if (match) {
    const value = parseFloat(match[1]);
    return value > 1 ? value : value * 100;
  }
  return null;
}

function extractSignal(decision: AgentDecision): PipelineSignal | undefined {
  const payload = parseDecisionPayload(decision);
  const candidate = payload?.action ?? payload?.signal;
  if (typeof candidate === 'string') {
    const normalized = candidate.trim().toLowerCase();
    if (normalized === 'buy' || normalized === 'sell' || normalized === 'hold') {
      return normalized;
    }
  }

  const match = decision.output_text.match(/(?:action|signal)["'\s:]*([a-z]+)/i);
  if (!match) {
    return undefined;
  }

  const normalized = match[1].trim().toLowerCase();
  if (normalized === 'buy' || normalized === 'sell' || normalized === 'hold') {
    return normalized;
  }

  return undefined;
}

export function FinalSignal({ signal, signalDecision, onSelectDecision }: FinalSignalProps) {
  const resolvedSignal = signal ?? (signalDecision ? extractSignal(signalDecision) : undefined);
  const confidence = signalDecision ? extractConfidence(signalDecision) : null;

  return (
    <div data-testid="final-signal">
      <h3 className="mb-3 text-sm font-semibold text-muted-foreground">Phase 5 — Final Signal</h3>
      <Card
        className={signalDecision ? 'cursor-pointer transition-shadow hover:shadow-md' : ''}
        onClick={() => signalDecision && onSelectDecision(signalDecision)}
        role={signalDecision ? 'button' : undefined}
        tabIndex={signalDecision ? 0 : -1}
        onKeyDown={(event) => {
          if (!signalDecision) return;
          if (event.key === 'Enter' || event.key === ' ') {
            event.preventDefault();
            onSelectDecision(signalDecision);
          }
        }}
      >
        <CardHeader className="pb-2">
          <CardTitle className="text-sm">Risk Manager Verdict</CardTitle>
        </CardHeader>
        <CardContent>
          {resolvedSignal ? (
            <div className="flex flex-col gap-3">
              <div className="flex items-center gap-3">
                <Badge variant={signalVariant(resolvedSignal)} className="text-base uppercase">
                  {resolvedSignal}
                </Badge>
                {confidence !== null && (
                  <span className="text-sm text-muted-foreground" data-testid="confidence-score">
                    {confidence.toFixed(0)}% confidence
                  </span>
                )}
              </div>
              {signalDecision && (
                <p className="line-clamp-4 text-xs text-muted-foreground">
                  {signalDecision.output_text.slice(0, 400)}
                </p>
              )}
            </div>
          ) : signalDecision ? (
            <p className="line-clamp-4 text-xs text-muted-foreground">
              {signalDecision.output_text.slice(0, 400)}
            </p>
          ) : (
            <p className="text-xs text-muted-foreground">Waiting for final signal…</p>
          )}
        </CardContent>
      </Card>
    </div>
  );
}
