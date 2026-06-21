import type { RawJson } from '@/shared/types/primitives'

export const apiErrorCodes = [
  'ERR_BAD_REQUEST',
  'ERR_NOT_FOUND',
  'ERR_NOT_IMPLEMENTED',
  'ERR_INTERNAL',
  'ERR_VALIDATION',
  'ERR_METHOD_NOT_ALLOWED',
  'ERR_UNAUTHORIZED',
  'ERR_RATE_LIMITED',
  'ERR_CONFLICT',
] as const

export type KnownApiErrorCode = (typeof apiErrorCodes)[number]
export type ApiErrorCode = KnownApiErrorCode | (string & {})

export type ApiError = {
  error: string
  code: ApiErrorCode
  details?: RawJson
}

export type ListResponse<T> = {
  data: T[]
  total?: number
  limit: number
  offset: number
}

export type PortfolioSummary = {
  open_positions: number
  unrealized_pnl: number
  realized_pnl: number
}
