import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { HudBadge, HudRow, StatusLed } from '@/components/ui/hud'

type EventMarketSummaryCardProps = {
  provider: string
  watchedMarkets: number
  activePaper: number
  lastRunStatus: string
  liveTradingReady: boolean
}

export function EventMarketSummaryCard({
  provider,
  watchedMarkets,
  activePaper,
  lastRunStatus,
  liveTradingReady,
}: EventMarketSummaryCardProps) {
  const statusLabel = liveTradingReady ? 'live ready' : 'paper/data only'

  return (
    <Card data-testid="event-market-summary-card" className="overflow-hidden">
      <div className="h-1 bg-[linear-gradient(90deg,hsl(var(--caution))_0%,hsl(var(--signal))_50%,hsl(var(--pulse))_100%)]" />
      <CardHeader className="gap-3">
        <div className="flex items-start justify-between gap-3">
          <div className="space-y-1">
            <p className="text-[11px] uppercase tracking-[0.18em] text-muted-foreground">Shared summary</p>
            <CardTitle>{provider}</CardTitle>
            <p className="text-sm text-ink-dim">Paper/data status at a glance.</p>
          </div>
          <StatusLed state={liveTradingReady ? 'ok' : 'warn'} label={statusLabel} />
        </div>

        <div className="flex flex-wrap gap-2">
          <HudBadge tone={liveTradingReady ? 'confirm' : 'caution'}>{liveTradingReady ? 'Live ready' : 'No live submit'}</HudBadge>
          <HudBadge tone="ink">{lastRunStatus}</HudBadge>
        </div>
      </CardHeader>

      <CardContent className="grid gap-3 md:grid-cols-3">
        <HudRow label="Watched markets" value={watchedMarkets} />
        <HudRow label="Active paper" value={activePaper} />
        <HudRow label="Discovery" value={lastRunStatus} />
      </CardContent>
    </Card>
  )
}
