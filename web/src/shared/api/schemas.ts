import { z } from 'zod'

import {
  forwardCompatibleEnumSchema,
  isoDateSchema,
  optionalNullable,
  rawJsonSchema,
  uuidSchema,
} from '@/shared/api/contract'

export const apiErrorSchema = z
  .object({
    error: z.string(),
    code: z.string().min(1),
    details: rawJsonSchema.optional(),
  })
  .passthrough()

export function listResponseSchema<T extends z.ZodType>(itemSchema: T) {
  return z
    .object({
      data: z.array(itemSchema),
      total: z.number().int().nonnegative().optional(),
      limit: z.number().int().nonnegative(),
      offset: z.number().int().nonnegative(),
    })
    .passthrough()
}

export const authResponseSchema = z
  .object({
    access_token: z.string().min(1),
    refresh_token: z.string().min(1),
    expires_at: isoDateSchema,
  })
  .passthrough()

export const userSchema = z
  .object({
    id: uuidSchema,
    username: z.string().min(1),
    created_at: isoDateSchema,
    updated_at: isoDateSchema,
  })
  .passthrough()

export const strategyLatestRunSummarySchema = z
  .object({
    id: uuidSchema,
    strategy_id: uuidSchema,
    ticker: z.string().min(1),
    status: forwardCompatibleEnumSchema,
    signal: forwardCompatibleEnumSchema.optional(),
    started_at: isoDateSchema,
    completed_at: optionalNullable(isoDateSchema).optional(),
  })
  .passthrough()

export const strategySchema = z
  .object({
    id: uuidSchema,
    name: z.string().min(1),
    description: z.string().optional(),
    ticker: z.string().min(1),
    market_type: forwardCompatibleEnumSchema,
    schedule_cron: z.string().optional(),
    config: rawJsonSchema,
    status: forwardCompatibleEnumSchema,
    skip_next_run: z.boolean(),
    is_paper: z.boolean(),
    created_at: isoDateSchema,
    updated_at: isoDateSchema,
    latest_run_summary: strategyLatestRunSummarySchema.optional(),
  })
  .passthrough()

export const strategyCreateRequestSchema = z.object({
  name: z.string().trim().min(1, 'Name is required'),
  description: z.string().trim().optional(),
  ticker: z.string().trim().min(1, 'Ticker is required'),
  market_type: z.enum(['stock', 'crypto', 'polymarket', 'kalshi', 'options']),
  schedule_cron: z.string().trim().optional(),
  config: rawJsonSchema,
  is_paper: z.literal(true),
})

export const strategyUpdateRequestSchema = z.object({
  name: z.string().trim().min(1, 'Name is required'),
  description: z.string().trim().optional(),
  ticker: z.string().trim().min(1, 'Ticker is required'),
  market_type: z.enum(['stock', 'crypto', 'polymarket', 'kalshi', 'options']),
  schedule_cron: z.string().trim().optional(),
  config: rawJsonSchema,
  updated_at: isoDateSchema,
})

export const strategyRunAcceptedResponseSchema = z
  .object({
    status: forwardCompatibleEnumSchema,
    strategy_id: uuidSchema,
    message: z.string().min(1),
  })
  .passthrough()

export const reportArtifactSchema = z
  .object({
    id: uuidSchema,
    strategy_id: uuidSchema,
    report_type: z.string().min(1),
    time_bucket: isoDateSchema,
    status: forwardCompatibleEnumSchema,
    report_json: rawJsonSchema.optional(),
    provider: z.string().optional(),
    model: z.string().optional(),
    prompt_tokens: z.number().int().nonnegative(),
    completion_tokens: z.number().int().nonnegative(),
    latency_ms: z.number().int().nonnegative(),
    error_message: z.string().optional(),
    created_at: isoDateSchema,
    completed_at: optionalNullable(isoDateSchema).optional(),
  })
  .passthrough()

export const reportLatestResponseSchema = reportArtifactSchema.extend({
  stale_seconds: z.number().nonnegative(),
})

export const pipelineRunSchema = z
  .object({
    id: uuidSchema,
    strategy_id: uuidSchema,
    ticker: z.string().min(1),
    trade_date: isoDateSchema,
    status: forwardCompatibleEnumSchema,
    signal: forwardCompatibleEnumSchema.optional(),
    started_at: isoDateSchema,
    completed_at: optionalNullable(isoDateSchema).optional(),
    error_message: z.string().optional(),
    config_snapshot: rawJsonSchema.optional(),
    phase_timings: rawJsonSchema.optional(),
  })
  .passthrough()

