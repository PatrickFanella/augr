import type { ReactNode } from 'react'

import { cn } from '@/lib/utils'

interface PageHeaderProps {
  eyebrow?: string
  title: string
  description?: string
  meta?: ReactNode
  actions?: ReactNode
  className?: string
}

export function PageHeader({ title, description, meta, actions, className }: PageHeaderProps) {
  return (
    <section
      className={cn(
        'rounded-lg border border-border bg-card px-4 py-3',
        className,
      )}
    >
      <div className="flex flex-col gap-2 xl:flex-row xl:items-center xl:justify-between">
        <div className="flex flex-wrap items-center gap-2.5">
          <h1 className="text-lg font-semibold tracking-tight text-foreground">
            {title}
          </h1>
          {meta}
          {description ? (
            <span className="text-sm text-muted-foreground">{description}</span>
          ) : null}
        </div>
        {actions ? <div className="flex flex-wrap items-center gap-2">{actions}</div> : null}
      </div>
    </section>
  )
}
