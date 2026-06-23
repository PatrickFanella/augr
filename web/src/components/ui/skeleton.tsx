import { cn } from '@/lib/utils'

export type SkeletonProps = {
  className?: string
}

export function Skeleton({ className }: SkeletonProps) {
  return (
    <div
      className={cn(
        'animate-pulse rounded-md bg-[var(--color-surface-overlay)]',
        className,
      )}
    />
  )
}
