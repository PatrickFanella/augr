import { redact } from '@/shared/logging/redact'

export const logger = {
  warn(message: string, meta?: unknown) {
    if (import.meta.env.DEV) console.warn(message, redact(meta))
  },
  error(message: string, meta?: unknown) {
    if (import.meta.env.DEV) console.error(message, redact(meta))
  },
}
