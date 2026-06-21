import { describe, expect, it } from 'vitest'

import {
  buildAutomationHealth,
  buildAuthSession,
  buildOrder,
  buildPosition,
  buildRiskStatus,
  buildRun,
  buildStrategy,
  buildTrade,
  buildUser,
  buildWebSocketEvent,
  fixtureDate,
  fixtureId,
} from '@/test/fixtures'

describe('fixture builders', () => {
  it('uses deterministic IDs and dates', () => {
    expect(fixtureId(7)).toBe('00000000-0000-4000-8000-000000000007')
    expect(buildUser().created_at).toBe(fixtureDate)
  })

  it('supports overrides', () => {
    expect(buildStrategy({ status: 'paused' }).status).toBe('paused')
    expect(buildRun({ status: 'completed' }).status).toBe('completed')
    expect(buildPosition({ ticker: 'MSFT' }).ticker).toBe('MSFT')
    expect(buildOrder({ status: 'rejected' }).status).toBe('rejected')
    expect(buildTrade({ fee: 1.23 }).fee).toBe(1.23)
  })

  it('marks test data as development or paper mode', () => {
    expect(buildAuthSession().user.username).toContain('dev-paper')
    expect(buildStrategy().is_paper).toBe(true)
    expect(buildAutomationHealth().jobs[0]?.name).toContain('dev-paper')
    expect(buildWebSocketEvent().data).toEqual({ fixture: true, mode: 'paper' })
    expect(buildRiskStatus().kill_switch.active).toBe(false)
  })
})
