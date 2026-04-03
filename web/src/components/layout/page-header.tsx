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

export function PageHeader({ eyebrow, title, description, meta, actions, className }: PageHeaderProps) {
  return (
    <section
      className={cn(
        'rounded-lg border border-white/10 bg-card/85 px-4 py-4 shadow-[0_0_0_1px_rgba(255,255,255,0.02),0_14px_40px_rgba(2,6,23,0.34)] backdrop-blur-sm',
        className,
      )}
    >
      <div className="flex flex-col gap-4 xl:flex-row xl:items-start xl:justify-between">
        <div className="space-y-2">
          {eyebrow ? (
            <p className="font-mono text-[11px] font-medium uppercase tracking-[0.22em] text-primary/80">
              {eyebrow}
            </p>
          ) : null}
          <div className="flex flex-wrap items-center gap-2.5">
            <h1 className="text-2xl font-semibold tracking-tight text-foreground sm:text-[1.75rem]">
              {title}
            </h1>
            {meta}
          </div>
          {description ? (
            <p className="max-w-3xl text-sm leading-6 text-muted-foreground">{description}</p>
          ) : null}
        </div>
        {actions ? <div className="flex flex-wrap items-center gap-2">{actions}</div> : null}
      </div>
    </section>
  )
}
