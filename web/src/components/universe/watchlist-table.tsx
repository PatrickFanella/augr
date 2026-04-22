import { useNavigate } from 'react-router-dom'

import { Badge } from '@/components/ui/badge'
import type { ScoredTicker, TrackedTicker } from '@/lib/api/types'

type WatchlistTicker = ScoredTicker | TrackedTicker

interface WatchlistTableProps {
  tickers: WatchlistTicker[]
}

function scoreBadgeVariant(score: number | undefined) {
  if ((score ?? 0) > 0.7) return 'success' as const
  if ((score ?? 0) > 0.4) return 'warning' as const
  return 'destructive' as const
}

function resolveTickerScore(ticker: WatchlistTicker): number {
  if ('score' in ticker && typeof ticker.score === 'number') {
    return ticker.score
  }
  if ('watch_score' in ticker && typeof ticker.watch_score === 'number') {
    return ticker.watch_score
  }
  return 0
}

function resolveTickerChangePct(ticker: WatchlistTicker): number {
  return 'change_pct' in ticker && typeof ticker.change_pct === 'number' ? ticker.change_pct : 0
}

function resolveTickerGapPct(ticker: WatchlistTicker): number {
  return 'gap_pct' in ticker && typeof ticker.gap_pct === 'number' ? ticker.gap_pct : 0
}

function resolveTickerDayVolume(ticker: WatchlistTicker): number {
  return 'day_volume' in ticker && typeof ticker.day_volume === 'number' ? ticker.day_volume : 0
}

function resolveTickerDayClose(ticker: WatchlistTicker): number {
  return 'day_close' in ticker && typeof ticker.day_close === 'number' ? ticker.day_close : 0
}

function resolveTickerReasons(ticker: WatchlistTicker): string[] {
  return 'reasons' in ticker && Array.isArray(ticker.reasons) ? ticker.reasons : []
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
          {tickers.map((t) => {
            const score = resolveTickerScore(t)
            const changePct = resolveTickerChangePct(t)
            const gapPct = resolveTickerGapPct(t)
            const dayVolume = resolveTickerDayVolume(t)
            const dayClose = resolveTickerDayClose(t)
            const reasons = resolveTickerReasons(t)

            return (
              <tr
                key={t.ticker}
                className="cursor-pointer border-b border-border/50 hover:bg-accent/30"
                onClick={() => navigate(`/discovery?tickers=${encodeURIComponent(t.ticker)}`)}
              >
                <td className="px-2 py-1.5 font-mono font-medium">{t.ticker}</td>
                <td className="px-2 py-1.5">
                  <Badge variant={scoreBadgeVariant(score)}>{score.toFixed(2)}</Badge>
                </td>
                <td
                  className={`px-2 py-1.5 text-right font-mono ${
                    changePct >= 0 ? 'text-emerald-400' : 'text-red-400'
                  }`}
                >
                  {changePct >= 0 ? '+' : ''}
                  {changePct.toFixed(2)}%
                </td>
                <td
                  className={`px-2 py-1.5 text-right font-mono ${
                    gapPct >= 0 ? 'text-emerald-400' : 'text-red-400'
                  }`}
                >
                  {gapPct >= 0 ? '+' : ''}
                  {gapPct.toFixed(2)}%
                </td>
                <td className="px-2 py-1.5 text-right font-mono text-muted-foreground">
                  {formatVolume(dayVolume)}
                </td>
                <td className="px-2 py-1.5 text-right font-mono text-muted-foreground">
                  ${dayClose.toFixed(2)}
                </td>
                <td className="px-2 py-1.5">
                  <div className="flex flex-wrap gap-1">
                    {reasons.map((reason, i) => (
                      <Badge key={i} variant="secondary" className="text-[10px]">
                        {reason}
                      </Badge>
                    ))}
                  </div>
                </td>
              </tr>
            )
          })}
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
