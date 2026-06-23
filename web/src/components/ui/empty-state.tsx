import type { ReactNode } from 'react'

export type EmptyStateProps = {
  icon?: string
  title: string
  description?: string
  actions?: ReactNode
}

export function EmptyState({ icon = '>', title, description, actions }: EmptyStateProps) {
  return (
    <div className="empty-state">
      <div className="font-mono text-3xl text-[var(--color-accent-primary)]">{icon}</div>
      <h3 className="font-bold text-[var(--color-text-primary)]">{title}</h3>
      {description && (
        <p className="mt-2 max-w-md text-sm leading-6">{description}</p>
      )}
      {actions && (
        <div className="mt-4 flex flex-wrap justify-center gap-2">{actions}</div>
      )}
    </div>
  )
}
