import type { AgentRole, StrategyConfigWire, StrategyLLMProvider } from '@/lib/api/types'

export type { StrategyConfigWire, StrategyLLMProvider }

export interface StrategyConfigForm {
  llm: {
    provider: '' | StrategyLLMProvider
    deepThinkModel: string
    quickThinkModel: string
  }
  pipeline: {
    debateRounds: string
    analysisTimeoutSeconds: string
    debateTimeoutSeconds: string
  }
  risk: {
    positionSizePct: string
    stopLossMultiplier: string
    takeProfitMultiplier: string
    minConfidence: string
  }
  analysts: {
    mode: 'default' | 'custom'
    selected: AgentRole[]
  }
  promptOverridesJson: string
}

export type StrategyConfigSubmitResult =
  | { ok: true; config?: StrategyConfigWire }
  | { ok: false; fieldErrors: Record<string, string> }

export const defaultAnalysts: AgentRole[] = [
  'market_analyst',
  'fundamentals_analyst',
  'news_analyst',
  'social_media_analyst',
]

export const llmProviderOptions: StrategyLLMProvider[] = [
  'openai',
  'anthropic',
  'google',
  'openrouter',
  'xai',
  'ollama',
]

function isDefaultSelection(selected: AgentRole[]): boolean {
  return (
    selected.length === defaultAnalysts.length &&
    defaultAnalysts.every((r) => selected.includes(r))
  )
}

function emptyForm(): StrategyConfigForm {
  return {
    llm: { provider: '', deepThinkModel: '', quickThinkModel: '' },
    pipeline: { debateRounds: '', analysisTimeoutSeconds: '', debateTimeoutSeconds: '' },
    risk: { positionSizePct: '', stopLossMultiplier: '', takeProfitMultiplier: '', minConfidence: '' },
    analysts: { mode: 'default', selected: [...defaultAnalysts] },
    promptOverridesJson: '',
  }
}

