import type { ReactNode } from 'react'
import { cn } from '@/lib/utils'

export type PanelProps = {
  title?: string
  eyebrow?: string
  actions?: ReactNode
  children: ReactNode
  className?: string
  wide?: boolean
}

export function Panel({ title, eyebrow, actions, children, className, wide }: PanelProps) {
  return (
    <section
      className={cn(
        'panel min-w-0',
        wide && 'hero-panel',
        className,
      )}
    >
      {(title || eyebrow || actions) && (
        <header className="panel-header">
          <div className="min-w-0">
            {eyebrow && (
              <p className="eyebrow">{eyebrow}</p>
            )}
            {title && (
              <h2 className="truncate">{title}</h2>
            )}
          </div>
          {actions && (
            <div className="header-cluster shrink-0">{actions}</div>
          )}
        </header>
      )}
      <div className="min-w-0">{children}</div>
    </section>
  )
}
