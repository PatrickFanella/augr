const sensitiveKeys = new Set([
  'authorization',
  'access_token',
  'refresh_token',
  'password',
  'current_password',
  'new_password',
  'api_key',
  'x-api-key',
  'x-admin-key',
  'token',
])

export function redactUrl(value: string): string {
  try {
    const url = new URL(value, window.location.origin)
    for (const key of [...url.searchParams.keys()]) {
      if (sensitiveKeys.has(key.toLowerCase())) {
        url.searchParams.set(key, '[redacted]')
      }
    }
    return url.pathname + url.search + url.hash
  } catch {
    return value.replace(/([?&](?:token|api_key)=)[^&]+/gi, '$1[redacted]')
  }
}

export function redact(value: unknown): unknown {
  if (Array.isArray(value)) return value.map(redact)
  if (!value || typeof value !== 'object') return value
  return Object.fromEntries(
    Object.entries(value).map(([key, entry]) => [
      key,
      sensitiveKeys.has(key.toLowerCase()) ? '[redacted]' : redact(entry),
    ]),
  )
}
