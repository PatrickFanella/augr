import { isApiClientError } from '@/shared/api/errors'
import { RefreshCw } from 'lucide-react'

export function LoadingState({ label = 'Loading…' }: { label?: string }) {
  return <p className="muted" aria-live="polite">{label}</p>
}

export function EmptyState({ title, message }: { title: string; message: string }) {
  return (
    <div className="empty-state">
      <h2>{title}</h2>
      <p>{message}</p>
    </div>
  )
}

export function FeatureUnavailable({ message = 'Feature unavailable on this server.' }: { message?: string }) {
  return <div role="status" className="warning-box">{message}</div>
}

export function ErrorState({ error, onRetry }: { error: unknown; onRetry: () => void }) {
  if (isApiClientError(error) && error.kind === 'not_implemented') return <FeatureUnavailable />
  const message = isApiClientError(error) ? error.message : 'Unable to load data.'
  return (
    <div role="alert" className="error-box state-box">
      <p>{message}</p>
      <button type="button" onClick={onRetry}><RefreshCw size={14} /> Reload</button>
    </div>
  )
}

export function StaleBanner({ show, message }: { show: boolean; message: string }) {
  if (!show) return null
  return <div role="status" className="warning-box">{message}</div>
}

export function LastUpdated({ date }: { date?: string | number | Date }) {
  if (!date) return <span className="muted">Last updated: not available</span>
  return <span className="muted">Last updated: {new Date(date).toLocaleString()}</span>
}
