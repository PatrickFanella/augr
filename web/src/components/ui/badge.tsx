/* eslint-disable react-refresh/only-export-components */
import { cva, type VariantProps } from 'class-variance-authority'
import { type ComponentProps } from 'react'

import { cn } from '@/lib/utils'

const badgeVariants = cva(
  'inline-flex items-center rounded-full border px-2 py-0.5 font-mono text-[11px] font-medium uppercase tracking-[0.18em] transition-colors',
  {
    variants: {
      variant: {
        default: 'border-primary/25 bg-primary/15 text-primary',
        secondary: 'border-white/10 bg-secondary text-secondary-foreground',
        outline: 'border-white/12 bg-transparent text-foreground',
        destructive: 'border-destructive/30 bg-destructive/12 text-destructive',
        success: 'border-emerald-500/30 bg-emerald-500/12 text-emerald-400',
        warning: 'border-amber-500/30 bg-amber-500/12 text-amber-300',
      },
    },
    defaultVariants: {
      variant: 'default',
    },
  },
)

type BadgeProps = ComponentProps<'span'> & VariantProps<typeof badgeVariants>

function Badge({ className, variant, ...props }: BadgeProps) {
  return <span className={cn(badgeVariants({ variant, className }))} {...props} />
}

export { Badge, badgeVariants }
