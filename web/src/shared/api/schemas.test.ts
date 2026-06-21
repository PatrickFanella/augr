import { describe, expect, it } from 'vitest'

import { ApiContractError, parseContract } from '@/shared/api/contract'
import {
  apiErrorSchema,
  authResponseSchema,
  listResponseSchema,
  riskEngineStatusSchema,
  settingsResponseSchema,
  websocketEventEnvelopeSchema,
} from '@/shared/api/schemas'
import { buildAuthResponse, buildRiskStatus, buildRun, buildSettings, buildWebSocketEvent } from '@/test/fixtures'

describe('runtime schemas', () => {
  it('validates auth responses and preserves unknown fields', () => {
    const parsed = parseContract('auth', authResponseSchema, {
      ...buildAuthResponse(),
      debug_trace_id: 'fixture-trace',
    })

    expect(parsed.access_token).toBe('dev-paper-access-token')
    expect(parsed).toHaveProperty('debug_trace_id', 'fixture-trace')
  })

  it('validates shared list envelopes', () => {
    const schema = listResponseSchema(websocketEventEnvelopeSchema)
    const parsed = parseContract('events list', schema, {
      data: [buildWebSocketEvent()],
      total: 1,
      limit: 50,
      offset: 0,
    })

    expect(parsed.data).toHaveLength(1)
  })

  it('accepts unknown enum values for forward compatibility', () => {
    const parsed = parseContract('risk', riskEngineStatusSchema, buildRiskStatus({ risk_status: 'maintenance_mode' }))

    expect(parsed.risk_status).toBe('maintenance_mode')
  })

  it('throws understandable contract errors for invalid sensitive responses', () => {
    expect(() => parseContract('auth', authResponseSchema, { access_token: '' })).toThrow(ApiContractError)
  })

  it('validates important singleton settings and risk responses', () => {
    expect(parseContract('settings', settingsResponseSchema, buildSettings()).system.environment).toBe('development-paper')
    expect(parseContract('risk', riskEngineStatusSchema, buildRiskStatus()).risk_status).toBe('normal')
  })

  it('validates error envelopes including nonstandard codes', () => {
    const parsed = parseContract('error', apiErrorSchema, { error: 'signal unavailable', code: 'signal_hub_unavailable' })

    expect(parsed.code).toBe('signal_hub_unavailable')
  })

  it('keeps raw payloads explicit for undocumented fields', () => {
    const run = buildRun({ config_snapshot: { nested: { raw: true } } })
    expect(run.config_snapshot).toEqual({ nested: { raw: true } })
  })
})