export const strategyConfigBoundary = {
  load(raw: StrategyConfigWire | null | undefined): StrategyConfigForm {
    if (!raw) return emptyForm()

    const llm = raw.llm_config ?? {}
    const pipeline = raw.pipeline_config ?? {}
    const risk = raw.risk_config ?? {}
    const rawAnalysts = raw.analyst_selection
    const selected = rawAnalysts && rawAnalysts.length > 0 ? [...rawAnalysts] : [...defaultAnalysts]

    return {
      llm: {
        provider: (llm.provider ?? '') as '' | StrategyLLMProvider,
        deepThinkModel: llm.deep_think_model ?? '',
        quickThinkModel: llm.quick_think_model ?? '',
      },
      pipeline: {
        debateRounds: pipeline.debate_rounds != null ? String(pipeline.debate_rounds) : '',
        analysisTimeoutSeconds:
          pipeline.analysis_timeout_seconds != null
            ? String(pipeline.analysis_timeout_seconds)
            : '',
        debateTimeoutSeconds:
          pipeline.debate_timeout_seconds != null ? String(pipeline.debate_timeout_seconds) : '',
      },
      risk: {
        // wire stores position_size_pct as e.g. 5 (meaning 5%), form shows 0.05
        positionSizePct:
          risk.position_size_pct != null ? String(risk.position_size_pct / 100) : '',
        stopLossMultiplier:
          risk.stop_loss_multiplier != null ? String(risk.stop_loss_multiplier) : '',
        takeProfitMultiplier:
          risk.take_profit_multiplier != null ? String(risk.take_profit_multiplier) : '',
        minConfidence: risk.min_confidence != null ? String(risk.min_confidence) : '',
      },
      analysts: {
        mode: isDefaultSelection(selected) ? 'default' : 'custom',
        selected,
      },
      promptOverridesJson:
        raw.prompt_overrides && Object.keys(raw.prompt_overrides).length > 0
          ? JSON.stringify(raw.prompt_overrides, null, 2)
          : '',
    }
  },

  submit(form: StrategyConfigForm): StrategyConfigSubmitResult {
    const fieldErrors: Record<string, string> = {}
    const config: StrategyConfigWire = {}

    // LLM config
    if (form.llm.provider || form.llm.deepThinkModel || form.llm.quickThinkModel) {
      const llm: NonNullable<StrategyConfigWire['llm_config']> = {}
      if (form.llm.provider) llm.provider = form.llm.provider as StrategyLLMProvider
      if (form.llm.deepThinkModel) llm.deep_think_model = form.llm.deepThinkModel
      if (form.llm.quickThinkModel) llm.quick_think_model = form.llm.quickThinkModel
      config.llm_config = llm
    }

    // Pipeline config
    const pipeline: NonNullable<StrategyConfigWire['pipeline_config']> = {}
    if (form.pipeline.debateRounds.trim()) {
      const v = Number(form.pipeline.debateRounds)
      if (!Number.isFinite(v) || v < 1 || v > 10) {
        fieldErrors['pipeline.debateRounds'] = 'Debate rounds must be between 1 and 10'
      } else {
        pipeline.debate_rounds = v
      }
    }
    if (form.pipeline.analysisTimeoutSeconds.trim()) {
      const v = Number(form.pipeline.analysisTimeoutSeconds)
      if (!Number.isFinite(v) || v <= 0) {
        fieldErrors['pipeline.analysisTimeoutSeconds'] = 'Analysis timeout must be greater than 0'
      } else {
        pipeline.analysis_timeout_seconds = v
      }
    }
    if (form.pipeline.debateTimeoutSeconds.trim()) {
      const v = Number(form.pipeline.debateTimeoutSeconds)
      if (!Number.isFinite(v) || v <= 0) {
        fieldErrors['pipeline.debateTimeoutSeconds'] = 'Debate timeout must be greater than 0'
      } else {
        pipeline.debate_timeout_seconds = v
      }
    }
    if (Object.keys(pipeline).length > 0) config.pipeline_config = pipeline

    // Risk config
    const risk: NonNullable<StrategyConfigWire['risk_config']> = {}
    if (form.risk.positionSizePct.trim()) {
      const v = Number(form.risk.positionSizePct)
      if (!Number.isFinite(v) || v < 0.01 || v > 1) {
        fieldErrors['risk.positionSizePct'] = 'Max position size % must be between 0.01 and 1.00'
      } else {
        risk.position_size_pct = v * 100
      }
    }
    if (form.risk.stopLossMultiplier.trim()) {
      const v = Number(form.risk.stopLossMultiplier)
      if (!Number.isFinite(v) || v <= 0) {
        fieldErrors['risk.stopLossMultiplier'] = 'Stop loss ATR multiplier must be greater than 0'
      } else {
        risk.stop_loss_multiplier = v
      }
    }
    if (form.risk.takeProfitMultiplier.trim()) {
      const v = Number(form.risk.takeProfitMultiplier)
      if (!Number.isFinite(v) || v <= 0) {
        fieldErrors['risk.takeProfitMultiplier'] =
          'Take profit ATR multiplier must be greater than 0'
      } else {
        risk.take_profit_multiplier = v
      }
    }
    if (form.risk.minConfidence.trim()) {
      const v = Number(form.risk.minConfidence)
      if (!Number.isFinite(v) || v < 0 || v > 1) {
        fieldErrors['risk.minConfidence'] = 'Min confidence threshold must be between 0 and 1'
      } else {
        risk.min_confidence = v
      }
    }
    if (Object.keys(risk).length > 0) config.risk_config = risk

    // Analysts
    const selected = form.analysts.mode === 'default' ? defaultAnalysts : form.analysts.selected
    if (selected.length === 0) {
      fieldErrors['analysts.selected'] = 'Select at least one analyst'
    } else {
      config.analyst_selection = selected
    }

    // Prompt overrides
    const json = form.promptOverridesJson.trim()
    if (json) {
      try {
        const parsed = JSON.parse(json) as unknown
        if (parsed == null || typeof parsed !== 'object' || Array.isArray(parsed)) {
          fieldErrors['promptOverridesJson'] = 'Prompt overrides must be a JSON object'
        } else {
          const entries = Object.entries(parsed as Record<string, unknown>)
          if (entries.some(([, v]) => typeof v !== 'string')) {
            fieldErrors['promptOverridesJson'] = 'Prompt overrides must map roles to strings'
          } else {
            config.prompt_overrides = Object.fromEntries(entries) as Record<string, string>
          }
        }
      } catch {
        fieldErrors['promptOverridesJson'] = 'Prompt overrides must be valid JSON'
      }
    }

    if (Object.keys(fieldErrors).length > 0) return { ok: false, fieldErrors }
    return { ok: true, config: Object.keys(config).length > 0 ? config : undefined }
  },
}
