/**
 * Resolves CSS theme variables to hex/rgb strings for use in recharts
 * and other libraries that require concrete color values (not CSS vars).
 *
 * Values are read once at module load and re-read on theme change.
 * Components using these should re-render on theme change via useTheme().
 */

function readVar(name: string): string {
  if (typeof window === 'undefined') return ''
  return getComputedStyle(document.documentElement).getPropertyValue(name).trim()
}

export type ChartColors = {
  accent: string
  accentSecondary: string
  success: string
  danger: string
  warning: string
  info: string
  long: string
  short: string
  /** Palette for pie/distribution charts */
  distribution: string[]
}

function resolveColors(): ChartColors {
  return {
    accent: readVar('--color-accent-primary') || '#cba6f7',
    accentSecondary: readVar('--color-accent-secondary') || '#89b4fa',
    success: readVar('--color-success') || '#a6e3a1',
    danger: readVar('--color-danger') || '#f38ba8',
    warning: readVar('--color-warning') || '#f9e2af',
    info: readVar('--color-info') || '#89dceb',
    long: readVar('--color-long') || '#a6e3a1',
    short: readVar('--color-short') || '#f38ba8',
    distribution: [
      readVar('--color-accent-primary') || '#cba6f7',
      readVar('--color-accent-secondary') || '#89b4fa',
      readVar('--color-info') || '#89dceb',
      readVar('--color-running') || '#74c7ec',
      readVar('--color-success') || '#a6e3a1',
      readVar('--color-paused') || '#b4befe',
    ],
  }
}

let cached: ChartColors | null = null

export function getChartColors(): ChartColors {
  if (!cached) cached = resolveColors()
  return cached
}

/** Force re-read of CSS variables (call on theme change) */
export function refreshChartColors(): void {
  cached = resolveColors()
}
