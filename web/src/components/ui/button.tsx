/* eslint-disable react-refresh/only-export-components */
import { Slot } from '@radix-ui/react-slot'
import { cva, type VariantProps } from 'class-variance-authority'
import { type ComponentProps } from 'react'

import { cn } from '@/lib/utils'

const buttonVariants = cva(
  'inline-flex cursor-pointer items-center justify-center gap-2 whitespace-nowrap rounded-md border border-transparent text-sm font-medium tracking-[0.02em] transition-all disabled:pointer-events-none disabled:opacity-50 [&_svg]:pointer-events-none [&_svg]:size-4 [&_svg]:shrink-0 outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 focus-visible:ring-offset-background active:scale-[0.985]',
  {
    variants: {
      variant: {
        default:
          'bg-primary text-primary-foreground shadow-[0_0_0_1px_rgba(255,255,255,0.03),0_10px_30px_rgba(96,165,250,0.16)] hover:brightness-110',
        secondary:
          'border-white/10 bg-secondary text-secondary-foreground shadow-[inset_0_1px_0_rgba(255,255,255,0.03)] hover:border-white/20 hover:bg-secondary/85',
        outline:
          'border-input bg-background/80 text-foreground shadow-[inset_0_1px_0_rgba(255,255,255,0.02)] hover:border-primary/35 hover:bg-accent hover:text-accent-foreground',
        ghost: 'text-muted-foreground hover:bg-accent/80 hover:text-accent-foreground',
        destructive:
          'bg-destructive text-destructive-foreground shadow-[0_0_0_1px_rgba(255,255,255,0.03),0_10px_24px_rgba(239,68,68,0.18)] hover:brightness-110',
      },
      size: {
        default: 'h-9 px-3.5 py-2',
        sm: 'h-8 rounded-md px-3 text-xs',
        lg: 'h-10 rounded-md px-5',
        dense: 'h-8 rounded-md px-2.5 text-xs font-medium',
      },
    },
    defaultVariants: {
      variant: 'default',
      size: 'default',
    },
  },
)

type ButtonProps = ComponentProps<'button'> &
  VariantProps<typeof buttonVariants> & {
    asChild?: boolean
  }

function Button({ className, variant, size, asChild = false, ...props }: ButtonProps) {
  const Comp = asChild ? Slot : 'button'

  return <Comp className={cn(buttonVariants({ variant, size, className }))} {...props} />
}

export { Button, buttonVariants }
