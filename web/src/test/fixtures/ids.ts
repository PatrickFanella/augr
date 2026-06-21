import type { ISODate, UUID } from '@/shared/types/primitives'

export const fixtureDate: ISODate = '2026-01-15T12:00:00Z'

export const fixtureLaterDate: ISODate = '2026-01-15T12:05:00Z'

export function fixtureId(index = 1): UUID {
  return `00000000-0000-4000-8000-${String(index).padStart(12, '0')}`
}