export const agentDecisionSchema = z
  .object({
    id: uuidSchema,
    pipeline_run_id: uuidSchema,
    agent_role: z.string().min(1),
    phase: z.string().min(1),
    round_number: z.number().int().optional(),
    input_summary: z.string().optional(),
    output_text: z.string(),
    output_structured: rawJsonSchema.optional(),
    llm_provider: z.string().optional(),
    llm_model: z.string().optional(),
    prompt_text: z.string().optional(),
    prompt_tokens: z.number().int().nonnegative().optional(),
    completion_tokens: z.number().int().nonnegative().optional(),
    latency_ms: z.number().int().nonnegative().optional(),
    cost_usd: z.number().nonnegative().optional(),
    created_at: isoDateSchema,
  })
  .passthrough()

export const runSnapshotSchema = z.record(z.string().min(1), rawJsonSchema)

export const agentEventSchema = z
  .object({
    id: uuidSchema,
    pipeline_run_id: uuidSchema.optional(),
    strategy_id: uuidSchema.optional(),
    agent_role: z.string().optional(),
    event_kind: z.string().min(1),
    title: z.string().min(1),
    summary: z.string().optional(),
    tags: z.array(z.string()).optional(),
    metadata: rawJsonSchema.optional(),
    created_at: isoDateSchema,
  })
  .passthrough()

const numberMapSchema = z.record(z.string(), z.number().int().nonnegative())

export const allocatorDiagnosticsSchema = z
  .object({
    run_counts_by_signal: numberMapSchema,
    run_counts_by_status: numberMapSchema,
    decision_counts_by_status: numberMapSchema,
    no_action_reasons: numberMapSchema,
    active_strategies_by_market: numberMapSchema,
    open_positions_by_market: numberMapSchema,
    buying_power_utilization_pct: z.number(),
    gross_exposure_pct: z.number(),
    target_gross_exposure_pct: z.number(),
    utilization_gap_pct: z.number(),
    warnings: z.array(z.string()),
  })
  .passthrough()

export const allocatorOpportunitySchema = z
  .object({
    id: uuidSchema,
    strategy_id: uuidSchema,
    pipeline_run_id: uuidSchema.optional(),
    market_type: forwardCompatibleEnumSchema,
    ticker: z.string().min(1),
    side: z.string().min(1),
    prediction_side: z.string().optional(),
    signal: z.string().min(1),
    status: z.string().min(1),
    score: z.number().optional(),
    confidence: z.number(),
    edge_pct: z.number(),
    expected_return_pct: z.number(),
    max_loss_pct: z.number(),
    entry_price: z.number(),
    liquidity_usd: z.number(),
    market_cap_usd: z.number(),
    spread_pct: z.number(),
    proposed_notional: z.number(),
    selected_notional: z.number(),
    reason: z.string(),
    reject_reason: z.string().optional(),
    evidence: rawJsonSchema.optional(),
    expires_at: isoDateSchema,
    created_at: isoDateSchema,
    updated_at: isoDateSchema,
    dedupe_key: z.string().min(1),
  })
  .passthrough()

export const allocationDecisionSchema = z
  .object({
    id: uuidSchema,
    opportunity_id: uuidSchema.optional(),
    strategy_id: uuidSchema.optional(),
    mode: z.string().min(1),
    action: z.string().min(1),
    score: z.number(),
    notional_usd: z.number(),
    quantity: z.number(),
    reasons: z.array(z.string()),
    created_order_id: uuidSchema.optional(),
    created_at: isoDateSchema,
  })
  .passthrough()

export const allocatorSummarySchema = z
  .object({
    opportunity_counts_by_status: numberMapSchema,
    recent_decisions: z.array(allocationDecisionSchema),
    warnings: z.array(z.string()).optional(),
  })
  .passthrough()

export const positionSchema = z
  .object({
    id: uuidSchema,
    strategy_id: uuidSchema.optional(),
    market_type: forwardCompatibleEnumSchema.optional(),
    ticker: z.string().min(1),
    side: forwardCompatibleEnumSchema,
    quantity: z.number(),
    avg_entry: z.number(),
    current_price: z.number().optional(),
    unrealized_pnl: z.number().optional(),
    realized_pnl: z.number(),
    stop_loss: z.number().optional(),
    take_profit: z.number().optional(),
    opened_at: isoDateSchema,
    closed_at: optionalNullable(isoDateSchema).optional(),
  })
  .passthrough()

