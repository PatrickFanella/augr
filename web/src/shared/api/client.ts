import { parseContract } from '@/shared/api/contract'
import { ApiClientError, toApiClientError } from '@/shared/api/errors'
import { apiErrorSchema } from '@/shared/api/schemas'
import { refreshAccessToken } from '@/shared/auth/refresh'
import { getAccessToken } from '@/shared/auth/tokenStore'
import { redactUrl } from '@/shared/logging/redact'

export type HttpMethod = 'GET' | 'POST' | 'PUT' | 'PATCH' | 'DELETE'

export type ApiRequestOptions<T> = {
  method?: HttpMethod
  body?: unknown
  signal?: AbortSignal
  schema?: { parse: (value: unknown) => T }
  auth?: boolean
  retryOnUnauthorized?: boolean
  headers?: HeadersInit
}

export type ApiClientConfig = {
  baseUrl: string
}

let config: ApiClientConfig = { baseUrl: '/api/v1' }

export function configureApiClient(next: ApiClientConfig): void {
  config = next
}

export async function apiRequest<T = unknown>(path: string, options: ApiRequestOptions<T> = {}): Promise<T> {
  return sendRequest(path, options, false)
}

async function sendRequest<T>(path: string, options: ApiRequestOptions<T>, didRefresh: boolean): Promise<T> {
  const method = options.method ?? 'GET'
  const url = `${config.baseUrl}${path}`
  const headers = new Headers({ accept: 'application/json' })
  if (options.headers) {
    new Headers(options.headers).forEach((value, key) => headers.set(key, value))
  }
  if (options.body !== undefined) headers.set('content-type', 'application/json')
  if (options.auth !== false) {
    const token = getAccessToken()
    if (token) headers.set('authorization', `Bearer ${token}`)
  }

  let response: Response
  try {
    response = await fetch(url, {
      method,
      headers,
      body: options.body === undefined ? undefined : JSON.stringify(options.body),
      signal: options.signal,
    })
  } catch (error) {
    if (error instanceof DOMException && error.name === 'AbortError') throw error
    throw new ApiClientError('Network request failed', { kind: 'network', endpoint: redactUrl(url) })
  }

  const body = response.status === 204 ? undefined : await response.json().catch(() => undefined)

  if (response.status === 401 && method === 'GET' && options.auth !== false && options.retryOnUnauthorized !== false && !didRefresh) {
    await refreshAccessToken()
    return sendRequest(path, options, true)
  }

  if (!response.ok) {
    const errorBody = apiErrorSchema.safeParse(body)
    throw toApiClientError(response.status, errorBody.success ? errorBody.data : undefined, redactUrl(url))
  }

  if (!options.schema) return body as T
  return parseContract(`${method} ${path}`, options.schema as never, body) as T
}

export const api = {
  get<T>(path: string, options: Omit<ApiRequestOptions<T>, 'method' | 'body'> = {}) {
    return apiRequest<T>(path, { ...options, method: 'GET' })
  },
  post<T>(path: string, body?: unknown, options: Omit<ApiRequestOptions<T>, 'method' | 'body'> = {}) {
    return apiRequest<T>(path, { ...options, method: 'POST', body, retryOnUnauthorized: options.retryOnUnauthorized ?? false })
  },
  put<T>(path: string, body?: unknown, options: Omit<ApiRequestOptions<T>, 'method' | 'body'> = {}) {
    return apiRequest<T>(path, { ...options, method: 'PUT', body, retryOnUnauthorized: options.retryOnUnauthorized ?? false })
  },
  delete<T>(path: string, options: Omit<ApiRequestOptions<T>, 'method' | 'body'> = {}) {
    return apiRequest<T>(path, { ...options, method: 'DELETE', retryOnUnauthorized: options.retryOnUnauthorized ?? false })
  },
}
