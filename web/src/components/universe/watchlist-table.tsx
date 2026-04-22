import { useNavigate } from 'react-router-dom'

import { Badge } from '@/components/ui/badge'
import type { ScoredTicker } from '@/lib/api/types'

interface WatchlistTableProps {
  tickers: ScoredTicker[]
}

function scoreBadgeVariant(score: number | undefined) {
  if ((score ?? 0) > 0.7) return 'success' as const
  if ((score ?? 0) > 0.4) return 'warning' as const
  return 'destructive' as const
}

export function WatchlistTable({ tickers }: WatchlistTableProps) {
  const navigate = useNavigate()

  if (tickers.length === 0) {
    return <p className="py-4 text-sm text-muted-foreground">No scored tickers.</p>
  }

  return (
    <div className="overflow-x-auto">
      <table className="w-full text-left text-sm">
        <thead>
          <tr className="border-b border-border text-xs font-medium uppercase tracking-wider text-muted-foreground">
            <th className="px-2 py-2">Ticker</th>
            <th className="px-2 py-2">Score</th>
            <th className="px-2 py-2 text-right">Change%</th>
            <th className="px-2 py-2 text-right">Gap%</th>
            <th className="px-2 py-2 text-right">Volume</th>
            <th className="px-2 py-2 text-right">Close</th>
            <th className="px-2 py-2">Reasons</th>
          </tr>
        </thead>
        <tbody>
          {tickers.map((t) => (
            <tr
              key={t.ticker}
              className="cursor-pointer border-b border-border/50 hover:bg-accent/30"
              onClick={() =>
                navigate(`/discovery?tickers=${encodeURIComponent(t.ticker)}`)
              }
            >
              <td className="px-2 py-1.5 font-mono font-medium">{t.ticker}</td>
              <td className="px-2 py-1.5">
                <Badge variant={scoreBadgeVariant(t.score)}>
                  {(t.score ?? 0).toFixed(2)}
                </Badge>
              </td>
              <td
                className={`px-2 py-1.5 text-right font-mono ${
                  (t.change_pct ?? 0) >= 0 ? 'text-emerald-400' : 'text-red-400'
                }`}
              >
                {(t.change_pct ?? 0) >= 0 ? '+' : ''}
                {(t.change_pct ?? 0).toFixed(2)}%
              </td>
              <td
                className={`px-2 py-1.5 text-right font-mono ${
                  (t.gap_pct ?? 0) >= 0 ? 'text-emerald-400' : 'text-red-400'
                }`}
              >
                {(t.gap_pct ?? 0) >= 0 ? '+' : ''}
                {(t.gap_pct ?? 0).toFixed(2)}%
              </td>
              <td className="px-2 py-1.5 text-right font-mono text-muted-foreground">
                {formatVolume(t.day_volume ?? 0)}
              </td>
              <td className="px-2 py-1.5 text-right font-mono text-muted-foreground">
                ${(t.day_close ?? 0).toFixed(2)}
              </td>
              <td className="px-2 py-1.5">
                <div className="flex flex-wrap gap-1">
                  {(t.reasons ?? []).map((reason, i) => (
                    <Badge key={i} variant="secondary" className="text-[10px]">
                      {reason}
                    </Badge>
                  ))}
                </div>
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  )
}

function formatVolume(vol: number): string {
  if (vol >= 1_000_000_000) return `${(vol / 1_000_000_000).toFixed(1)}B`
  if (vol >= 1_000_000) return `${(vol / 1_000_000).toFixed(1)}M`
  if (vol >= 1_000) return `${(vol / 1_000).toFixed(1)}K`
  return vol.toFixed(0)
}
