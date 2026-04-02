import type { AgentRole } from '@/lib/api/types'

export const analystOptions: ReadonlyArray<{ role: AgentRole; label: string }> = [
  { role: 'market_analyst', label: 'Market Analyst' },
  { role: 'fundamentals_analyst', label: 'Fundamentals Analyst' },
  { role: 'news_analyst', label: 'News Analyst' },
  { role: 'social_media_analyst', label: 'Social Media Analyst' },
]

export const defaultAnalysts = analystOptions.map(({ role }) => role)

export interface StructuredStrategyConfigValues {
  researchDebateRounds: string
  riskDebateRounds: string
  phaseTimeout: string
  pipelineTimeout: string
  maxPositionSizePct: string
  stopLossAtrMultiplier: string
  takeProfitAtrMultiplier: string
  minConfidenceThreshold: string
  selectedAnalysts: AgentRole[]
  promptOverrides: string
}

function asRecord(value: unknown): Record<string, unknown> {
  if (value != null && typeof value === 'object' && !Array.isArray(value)) {
    return { ...(value as Record<string, unknown>) }
  }

  return {}
}

function requireNumberInRange(value: string, min: number, max: number, message: string) {
  const parsed = Number(value)
  if (!Number.isFinite(parsed) || parsed < min || parsed > max) {
    return { error: message }
  }

  return { value: parsed }
}

function requirePositiveNumber(value: string, message: string) {
  const parsed = Number(value)
  if (!Number.isFinite(parsed) || parsed <= 0) {
    return { error: message }
  }

  return { value: parsed }
}

function parsePromptOverrides(value: string) {
  const trimmed = value.trim()
  if (!trimmed) {
    return { value: {} as Record<string, string> }
  }

  try {
    const parsed = JSON.parse(trimmed) as unknown
    if (parsed == null || typeof parsed !== 'object' || Array.isArray(parsed)) {
      return { error: 'Prompt overrides must be a JSON object' }
    }

    const entries = Object.entries(parsed as Record<string, unknown>)
    if (entries.some(([, prompt]) => typeof prompt !== 'string')) {
      return { error: 'Prompt overrides must map roles to strings' }
    }

    return { value: Object.fromEntries(entries) as Record<string, string> }
  } catch {
    return { error: 'Prompt overrides must be valid JSON' }
  }
}

export function parseStructuredStrategyConfig(config: unknown): StructuredStrategyConfigValues {
  const cfg = asRecord(config)
  const pipeline = asRecord(cfg.pipeline_config)
  const risk = asRecord(cfg.risk_config)
  const analysts = Array.isArray(cfg.analyst_selection) ? (cfg.analyst_selection as AgentRole[]) : defaultAnalysts
  const promptOverrides = asRecord(cfg.prompt_overrides)

  return {
    researchDebateRounds: pipeline.debate_rounds == null ? '' : String(pipeline.debate_rounds),
    riskDebateRounds: '',
    phaseTimeout: pipeline.analysis_timeout_seconds == null ? '' : String(pipeline.analysis_timeout_seconds),
    pipelineTimeout: pipeline.debate_timeout_seconds == null ? '' : String(pipeline.debate_timeout_seconds),
    maxPositionSizePct: risk.position_size_pct == null ? '' : String((risk.position_size_pct as number) / 100),
    stopLossAtrMultiplier: risk.stop_loss_multiplier == null ? '' : String(risk.stop_loss_multiplier),
    takeProfitAtrMultiplier: risk.take_profit_multiplier == null ? '' : String(risk.take_profit_multiplier),
    minConfidenceThreshold: risk.min_confidence == null ? '' : String(risk.min_confidence),
    selectedAnalysts: analysts.length > 0 ? analysts : defaultAnalysts,
    promptOverrides: JSON.stringify(promptOverrides as Record<string, string>, null, 2),
  }
}

export function buildStructuredStrategyConfig(
  values: StructuredStrategyConfigValues,
  baseConfig: unknown = {},
): { config?: Record<string, unknown>; error?: string } {
  if (values.selectedAnalysts.length === 0) {
    return { error: 'Select at least one analyst' }
  }

  const promptOverridesResult = parsePromptOverrides(values.promptOverrides)
  if ('error' in promptOverridesResult) {
    return promptOverridesResult
  }

  const config = asRecord(baseConfig)
  const pipelineConfig = asRecord(config.pipeline_config)
  const riskConfig = asRecord(config.risk_config)

  const debateRoundsSource = values.researchDebateRounds.trim() || values.riskDebateRounds.trim()
  if (debateRoundsSource) {
    const result = requireNumberInRange(debateRoundsSource, 1, 10, 'Debate rounds must be between 1 and 10')
    if ('error' in result) return result
    pipelineConfig.debate_rounds = result.value
  } else {
    delete pipelineConfig.debate_rounds
  }

  if (values.phaseTimeout.trim()) {
    const result = requirePositiveNumber(values.phaseTimeout, 'Analysis timeout must be at least 1 second')
    if ('error' in result) return result
    pipelineConfig.analysis_timeout_seconds = result.value
  } else {
    delete pipelineConfig.analysis_timeout_seconds
  }

  if (values.pipelineTimeout.trim()) {
    const result = requirePositiveNumber(values.pipelineTimeout, 'Debate timeout must be at least 1 second')
    if ('error' in result) return result
    pipelineConfig.debate_timeout_seconds = result.value
  } else {
    delete pipelineConfig.debate_timeout_seconds
  }

  if (Object.keys(pipelineConfig).length > 0) {
    config.pipeline_config = pipelineConfig
  } else {
    delete config.pipeline_config
  }

  if (values.maxPositionSizePct.trim()) {
    const result = requireNumberInRange(values.maxPositionSizePct, 0.01, 1, 'Max position size % must be between 0.01 and 1.00')
    if ('error' in result) return result
    riskConfig.position_size_pct = result.value * 100
  } else {
    delete riskConfig.position_size_pct
  }

  if (values.stopLossAtrMultiplier.trim()) {
    const result = requirePositiveNumber(values.stopLossAtrMultiplier, 'Stop loss ATR multiplier must be greater than 0')
    if ('error' in result) return result
    riskConfig.stop_loss_multiplier = result.value
  } else {
    delete riskConfig.stop_loss_multiplier
  }

  if (values.takeProfitAtrMultiplier.trim()) {
    const result = requirePositiveNumber(values.takeProfitAtrMultiplier, 'Take profit ATR multiplier must be greater than 0')
    if ('error' in result) return result
    riskConfig.take_profit_multiplier = result.value
  } else {
    delete riskConfig.take_profit_multiplier
  }

  if (values.minConfidenceThreshold.trim()) {
    const result = requireNumberInRange(values.minConfidenceThreshold, 0, 1, 'Min confidence threshold must be between 0.00 and 1.00')
    if ('error' in result) return result
    riskConfig.min_confidence = result.value
  } else {
    delete riskConfig.min_confidence
  }

  if (Object.keys(riskConfig).length > 0) {
    config.risk_config = riskConfig
  } else {
    delete config.risk_config
  }

  config.analyst_selection = values.selectedAnalysts

  if (Object.keys(promptOverridesResult.value).length > 0) {
    config.prompt_overrides = promptOverridesResult.value
  } else {
    delete config.prompt_overrides
  }

  return { config }
}
