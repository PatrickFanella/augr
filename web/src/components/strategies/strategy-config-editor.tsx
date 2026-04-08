import { type FormEvent, useEffect, useMemo, useState } from 'react';

import { Button } from '@/components/ui/button';
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { Textarea } from '@/components/ui/textarea';
import type {
  AgentRole,
  MarketType,
  Settings,
  Strategy,
  StrategyStatus,
  StrategyUpdateRequest,
} from '@/lib/api/types';
import {
  defaultAnalysts,
  strategyConfigBoundary,
  type StrategyConfigForm,
} from '@/lib/strategy-config/boundary';

interface StrategyConfigEditorProps {
  strategy: Strategy;
  onSave: (data: StrategyUpdateRequest) => void;
  isSaving?: boolean;
  settings?: Settings | null;
}

const marketTypes: MarketType[] = ['stock', 'crypto', 'polymarket'];
const denseSelectClassName =
  'flex h-9 w-full rounded-md border border-input bg-card px-3 py-1 text-sm text-foreground transition-colors focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring';

function resolveStrategyStatus(strategy: Strategy): StrategyStatus {
  if (strategy.status) {
    return strategy.status;
  }

  return strategy.is_active ? 'active' : 'inactive';
}

export function StrategyConfigEditor({
  strategy,
  onSave,
  isSaving,
  settings,
}: StrategyConfigEditorProps) {
  const [name, setName] = useState(strategy.name);
  const [description, setDescription] = useState(strategy.description ?? '');
  const [ticker, setTicker] = useState(strategy.ticker);
  const [marketType, setMarketType] = useState<MarketType>(strategy.market_type);
  const [scheduleCron, setScheduleCron] = useState(strategy.schedule_cron ?? '');
  const [isPaper, setIsPaper] = useState(strategy.is_paper);
  const [isActive, setIsActive] = useState(resolveStrategyStatus(strategy) === 'active');
  const [configForm, setConfigForm] = useState<StrategyConfigForm>(() =>
    strategyConfigBoundary.load(strategy.config),
  );
  const [showAdvanced, setShowAdvanced] = useState(false);
  const [fieldErrors, setFieldErrors] = useState<Record<string, string>>({});

  useEffect(() => {
    setName(strategy.name);
    setDescription(strategy.description ?? '');
    setTicker(strategy.ticker);
    setMarketType(strategy.market_type);
    setScheduleCron(strategy.schedule_cron ?? '');
    setIsPaper(strategy.is_paper);
    setIsActive(resolveStrategyStatus(strategy) === 'active');
    setConfigForm(strategyConfigBoundary.load(strategy.config));
    setFieldErrors({});
    setShowAdvanced(false);
  }, [strategy]);

  const providerOptions = useMemo(
    () => (settings?.llm?.providers ? Object.keys(settings.llm.providers) : []),
    [settings?.llm?.providers],
  );

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

  function toggleAnalyst(role: AgentRole, checked: boolean) {
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

  function handleSubmit(e: FormEvent) {
    e.preventDefault();

    const result = strategyConfigBoundary.submit(configForm);
    if (!result.ok) {
      setFieldErrors(result.fieldErrors);
      return;
    }

    setFieldErrors({});

    const currentStatus = resolveStrategyStatus(strategy);
    const nextStatus: StrategyStatus = isActive
      ? 'active'
      : currentStatus === 'paused'
        ? 'paused'
        : 'inactive';

    onSave({
      name,
      description: description || undefined,
      ticker: ticker.toUpperCase(),
      market_type: marketType,
      schedule_cron: scheduleCron || undefined,
      config: result.config,
      status: nextStatus,
      is_paper: isPaper,
      skip_next_run: strategy.skip_next_run,
    });
  }

  const firstError = Object.values(fieldErrors)[0];

  return (
    <Card data-testid="strategy-config-editor">
      <CardHeader>
        <CardTitle>Configuration</CardTitle>
        <CardDescription>Edit strategy settings</CardDescription>
      </CardHeader>
      <CardContent>
        <form onSubmit={handleSubmit} className="space-y-4">
          <div className="grid gap-4 sm:grid-cols-2">
            <div className="space-y-2 rounded-lg border border-border bg-background p-4">
              <Label htmlFor="edit-name">Name</Label>
              <Input
                id="edit-name"
                value={name}
                onChange={(e) => setName(e.target.value)}
                required
              />
            </div>
            <div className="space-y-2 rounded-lg border border-border bg-background p-4">
              <Label htmlFor="edit-ticker">Ticker</Label>
              <Input
                id="edit-ticker"
                value={ticker}
                onChange={(e) => setTicker(e.target.value)}
                required
              />
            </div>
          </div>

          <div className="space-y-2 rounded-lg border border-border bg-background p-4">
            <Label htmlFor="edit-description">Description</Label>
            <Input
              id="edit-description"
              value={description}
              onChange={(e) => setDescription(e.target.value)}
            />
          </div>

          <div className="grid gap-4 sm:grid-cols-2">
            <div className="space-y-2 rounded-lg border border-border bg-background p-4">
              <Label htmlFor="edit-market-type">Market type</Label>
              <select
                id="edit-market-type"
                value={marketType}
                onChange={(e) => setMarketType(e.target.value as MarketType)}
                className={denseSelectClassName}
              >
                {marketTypes.map((mt) => (
                  <option key={mt} value={mt}>
                    {mt}
                  </option>
                ))}
              </select>
            </div>
            <div className="space-y-2 rounded-lg border border-border bg-background p-4">
              <Label htmlFor="edit-schedule">Schedule (cron)</Label>
              <Input
                id="edit-schedule"
                value={scheduleCron}
                onChange={(e) => setScheduleCron(e.target.value)}
                placeholder="0 9 * * 1-5"
              />
            </div>
          </div>

          <div className="grid gap-3 sm:grid-cols-2">
            <label className="flex items-center gap-2 rounded-lg border border-border bg-background px-4 py-3 text-sm">
              <input
                type="checkbox"
                checked={isPaper}
                onChange={(e) => setIsPaper(e.target.checked)}
                className="rounded border-input"
              />
              Paper trading
            </label>
            <label className="flex items-center gap-2 rounded-lg border border-border bg-background px-4 py-3 text-sm">
              <input
                type="checkbox"
                checked={isActive}
                onChange={(e) => setIsActive(e.target.checked)}
                className="rounded border-input"
              />
              Active
            </label>
          </div>

          <div className="space-y-4 rounded-lg border border-border bg-background p-4">
            <h4 className="font-mono text-[11px] font-medium uppercase tracking-[0.16em] text-muted-foreground">
              LLM Configuration
            </h4>
            <div className="grid gap-4 sm:grid-cols-2">
              <div className="space-y-2">
                <Label htmlFor="llm-provider">Provider</Label>
                <select
                  id="llm-provider"
                  value={configForm.llm.provider}
                  onChange={(e) => setLlm({ provider: e.target.value as '' })}
                  className={denseSelectClassName}
                >
                  <option value="">Use global default</option>
                  {providerOptions.map((provider) => (
                    <option key={provider} value={provider}>
                      {provider}
                    </option>
                  ))}
                </select>
              </div>
              <div className="space-y-2">
                <Label htmlFor="deep-think-model">Deep Think Model</Label>
                <Input
                  id="deep-think-model"
                  value={configForm.llm.deepThinkModel}
                  onChange={(e) => setLlm({ deepThinkModel: e.target.value })}
                  placeholder={settings?.llm?.deep_think_model ?? 'Global default'}
                />
              </div>
            </div>
            <div className="space-y-2">
              <Label htmlFor="quick-think-model">Quick Think Model</Label>
              <Input
                id="quick-think-model"
                value={configForm.llm.quickThinkModel}
                onChange={(e) => setLlm({ quickThinkModel: e.target.value })}
                placeholder={settings?.llm?.quick_think_model ?? 'Global default'}
              />
            </div>
          </div>

          <div className="space-y-4 rounded-lg border border-border bg-background p-4">
            <h4 className="font-mono text-[11px] font-medium uppercase tracking-[0.16em] text-muted-foreground">
              Pipeline
            </h4>
            <div className="grid gap-4 sm:grid-cols-2">
              <div className="space-y-2">
                <Label htmlFor="debate-rounds">Debate Rounds</Label>
                <Input
                  id="debate-rounds"
                  type="number"
                  min={1}
                  max={10}
                  value={configForm.pipeline.debateRounds}
                  onChange={(e) => setPipeline({ debateRounds: e.target.value })}
                />
              </div>
              <div className="space-y-2">
                <Label htmlFor="phase-timeout">Analysis Timeout (seconds)</Label>
                <Input
                  id="phase-timeout"
                  type="number"
                  min={1}
                  value={configForm.pipeline.analysisTimeoutSeconds}
                  onChange={(e) => setPipeline({ analysisTimeoutSeconds: e.target.value })}
                  placeholder="120"
                />
              </div>
            </div>
            <div className="space-y-2">
              <Label htmlFor="pipeline-timeout">Debate Timeout (seconds)</Label>
              <Input
                id="pipeline-timeout"
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
                <Label htmlFor="max-position-size-pct">Max Position Size %</Label>
                <Input
                  id="max-position-size-pct"
                  type="number"
                  step="0.01"
                  min={0.01}
                  max={1}
                  value={configForm.risk.positionSizePct}
                  onChange={(e) => setRisk({ positionSizePct: e.target.value })}
                />
              </div>
              <div className="space-y-2">
                <Label htmlFor="stop-loss-atr-multiplier">Stop Loss ATR Multiplier</Label>
                <Input
                  id="stop-loss-atr-multiplier"
                  type="number"
                  step="0.1"
                  value={configForm.risk.stopLossMultiplier}
                  onChange={(e) => setRisk({ stopLossMultiplier: e.target.value })}
                />
              </div>
            </div>
            <div className="grid gap-4 sm:grid-cols-2">
              <div className="space-y-2">
                <Label htmlFor="take-profit-atr-multiplier">Take Profit ATR Multiplier</Label>
                <Input
                  id="take-profit-atr-multiplier"
                  type="number"
                  step="0.1"
                  value={configForm.risk.takeProfitMultiplier}
                  onChange={(e) => setRisk({ takeProfitMultiplier: e.target.value })}
                />
              </div>
              <div className="space-y-2">
                <Label htmlFor="min-confidence-threshold">Min Confidence Threshold</Label>
                <Input
                  id="min-confidence-threshold"
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
              <div className="space-y-2">
                <Label htmlFor="prompt-overrides">Prompt Overrides (JSON)</Label>
                <Textarea
                  id="prompt-overrides"
                  value={configForm.promptOverridesJson}
                  onChange={(e) => {
                    setConfigForm((prev) => ({
                      ...prev,
                      promptOverridesJson: e.target.value,
                    }));
                    setFieldErrors({});
                  }}
                  rows={6}
                  className="font-mono text-xs"
                />
              </div>
            ) : null}
          </div>

          {firstError ? (
            <p className="rounded-md border border-destructive/20 bg-destructive/10 px-3 py-2 text-xs text-destructive">
              {firstError}
            </p>
          ) : null}

          <div className="flex justify-end">
            <Button type="submit" size="dense" disabled={isSaving || !name || !ticker}>
              {isSaving ? 'Saving…' : 'Save changes'}
            </Button>
          </div>
        </form>
      </CardContent>
    </Card>
  );
}