export const orderSchema = z
  .object({
    id: uuidSchema,
    strategy_id: uuidSchema.optional(),
    pipeline_run_id: uuidSchema.optional(),
    external_id: z.string().optional(),
    ticker: z.string().min(1),
    market_type: forwardCompatibleEnumSchema.optional(),
    side: forwardCompatibleEnumSchema,
    order_type: forwardCompatibleEnumSchema,
    quantity: z.number(),
    limit_price: z.number().optional(),
    stop_price: z.number().optional(),
    filled_quantity: z.number(),
    filled_avg_price: z.number().optional(),
    status: forwardCompatibleEnumSchema,
    broker: z.string(),
    submitted_at: optionalNullable(isoDateSchema).optional(),
    filled_at: optionalNullable(isoDateSchema).optional(),
    created_at: isoDateSchema,
  })
  .passthrough()

export const tradeSchema = z
  .object({
    id: uuidSchema,
    order_id: uuidSchema.optional(),
    position_id: uuidSchema.optional(),
    external_id: z.string().optional(),
    ticker: z.string().min(1),
    side: forwardCompatibleEnumSchema,
    quantity: z.number(),
    price: z.number(),
    fee: z.number(),
    executed_at: isoDateSchema,
    created_at: isoDateSchema,
  })
  .passthrough()

export const orderDetailResponseSchema = z
  .object({
    order: orderSchema,
    fills: z.array(tradeSchema),
  })
  .passthrough()

export const riskSettingsSchema = z
  .object({
    max_position_size_pct: z.number(),
    max_daily_loss_pct: z.number(),
    max_drawdown_pct: z.number(),
    max_open_positions: z.number().int(),
    max_total_exposure_pct: z.number().optional(),
    max_per_market_exposure_pct: z.number().optional(),
    circuit_breaker_threshold_pct: z.number().optional(),
    circuit_breaker_cooldown_min: z.number().optional(),
  })
  .passthrough()

export const riskEngineStatusSchema = z
  .object({
    risk_status: forwardCompatibleEnumSchema,
    circuit_breaker: z
      .object({
        state: forwardCompatibleEnumSchema,
        reason: z.string().optional(),
        tripped_at: optionalNullable(isoDateSchema).optional(),
        cooldown_end: optionalNullable(isoDateSchema).optional(),
      })
      .passthrough(),
    kill_switch: z
      .object({
        active: z.boolean(),
        reason: z.string().optional(),
        mechanisms: z.array(forwardCompatibleEnumSchema).optional(),
        activated_at: optionalNullable(isoDateSchema).optional(),
      })
      .passthrough(),
    market_kill_switches: z.record(z.string(), z.unknown()).optional(),
    position_limits: z
      .object({
        max_per_position_pct: z.number(),
        max_total_pct: z.number(),
        max_concurrent: z.number().int(),
        max_per_market_pct: z.number(),
        current_open_positions: z.number().int().optional(),
        current_total_exposure_pct: z.number().optional(),
      })
      .passthrough(),
    updated_at: isoDateSchema,
  })
  .passthrough()

export const riskBreakersResponseSchema = z
  .object({
    tripped: z.array(
      z
        .object({
          scope: z.string(),
          tripped_at: isoDateSchema,
          reason: z.string(),
          reset_at: optionalNullable(isoDateSchema).optional(),
        })
        .passthrough(),
    ),
  })
  .passthrough()

export const riskCockpitSummarySchema = z
  .object({
    generated_at: isoDateSchema,
    kill_switch_active: z.boolean(),
    circuit_breaker: z.boolean(),
    exposures: z.array(
      z.object({
        market_type: forwardCompatibleEnumSchema,
        open_positions: z.number().int(),
        approved_decisions: z.number().int(),
        rejected_decisions: z.number().int(),
        gross_exposure: z.number(),
        net_expected_value: z.number(),
      }).passthrough(),
    ),
    warnings: z.array(z.string()),
  })
  .passthrough()

export const killSwitchToggleRequestSchema = z.object({
  active: z.boolean(),
  reason: z.string().trim().min(1, 'Reason is required'),
})

export const killSwitchToggleResponseSchema = z
  .object({
    active: z.boolean(),
    reason: z.string().optional(),
    mechanisms: z.array(forwardCompatibleEnumSchema).optional(),
    activated_at: optionalNullable(isoDateSchema).optional(),
    updated_at: isoDateSchema,
  })
  .passthrough()

export const marketKillSwitchRequestSchema = z.object({
  reason: z.string().trim().min(1, 'Reason is required'),
})

export const marketKillSwitchResponseSchema = z
  .object({
    market_type: forwardCompatibleEnumSchema,
    active: z.boolean(),
  })
  .passthrough()

export const breakerResetRequestSchema = z.object({
  scope: z.string().trim().min(1, 'Scope is required'),
})

