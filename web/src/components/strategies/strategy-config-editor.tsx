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
  analystOptions,
  buildStructuredStrategyConfig,
  parseStructuredStrategyConfig,
} from './strategy-structured-config';

interface StrategyConfigEditorProps {
  strategy: Strategy;
  onSave: (data: StrategyUpdateRequest) => void;
  isSaving?: boolean;
  settings?: Settings | null;
}

const marketTypes: MarketType[] = ['stock', 'crypto', 'polymarket'];
const denseSelectClassName =
  'flex h-9 w-full rounded-md border border-input bg-card/70 px-3 py-1 text-sm text-foreground shadow-[inset_0_1px_0_rgba(255,255,255,0.02)] transition-colors focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring';

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
  const [configError, setConfigError] = useState<string | null>(null);
  const [deepThinkProvider, setDeepThinkProvider] = useState('');
  const [deepThinkModel, setDeepThinkModel] = useState('');
  const [quickThinkProvider, setQuickThinkProvider] = useState('');
  const [quickThinkModel, setQuickThinkModel] = useState('');
  const [researchDebateRounds, setResearchDebateRounds] = useState('');
  const [riskDebateRounds, setRiskDebateRounds] = useState('');
  const [phaseTimeout, setPhaseTimeout] = useState('');
  const [pipelineTimeout, setPipelineTimeout] = useState('');
  const [maxPositionSizePct, setMaxPositionSizePct] = useState('');
  const [stopLossAtrMultiplier, setStopLossAtrMultiplier] = useState('');
  const [takeProfitAtrMultiplier, setTakeProfitAtrMultiplier] = useState('');
  const [minConfidenceThreshold, setMinConfidenceThreshold] = useState('');
  const [selectedAnalysts, setSelectedAnalysts] = useState<AgentRole[]>([]);
  const [showAdvanced, setShowAdvanced] = useState(false);
  const [promptOverrides, setPromptOverrides] = useState('{}');

  useEffect(() => {
    const cfg = ((strategy.config ?? {}) as Record<string, unknown>) ?? {};
    const llm = ((cfg.llm_config ?? {}) as Record<string, unknown>) ?? {};
    const structuredConfig = parseStructuredStrategyConfig(strategy.config);

    setName(strategy.name);
    setDescription(strategy.description ?? '');
    setTicker(strategy.ticker);
    setMarketType(strategy.market_type);
    setScheduleCron(strategy.schedule_cron ?? '');
    setIsPaper(strategy.is_paper);
    setIsActive(resolveStrategyStatus(strategy) === 'active');
    setConfigError(null);

    setDeepThinkProvider((llm.provider as string) ?? '');
    setDeepThinkModel((llm.deep_think_model as string) ?? '');
    setQuickThinkProvider('');
    setQuickThinkModel((llm.quick_think_model as string) ?? '');

    setResearchDebateRounds(structuredConfig.researchDebateRounds);
    setRiskDebateRounds(structuredConfig.riskDebateRounds);
    setPhaseTimeout(structuredConfig.phaseTimeout);
    setPipelineTimeout(structuredConfig.pipelineTimeout);
    setMaxPositionSizePct(structuredConfig.maxPositionSizePct);
    setStopLossAtrMultiplier(structuredConfig.stopLossAtrMultiplier);
    setTakeProfitAtrMultiplier(structuredConfig.takeProfitAtrMultiplier);
    setMinConfidenceThreshold(structuredConfig.minConfidenceThreshold);
    setSelectedAnalysts(structuredConfig.selectedAnalysts);
    setPromptOverrides(structuredConfig.promptOverrides);
    setShowAdvanced(false);
  }, [strategy]);

  const providerOptions = useMemo(
    () => (settings?.llm?.providers ? Object.keys(settings.llm.providers) : []),
    [settings?.llm?.providers],
  );

  function toggleAnalyst(analyst: AgentRole, checked: boolean) {
    setSelectedAnalysts((prev) => {
      if (checked) {
        return prev.includes(analyst) ? prev : [...prev, analyst];
      }
      return prev.filter((value) => value !== analyst);
    });
    setConfigError(null);
  }

  function handleSubmit(e: FormEvent) {
    e.preventDefault();

    const structuredConfig = buildStructuredStrategyConfig(
      {
        researchDebateRounds,
        riskDebateRounds,
        phaseTimeout,
        pipelineTimeout,
        maxPositionSizePct,
        stopLossAtrMultiplier,
        takeProfitAtrMultiplier,
        minConfidenceThreshold,
        selectedAnalysts,
        promptOverrides,
      },
      strategy.config,
    );

    if (structuredConfig.error || !structuredConfig.config) {
      setConfigError(structuredConfig.error ?? 'Invalid configuration');
      return;
    }

    const config = structuredConfig.config;
    const llmConfig = ((config.llm_config ?? {}) as Record<string, unknown>) ?? {};

    if (deepThinkProvider) {
      llmConfig.provider = deepThinkProvider;
    } else {
      delete llmConfig.provider;
    }
    if (deepThinkModel) {
      llmConfig.deep_think_model = deepThinkModel;
    } else {
      delete llmConfig.deep_think_model;
    }
    if (quickThinkModel) {
      llmConfig.quick_think_model = quickThinkModel;
    } else {
      delete llmConfig.quick_think_model;
    }
    if (Object.keys(llmConfig).length > 0) {
      config.llm_config = llmConfig;
    } else {
      delete config.llm_config;
    }

    setConfigError(null);

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
      config,
      status: nextStatus,
      is_paper: isPaper,
      skip_next_run: strategy.skip_next_run,
    });
  }

  return (
    <Card data-testid="strategy-config-editor">
      <CardHeader>
        <CardTitle>Configuration</CardTitle>
        <CardDescription>Edit strategy settings</CardDescription>
      </CardHeader>
      <CardContent>
        <form onSubmit={handleSubmit} className="space-y-4">
          <div className="grid gap-4 sm:grid-cols-2">
            <div className="space-y-2 rounded-lg border border-white/8 bg-background/45 p-4">
              <Label htmlFor="edit-name">Name</Label>
              <Input
                id="edit-name"
                value={name}
                onChange={(e) => setName(e.target.value)}
                required
              />
            </div>
            <div className="space-y-2 rounded-lg border border-white/8 bg-background/45 p-4">
              <Label htmlFor="edit-ticker">Ticker</Label>
              <Input
                id="edit-ticker"
                value={ticker}
                onChange={(e) => setTicker(e.target.value)}
                required
              />
            </div>
          </div>

          <div className="space-y-2 rounded-lg border border-white/8 bg-background/45 p-4">
            <Label htmlFor="edit-description">Description</Label>
            <Input
              id="edit-description"
              value={description}
              onChange={(e) => setDescription(e.target.value)}
            />
          </div>

          <div className="grid gap-4 sm:grid-cols-2">
            <div className="space-y-2 rounded-lg border border-white/8 bg-background/45 p-4">
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
            <div className="space-y-2 rounded-lg border border-white/8 bg-background/45 p-4">
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
            <label className="flex items-center gap-2 rounded-lg border border-white/8 bg-background/45 px-4 py-3 text-sm">
              <input
                type="checkbox"
                checked={isPaper}
                onChange={(e) => setIsPaper(e.target.checked)}
                className="rounded border-input"
              />
              Paper trading
            </label>
            <label className="flex items-center gap-2 rounded-lg border border-white/8 bg-background/45 px-4 py-3 text-sm">
              <input
                type="checkbox"
                checked={isActive}
                onChange={(e) => setIsActive(e.target.checked)}
                className="rounded border-input"
              />
              Active
            </label>
          </div>

          <div className="space-y-4 rounded-lg border border-white/8 bg-background/45 p-4">
            <h4 className="font-mono text-[11px] font-medium uppercase tracking-[0.16em] text-muted-foreground">
              LLM Configuration
            </h4>
            <div className="grid gap-4 sm:grid-cols-2">
              <div className="space-y-2">
                <Label htmlFor="deep-think-provider">Deep Think Provider</Label>
                <select
                  id="deep-think-provider"
                  value={deepThinkProvider}
                  onChange={(e) => {
                    setDeepThinkProvider(e.target.value);
                    setConfigError(null);
                  }}
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
                  value={deepThinkModel}
                  onChange={(e) => {
                    setDeepThinkModel(e.target.value);
                    setConfigError(null);
                  }}
                  placeholder={settings?.llm?.deep_think_model ?? 'Global default'}
                />
              </div>
            </div>
            <div className="grid gap-4 sm:grid-cols-2">
              <div className="space-y-2">
                <Label htmlFor="quick-think-provider">Quick Think Provider</Label>
                <select
                  id="quick-think-provider"
                  value={quickThinkProvider}
                  onChange={(e) => {
                    setQuickThinkProvider(e.target.value);
                    setConfigError(null);
                  }}
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
                <Label htmlFor="quick-think-model">Quick Think Model</Label>
                <Input
                  id="quick-think-model"
                  value={quickThinkModel}
                  onChange={(e) => {
                    setQuickThinkModel(e.target.value);
                    setConfigError(null);
                  }}
                  placeholder={settings?.llm?.quick_think_model ?? 'Global default'}
                />
              </div>
            </div>
          </div>

          <div className="space-y-4 rounded-lg border border-white/8 bg-background/45 p-4">
            <h4 className="font-mono text-[11px] font-medium uppercase tracking-[0.16em] text-muted-foreground">
              Pipeline
            </h4>
            <div className="grid gap-4 sm:grid-cols-2">
              <div className="space-y-2">
                <Label htmlFor="research-debate-rounds">Research Debate Rounds</Label>
                <Input
                  id="research-debate-rounds"
                  type="number"
                  min={1}
                  max={10}
                  value={researchDebateRounds}
                  onChange={(e) => {
                    setResearchDebateRounds(e.target.value);
                    setConfigError(null);
                  }}
                />
              </div>
              <div className="space-y-2">
                <Label htmlFor="risk-debate-rounds">Risk Debate Rounds</Label>
                <Input
                  id="risk-debate-rounds"
                  type="number"
                  min={1}
                  max={10}
                  value={riskDebateRounds}
                  onChange={(e) => {
                    setRiskDebateRounds(e.target.value);
                    setConfigError(null);
                  }}
                />
              </div>
            </div>
            <div className="grid gap-4 sm:grid-cols-2">
              <div className="space-y-2">
                <Label htmlFor="phase-timeout">Analysis Timeout (seconds)</Label>
                <Input
                  id="phase-timeout"
                  type="number"
                  min={1}
                  value={phaseTimeout}
                  onChange={(e) => {
                    setPhaseTimeout(e.target.value);
                    setConfigError(null);
                  }}
                  placeholder="120"
                />
              </div>
              <div className="space-y-2">
                <Label htmlFor="pipeline-timeout">Debate Timeout (seconds)</Label>
                <Input
                  id="pipeline-timeout"
                  type="number"
                  min={1}
                  value={pipelineTimeout}
                  onChange={(e) => {
                    setPipelineTimeout(e.target.value);
                    setConfigError(null);
                  }}
                  placeholder="600"
                />
              </div>
            </div>
          </div>

          <div className="space-y-4 rounded-lg border border-white/8 bg-background/45 p-4">
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
                  value={maxPositionSizePct}
                  onChange={(e) => {
                    setMaxPositionSizePct(e.target.value);
                    setConfigError(null);
                  }}
                />
              </div>
              <div className="space-y-2">
                <Label htmlFor="stop-loss-atr-multiplier">Stop Loss ATR Multiplier</Label>
                <Input
                  id="stop-loss-atr-multiplier"
                  type="number"
                  step="0.1"
                  value={stopLossAtrMultiplier}
                  onChange={(e) => {
                    setStopLossAtrMultiplier(e.target.value);
                    setConfigError(null);
                  }}
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
                  value={takeProfitAtrMultiplier}
                  onChange={(e) => {
                    setTakeProfitAtrMultiplier(e.target.value);
                    setConfigError(null);
                  }}
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
                  value={minConfidenceThreshold}
                  onChange={(e) => {
                    setMinConfidenceThreshold(e.target.value);
                    setConfigError(null);
                  }}
                />
              </div>
            </div>
          </div>

          <div className="space-y-4 rounded-lg border border-white/8 bg-background/45 p-4">
            <h4 className="font-mono text-[11px] font-medium uppercase tracking-[0.16em] text-muted-foreground">
              Analysts
            </h4>
            <div className="grid gap-3 sm:grid-cols-2">
              {analystOptions.map(({ role, label }) => (
                <label
                  key={role}
                  className="flex items-center gap-2 rounded-md border border-white/8 bg-background/55 px-3 py-2 text-sm"
                >
                  <input
                    type="checkbox"
                    checked={selectedAnalysts.includes(role)}
                    onChange={(e) => toggleAnalyst(role, e.target.checked)}
                    className="rounded border-input"
                  />
                  {label}
                </label>
              ))}
            </div>
          </div>

          <div className="space-y-3 rounded-lg border border-white/8 bg-background/45 p-4">
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
                  value={promptOverrides}
                  onChange={(e) => {
                    setPromptOverrides(e.target.value);
                    setConfigError(null);
                  }}
                  rows={6}
                  className="font-mono text-xs"
                />
              </div>
            ) : null}
          </div>

          {configError ? (
            <p className="rounded-md border border-destructive/20 bg-destructive/10 px-3 py-2 text-xs text-destructive">
              {configError}
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
