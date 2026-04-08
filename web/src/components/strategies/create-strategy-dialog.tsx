import { type FormEvent, useState } from 'react';

import { Button } from '@/components/ui/button';
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { Textarea } from '@/components/ui/textarea';
import type { MarketType, StrategyCreateRequest } from '@/lib/api/types';
import { describeCron } from '@/lib/cron-describe';
import {
  defaultAnalysts,
  llmProviderOptions,
  strategyConfigBoundary,
  type StrategyConfigForm,
} from '@/lib/strategy-config/boundary';

interface CreateStrategyDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onSubmit: (data: StrategyCreateRequest) => void;
  isSubmitting?: boolean;
}

const marketTypes: MarketType[] = ['stock', 'crypto', 'polymarket'];
const denseSelectClassName =
  'flex h-9 w-full rounded-md border border-input bg-card px-3 py-1 text-sm text-foreground transition-colors focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring';

export function CreateStrategyDialog({
  open,
  onOpenChange,
  onSubmit,
  isSubmitting,
}: CreateStrategyDialogProps) {
  const [name, setName] = useState('');
  const [description, setDescription] = useState('');
  const [ticker, setTicker] = useState('');
  const [marketType, setMarketType] = useState<MarketType>('stock');
  const [scheduleCron, setScheduleCron] = useState('');
  const [isPaper, setIsPaper] = useState(true);
  const [isActive, setIsActive] = useState(false);
  const [configForm, setConfigForm] = useState<StrategyConfigForm>(() =>
    strategyConfigBoundary.load(null),
  );
  const [showAdvanced, setShowAdvanced] = useState(false);
  const [fieldErrors, setFieldErrors] = useState<Record<string, string>>({});

  function setPipeline(patch: Partial<StrategyConfigForm['pipeline']>) {
    setConfigForm((prev) => ({ ...prev, pipeline: { ...prev.pipeline, ...patch } }));
    setFieldErrors({});
  }
  function setRisk(patch: Partial<StrategyConfigForm['risk']>) {
    setConfigForm((prev) => ({ ...prev, risk: { ...prev.risk, ...patch } }));
    setFieldErrors({});
  }
  function setLlm(patch: Partial<StrategyConfigForm['llm']>) {
    setConfigForm((prev) => ({ ...prev, llm: { ...prev.llm, ...patch } }));
    setFieldErrors({});
  }

  function toggleAnalyst(role: (typeof defaultAnalysts)[number], checked: boolean) {
    setConfigForm((prev) => {
      const next = checked
        ? prev.analysts.selected.includes(role)
          ? prev.analysts.selected
          : [...prev.analysts.selected, role]
        : prev.analysts.selected.filter((r) => r !== role);
      return { ...prev, analysts: { mode: 'custom', selected: next } };
    });
    setFieldErrors({});
  }

  function resetForm() {
    setName('');
    setDescription('');
    setTicker('');
    setMarketType('stock');
    setScheduleCron('');
    setIsPaper(true);
    setIsActive(false);
    setConfigForm(strategyConfigBoundary.load(null));
    setShowAdvanced(false);
    setFieldErrors({});
  }

  function handleOpenChange(nextOpen: boolean) {
    if (!nextOpen) resetForm();
    onOpenChange(nextOpen);
  }

  function handleSubmit(e: FormEvent) {
    e.preventDefault();

    const result = strategyConfigBoundary.submit(configForm);
    if (!result.ok) {
      setFieldErrors(result.fieldErrors);
      return;
    }

    setFieldErrors({});
    onSubmit({
      name,
      description: description || undefined,
      ticker: marketType === 'polymarket' ? ticker.toLowerCase() : ticker.toUpperCase(),
      market_type: marketType,
      schedule_cron: scheduleCron || undefined,
      config: result.config,
      status: isActive ? 'active' : 'inactive',
      is_paper: isPaper,
    });
  }

  const firstError = Object.values(fieldErrors)[0];

  return (
    <Dialog open={open} onOpenChange={handleOpenChange}>
      <DialogContent data-testid="create-strategy-dialog">
        <DialogHeader>
          <DialogTitle>Create strategy</DialogTitle>
          <DialogDescription>
            Configure a new trading strategy. Required fields are marked with *.
          </DialogDescription>
        </DialogHeader>

        <form onSubmit={handleSubmit} className="space-y-4">
          <div className="grid gap-4 sm:grid-cols-2">
            <div className="space-y-2 rounded-lg border border-border bg-background p-4">
              <Label htmlFor="strategy-name">Name *</Label>
              <Input
                id="strategy-name"
                value={name}
                onChange={(e) => setName(e.target.value)}
                placeholder="AAPL Momentum"
                required
                data-testid="strategy-name-input"
              />
            </div>

            <div className="space-y-2 rounded-lg border border-border bg-background p-4">
              <Label htmlFor="strategy-ticker">
                {marketType === 'polymarket' ? 'Market slug *' : 'Ticker *'}
              </Label>
              <Input
                id="strategy-ticker"
                value={ticker}
                onChange={(e) => setTicker(e.target.value)}
                placeholder={marketType === 'polymarket' ? 'will-x-happen-by-date' : 'AAPL'}
                required
                data-testid="strategy-ticker-input"
              />
              {marketType === 'polymarket' && (
                <p className="text-[11px] text-muted-foreground">
                  Enter the Polymarket market slug (e.g. will-trump-win-2024). The price shown is
                  the YES token probability (0–1).
                </p>
              )}
            </div>
          </div>

          <div className="space-y-2 rounded-lg border border-border bg-background p-4">
            <Label htmlFor="strategy-description">Description</Label>
            <Input
              id="strategy-description"
              value={description}
              onChange={(e) => setDescription(e.target.value)}
              placeholder="Optional description"
            />
          </div>

          <div className="grid gap-4 sm:grid-cols-2">
            <div className="space-y-2 rounded-lg border border-border bg-background p-4">
              <Label htmlFor="strategy-market-type">Market type *</Label>
              <select
                id="strategy-market-type"
                value={marketType}
                onChange={(e) => setMarketType(e.target.value as MarketType)}
                className={denseSelectClassName}
                data-testid="strategy-market-type-select"
              >
                {marketTypes.map((mt) => (
                  <option key={mt} value={mt}>
                    {mt}
                  </option>
                ))}
              </select>
            </div>

            <div className="space-y-2 rounded-lg border border-border bg-background p-4">
              <Label htmlFor="strategy-schedule">Schedule (cron)</Label>
              <Input
                id="strategy-schedule"
                value={scheduleCron}
                onChange={(e) => setScheduleCron(e.target.value)}
                placeholder="0 9 * * 1-5"
              />
              {scheduleCron && (
                <p className="text-xs text-muted-foreground mt-1">{describeCron(scheduleCron)}</p>
              )}
            </div>
          </div>

          <div className="grid gap-3 sm:grid-cols-2">
            <label className="flex items-center gap-2 rounded-lg border border-border bg-background px-4 py-3 text-sm">
              <input
                type="checkbox"
                checked={isPaper}
                onChange={(e) => setIsPaper(e.target.checked)}
                className="rounded border-input"
                data-testid="strategy-paper-checkbox"
              />
              Paper trading
            </label>
            <label className="flex items-center gap-2 rounded-lg border border-border bg-background px-4 py-3 text-sm">
              <input
                type="checkbox"
                checked={isActive}
                onChange={(e) => setIsActive(e.target.checked)}
                className="rounded border-input"
                data-testid="strategy-active-checkbox"
              />
              Active
            </label>
          </div>

          <div className="space-y-4 rounded-lg border border-border bg-background p-4">
            <h4 className="font-mono text-[11px] font-medium uppercase tracking-[0.16em] text-muted-foreground">
              Pipeline
            </h4>
            <div className="grid gap-4 sm:grid-cols-2">
              <div className="space-y-2">
                <Label htmlFor="create-debate-rounds">Debate Rounds</Label>
                <Input
                  id="create-debate-rounds"
                  type="number"
                  min={1}
                  max={10}
                  value={configForm.pipeline.debateRounds}
                  onChange={(e) => setPipeline({ debateRounds: e.target.value })}
                />
              </div>
              <div className="space-y-2">
                <Label htmlFor="create-phase-timeout">Analysis Timeout (seconds)</Label>
                <Input
                  id="create-phase-timeout"
                  type="number"
                  min={1}
                  value={configForm.pipeline.analysisTimeoutSeconds}
                  onChange={(e) => setPipeline({ analysisTimeoutSeconds: e.target.value })}
                  placeholder="120"
                />
              </div>
            </div>
            <div className="space-y-2">
              <Label htmlFor="create-pipeline-timeout">Debate Timeout (seconds)</Label>
              <Input
                id="create-pipeline-timeout"
                type="number"
                min={1}
                value={configForm.pipeline.debateTimeoutSeconds}
                onChange={(e) => setPipeline({ debateTimeoutSeconds: e.target.value })}
                placeholder="600"
              />
            </div>
          </div>

          <div className="space-y-4 rounded-lg border border-border bg-background p-4">
            <h4 className="font-mono text-[11px] font-medium uppercase tracking-[0.16em] text-muted-foreground">
              Risk
            </h4>
            <div className="grid gap-4 sm:grid-cols-2">
              <div className="space-y-2">
                <Label htmlFor="create-max-position-size-pct">Max Position Size %</Label>
                <Input
                  id="create-max-position-size-pct"
                  type="number"
                  step="0.01"
                  min={0.01}
                  max={1}
                  value={configForm.risk.positionSizePct}
                  onChange={(e) => setRisk({ positionSizePct: e.target.value })}
                />
              </div>
              <div className="space-y-2">
                <Label htmlFor="create-stop-loss-atr-multiplier">Stop Loss ATR Multiplier</Label>
                <Input
                  id="create-stop-loss-atr-multiplier"
                  type="number"
                  step="0.1"
                  value={configForm.risk.stopLossMultiplier}
                  onChange={(e) => setRisk({ stopLossMultiplier: e.target.value })}
                />
              </div>
            </div>
            <div className="grid gap-4 sm:grid-cols-2">
              <div className="space-y-2">
                <Label htmlFor="create-take-profit-atr-multiplier">
                  Take Profit ATR Multiplier
                </Label>
                <Input
                  id="create-take-profit-atr-multiplier"
                  type="number"
                  step="0.1"
                  value={configForm.risk.takeProfitMultiplier}
                  onChange={(e) => setRisk({ takeProfitMultiplier: e.target.value })}
                />
              </div>
              <div className="space-y-2">
                <Label htmlFor="create-min-confidence-threshold">Min Confidence Threshold</Label>
                <Input
                  id="create-min-confidence-threshold"
                  type="number"
                  step="0.01"
                  min={0}
                  max={1}
                  value={configForm.risk.minConfidence}
                  onChange={(e) => setRisk({ minConfidence: e.target.value })}
                />
              </div>
            </div>
          </div>

          <div className="space-y-4 rounded-lg border border-border bg-background p-4">
            <h4 className="font-mono text-[11px] font-medium uppercase tracking-[0.16em] text-muted-foreground">
              Analysts
            </h4>
            <div className="grid gap-3 sm:grid-cols-2">
              {defaultAnalysts.map((role) => (
                <label
                  key={role}
                  className="flex items-center gap-2 rounded-md border border-border bg-background px-3 py-2 text-sm"
                >
                  <input
                    type="checkbox"
                    checked={configForm.analysts.selected.includes(role)}
                    onChange={(e) => toggleAnalyst(role, e.target.checked)}
                    className="rounded border-input"
                  />
                  {role.replace(/_/g, ' ')}
                </label>
              ))}
            </div>
          </div>

          <div className="space-y-3 rounded-lg border border-border bg-background p-4">
            <div className="flex items-center justify-between">
              <h4 className="font-mono text-[11px] font-medium uppercase tracking-[0.16em] text-muted-foreground">
                Advanced
              </h4>
              <Button
                type="button"
                variant="outline"
                size="dense"
                onClick={() => setShowAdvanced((prev) => !prev)}
              >
                {showAdvanced ? 'Hide' : 'Show'}
              </Button>
            </div>
            {showAdvanced ? (
              <div className="space-y-4">
                <div className="grid gap-4 sm:grid-cols-2">
                  <div className="space-y-2">
                    <Label htmlFor="create-llm-provider">LLM Provider</Label>
                    <select
                      id="create-llm-provider"
                      value={configForm.llm.provider}
                      onChange={(e) => setLlm({ provider: e.target.value as '' })}
                      className={denseSelectClassName}
                    >
                      <option value="">Use global default</option>
                      {llmProviderOptions.map((p) => (
                        <option key={p} value={p}>
                          {p}
                        </option>
                      ))}
                    </select>
                  </div>
                  <div className="space-y-2">
                    <Label htmlFor="create-deep-think-model">Deep Think Model</Label>
                    <Input
                      id="create-deep-think-model"
                      value={configForm.llm.deepThinkModel}
                      onChange={(e) => setLlm({ deepThinkModel: e.target.value })}
                      placeholder="Global default"
                    />
                  </div>
                </div>
                <div className="space-y-2">
                  <Label htmlFor="create-quick-think-model">Quick Think Model</Label>
                  <Input
                    id="create-quick-think-model"
                    value={configForm.llm.quickThinkModel}
                    onChange={(e) => setLlm({ quickThinkModel: e.target.value })}
                    placeholder="Global default"
                  />
                </div>
                <div className="space-y-2">
                  <Label htmlFor="create-prompt-overrides">Prompt Overrides (JSON)</Label>
                  <Textarea
                    id="create-prompt-overrides"
                    value={configForm.promptOverridesJson}
                    onChange={(e) => {
                      setConfigForm((prev) => ({ ...prev, promptOverridesJson: e.target.value }));
                      setFieldErrors({});
                    }}
                    rows={6}
                    className="font-mono text-xs"
                  />
                </div>
              </div>
            ) : null}
          </div>

          {firstError ? (
            <p
              className="rounded-md border border-destructive/20 bg-destructive/10 px-3 py-2 text-xs text-destructive"
              data-testid="config-error"
            >
              {firstError}
            </p>
          ) : null}

          <DialogFooter>
            <Button
              type="button"
              variant="outline"
              size="dense"
              onClick={() => onOpenChange(false)}
            >
              Cancel
            </Button>
            <Button type="submit" size="dense" disabled={isSubmitting || !name || !ticker}>
              {isSubmitting ? 'Creating…' : 'Create strategy'}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