export const breakerResetResponseSchema = z
  .object({
    scope: z.string(),
    reset: z.boolean(),
  })
  .passthrough()

export const automationHealthResponseSchema = z
  .object({
    jobs: z.array(
      z
        .object({
          name: z.string(),
          enabled: z.boolean(),
          running: z.boolean(),
          last_run: optionalNullable(isoDateSchema).optional(),
          last_error: z.string().optional(),
          error_count: z.number().int(),
          consecutive_failures: z.number().int(),
          run_count: z.number().int(),
        })
        .passthrough(),
    ),
    healthy: z.boolean(),
    total_jobs: z.number().int(),
    failing_jobs: z.number().int(),
    degraded_jobs: z.number().int(),
  })
  .passthrough()

export const automationJobStatusSchema = z
  .object({
    name: z.string(),
    description: z.string(),
    schedule: z.string(),
    last_run: optionalNullable(isoDateSchema).optional(),
    last_result: z.string(),
    last_summary: z.record(z.string(), z.unknown()).optional(),
    last_error: z.string().optional(),
    last_error_at: optionalNullable(isoDateSchema).optional(),
    run_count: z.number().int(),
    error_count: z.number().int(),
    consecutive_failures: z.number().int(),
    stuck_for: z.number().optional(),
    running: z.boolean(),
    enabled: z.boolean(),
  })
  .passthrough()

export const automationJobStatusListSchema = z.array(automationJobStatusSchema)

export const automationJobRunSchema = z
  .object({
    id: uuidSchema,
    job_name: z.string(),
    status: forwardCompatibleEnumSchema,
    started_at: isoDateSchema,
    completed_at: optionalNullable(isoDateSchema).optional(),
    duration_ns: z.number().optional(),
    error: z.string().optional(),
    last_error_at: optionalNullable(isoDateSchema).optional(),
    consecutive_failures: z.number().int(),
    created_at: isoDateSchema,
  })
  .passthrough()

export const healthStatusResponseSchema = z
  .object({
    status: forwardCompatibleEnumSchema,
    db: forwardCompatibleEnumSchema,
    redis: forwardCompatibleEnumSchema,
  })
  .passthrough()

export const portfolioSummarySchema = z
  .object({
    open_positions: z.number().int().nonnegative(),
    unrealized_pnl: z.number(),
    realized_pnl: z.number(),
  })
  .passthrough()

const llmProviderResponseSchema = z
  .object({
    api_key_configured: z.boolean(),
    api_key_last4: z.string().optional(),
    base_url: z.string().optional(),
    model: z.string(),
  })
  .passthrough()

export const settingsResponseSchema = z
  .object({
    llm: z
      .object({
        default_provider: z.string(),
        deep_think_model: z.string(),
        quick_think_model: z.string(),
        providers: z
          .object({
            openai: llmProviderResponseSchema,
            anthropic: llmProviderResponseSchema,
            google: llmProviderResponseSchema,
            openrouter: llmProviderResponseSchema,
            xai: llmProviderResponseSchema,
            ollama: llmProviderResponseSchema,
          })
          .passthrough(),
      })
      .passthrough(),
    risk: riskSettingsSchema,
    system: z
      .object({
        environment: z.string(),
        version: z.string(),
        current_schema_version: z.number().int(),
        required_schema_version: z.number().int(),
        schema_status: z.string(),
        uptime_seconds: z.number().int(),
        connected_brokers: z.array(
          z
            .object({
              name: z.string(),
              paper_mode: z.boolean(),
              configured: z.boolean(),
            })
            .passthrough(),
        ),
      })
      .passthrough(),
  })
  .passthrough()

export const websocketCommandSchema = z
  .discriminatedUnion('action', [
    z.object({ action: z.literal('subscribe'), strategy_ids: z.array(uuidSchema).optional(), run_ids: z.array(uuidSchema).optional() }).passthrough(),
    z.object({ action: z.literal('unsubscribe'), strategy_ids: z.array(uuidSchema).optional(), run_ids: z.array(uuidSchema).optional() }).passthrough(),
    z.object({ action: z.literal('subscribe_all') }).passthrough(),
    z.object({ action: z.literal('unsubscribe_all') }).passthrough(),
    z.object({ action: z.literal('subscribe_polymarket') }).passthrough(),
    z.object({ action: z.literal('unsubscribe_polymarket') }).passthrough(),
  ])

export const websocketEventEnvelopeSchema = z
  .object({
    type: forwardCompatibleEnumSchema,
    strategy_id: uuidSchema.optional(),
    run_id: uuidSchema.optional(),
    data: rawJsonSchema.optional(),
    timestamp: isoDateSchema,
  })
  .passthrough()
