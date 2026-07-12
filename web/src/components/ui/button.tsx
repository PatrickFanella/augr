import { forwardRef, type ButtonHTMLAttributes } from 'react'
import { Slot } from '@radix-ui/react-slot'
import { cva, type VariantProps } from 'class-variance-authority'
import { cn } from '@/lib/utils'

const buttonVariants = cva(
  [
    'inline-flex min-h-10 items-center justify-center gap-2',
    'rounded-md border-2 px-3 py-2 text-sm font-semibold',
    'transition disabled:pointer-events-none disabled:opacity-50',
    'focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-[var(--color-accent-primary)]',
  ],
  {
    variants: {
      variant: {
        primary: [
          'border-[var(--color-accent-primary)] bg-[var(--color-accent-primary)] text-[var(--color-surface-base)]',
          'shadow-[var(--shadow-brutal-sm)] hover:shadow-[var(--shadow-brutal)]',
          'hover:-translate-x-0.5 hover:-translate-y-0.5',
        ],
        secondary: [
          'border-[var(--color-border-strong)] bg-[var(--color-surface-raised)] text-[var(--color-text-primary)]',
          'hover:bg-[var(--color-surface-overlay)]',
        ],
        ghost: [
          'border-transparent bg-transparent text-[var(--color-text-secondary)]',
          'hover:border-[var(--color-border-default)] hover:bg-[var(--color-surface-overlay)] hover:text-[var(--color-text-primary)]',
        ],
        danger: [
          'border-[var(--color-danger)] bg-transparent text-[var(--color-danger)]',
          'hover:bg-[var(--color-danger)] hover:text-[var(--color-surface-base)]',
        ],
        terminal: [
          'border-[var(--color-border-strong)] bg-[var(--color-surface-base)] font-mono text-[var(--color-processing)]',
          'hover:border-[var(--color-processing)]',
        ],
      },
      size: {
        default: '',
        sm: 'min-h-8 px-2 py-1 text-xs',
        icon: 'min-h-10 min-w-10 p-0',
      },
    },
    defaultVariants: {
      variant: 'secondary',
      size: 'default',
    },
  },
)

export type ButtonProps = ButtonHTMLAttributes<HTMLButtonElement> &
  VariantProps<typeof buttonVariants> & {
    asChild?: boolean
  }

export const Button = forwardRef<HTMLButtonElement, ButtonProps>(
  ({ className, variant, size, asChild = false, ...props }, ref) => {
    const Comp = asChild ? Slot : 'button'
    return (
      <Comp
        ref={ref}
        className={cn(buttonVariants({ variant, size }), className)}
        {...props}
      />
    )
  },
)
Button.displayName = 'Button'
