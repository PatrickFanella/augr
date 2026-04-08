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
  analystOptions,
  buildStructuredStrategyConfig,
  defaultAnalysts,
} from './strategy-structured-config';

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
  const [researchDebateRounds, setResearchDebateRounds] = useState('');
  const [riskDebateRounds, setRiskDebateRounds] = useState('');
  const [phaseTimeout, setPhaseTimeout] = useState('');
  const [pipelineTimeout, setPipelineTimeout] = useState('');
  const [maxPositionSizePct, setMaxPositionSizePct] = useState('');
  const [stopLossAtrMultiplier, setStopLossAtrMultiplier] = useState('');
  const [takeProfitAtrMultiplier, setTakeProfitAtrMultiplier] = useState('');
  const [minConfidenceThreshold, setMinConfidenceThreshold] = useState('');
  const [selectedAnalysts, setSelectedAnalysts] = useState(defaultAnalysts);
  const [showAdvanced, setShowAdvanced] = useState(false);
  const [promptOverrides, setPromptOverrides] = useState('{}');
  const [configError, setConfigError] = useState<string | null>(null);

  function resetForm() {
    setName('');
    setDescription('');
    setTicker('');
    setMarketType('stock');
    setScheduleCron('');
    setIsPaper(true);
    setIsActive(false);
    setResearchDebateRounds('');
    setRiskDebateRounds('');
    setPhaseTimeout('');
    setPipelineTimeout('');
    setMaxPositionSizePct('');
    setStopLossAtrMultiplier('');
    setTakeProfitAtrMultiplier('');
    setMinConfidenceThreshold('');
    setSelectedAnalysts(defaultAnalysts);
    setShowAdvanced(false);
    setPromptOverrides('{}');
    setConfigError(null);
  }

  function handleOpenChange(nextOpen: boolean) {
    if (!nextOpen) {
      resetForm();
    }
    onOpenChange(nextOpen);
  }

  function toggleAnalyst(analyst: (typeof defaultAnalysts)[number], checked: boolean) {
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

    const result = buildStructuredStrategyConfig({
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
    });

    if (result.error || !result.config) {
      setConfigError(result.error ?? 'Invalid configuration');
      return;
    }

    setConfigError(null);
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
                  Enter the Polymarket market slug (e.g. will-trump-win-2024). The price shown is the YES token probability (0–1).
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
                <p className="text-xs text-muted-foreground mt-1">
                  {describeCron(scheduleCron)}
                </p>
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
                <Label htmlFor="create-research-debate-rounds">Research Debate Rounds</Label>
                <Input
                  id="create-research-debate-rounds"
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
                <Label htmlFor="create-risk-debate-rounds">Risk Debate Rounds</Label>
                <Input
                  id="create-risk-debate-rounds"
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
                <Label htmlFor="create-phase-timeout">Analysis Timeout (seconds)</Label>
                <Input
                  id="create-phase-timeout"
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
                <Label htmlFor="create-pipeline-timeout">Debate Timeout (seconds)</Label>
                <Input
                  id="create-pipeline-timeout"
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
                  value={maxPositionSizePct}
                  onChange={(e) => {
                    setMaxPositionSizePct(e.target.value);
                    setConfigError(null);
                  }}
                />
              </div>
              <div className="space-y-2">
                <Label htmlFor="create-stop-loss-atr-multiplier">Stop Loss ATR Multiplier</Label>
                <Input
                  id="create-stop-loss-atr-multiplier"
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
                <Label htmlFor="create-take-profit-atr-multiplier">
                  Take Profit ATR Multiplier
                </Label>
                <Input
                  id="create-take-profit-atr-multiplier"
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
                <Label htmlFor="create-min-confidence-threshold">Min Confidence Threshold</Label>
                <Input
                  id="create-min-confidence-threshold"
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

          <div className="space-y-4 rounded-lg border border-border bg-background p-4">
            <h4 className="font-mono text-[11px] font-medium uppercase tracking-[0.16em] text-muted-foreground">
              Analysts
            </h4>
            <div className="grid gap-3 sm:grid-cols-2">
              {analystOptions.map(({ role, label }) => (
                <label
                  key={role}
                  className="flex items-center gap-2 rounded-md border border-border bg-background px-3 py-2 text-sm"
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
                <Label htmlFor="create-prompt-overrides">Prompt Overrides (JSON)</Label>
                <Textarea
                  id="create-prompt-overrides"
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
            <p
              className="rounded-md border border-destructive/20 bg-destructive/10 px-3 py-2 text-xs text-destructive"
              data-testid="config-error"
            >
              {configError}
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
