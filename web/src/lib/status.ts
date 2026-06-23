/**
 * Unified status system — maps status values to color token, icon, and label.
 * Use with StatusBadge, or directly with the `.status-pill` CSS classes.
 */

export type AppStatus =
  | 'success'
  | 'warning'
  | 'caution'
  | 'danger'
  | 'running'
  | 'processing'
  | 'paused'
  | 'unknown'

export type StatusConfig = {
  label: string
  /** CSS class suffix for `.status-pill.<token>` */
  pillClass: string
  /** CSS variable token name (without --color- prefix) */
  token: string
  icon: string
}

export const statusConfig: Record<AppStatus, StatusConfig> = {
  success: {
    label: 'Success',
    pillClass: 'completed',
    token: 'success',
    icon: '✓',
  },
  warning: {
    label: 'Warning',
    pillClass: 'warning',
    token: 'warning',
    icon: '!',
  },
  caution: {
    label: 'Caution',
    pillClass: 'caution',
    token: 'caution',
    icon: '△',
  },
  danger: {
    label: 'Error',
    pillClass: 'failed',
    token: 'danger',
    icon: '×',
  },
  running: {
    label: 'Running',
    pillClass: 'running',
    token: 'running',
    icon: '●',
  },
  processing: {
    label: 'Processing',
    pillClass: 'processing',
    token: 'processing',
    icon: '◇',
  },
  paused: {
    label: 'Paused',
    pillClass: 'paused',
    token: 'paused',
    icon: 'Ⅱ',
  },
  unknown: {
    label: 'Unknown',
    pillClass: 'unknown',
    token: 'neutral',
    icon: '?',
  },
}

/**
 * Maps an arbitrary status string from the API to an AppStatus.
 * Handles common variations like "ok", "healthy", "connected", "failed", etc.
 */
export function normalizeStatus(value: string | undefined | null): AppStatus {
  if (!value) return 'unknown'
  const normalized = value.toLowerCase()
  if (['ok', 'safe', 'normal', 'closed', 'connected', 'healthy', 'completed', 'success', 'active', 'synced'].includes(normalized)) return 'success'
  if (['running', 'live', 'executing', 'started'].includes(normalized)) return 'running'
  if (['processing', 'queued', 'transforming', 'loading', 'pending'].includes(normalized)) return 'processing'
  if (['paused', 'suspended', 'waiting', 'inactive'].includes(normalized)) return 'paused'
  if (['warning', 'degraded', 'disconnected', 'tripped'].includes(normalized)) return 'warning'
  if (['caution', 'partial', 'unstable', 'risky'].includes(normalized)) return 'caution'
  if (['failed', 'error', 'breached', 'danger', 'critical'].includes(normalized)) return 'danger'
  if (['unknown', 'unavailable', 'not_configured', 'cancelled'].includes(normalized)) return 'unknown'
  return 'unknown'
}
