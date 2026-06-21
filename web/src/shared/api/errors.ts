import type { ApiError } from '@/shared/types/api'

export type ApiErrorKind =
  | 'bad_request'
  | 'validation'
  | 'unauthorized'
  | 'not_found'
  | 'conflict'
  | 'rate_limited'
  | 'not_implemented'
  | 'server'
  | 'network'
  | 'contract'
  | 'unknown'

export class ApiClientError extends Error {
  readonly kind: ApiErrorKind
  readonly status?: number
  readonly code?: string
  readonly endpoint?: string
  readonly payload?: unknown

  constructor(message: string, options: { kind: ApiErrorKind; status?: number; code?: string; endpoint?: string; payload?: unknown }) {
    super(message)
    this.name = 'ApiClientError'
    this.kind = options.kind
    this.status = options.status
    this.code = options.code
    this.endpoint = options.endpoint
    this.payload = options.payload
  }
}

export function errorKindFor(status: number, code?: string): ApiErrorKind {
  if (status === 401 || code === 'ERR_UNAUTHORIZED') return 'unauthorized'
  if (status === 404 || code === 'ERR_NOT_FOUND') return 'not_found'
  if (status === 409 || code === 'ERR_CONFLICT') return 'conflict'
  if (status === 422 || code === 'ERR_VALIDATION') return 'validation'
  if (status === 429 || code === 'ERR_RATE_LIMITED') return 'rate_limited'
  if (status === 501 || code === 'ERR_NOT_IMPLEMENTED') return 'not_implemented'
  if (status >= 500) return 'server'
  if (status >= 400) return 'bad_request'
  return 'unknown'
}

export function toApiClientError(status: number, body: Partial<ApiError> | undefined, endpoint: string): ApiClientError {
  const code = body?.code
  return new ApiClientError(body?.error || `Request failed with status ${status}`, {
    kind: errorKindFor(status, code),
    status,
    code,
    endpoint,
    payload: body,
  })
}

export function isApiClientError(error: unknown): error is ApiClientError {
  return error instanceof ApiClientError
}
