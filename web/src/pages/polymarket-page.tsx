import { useEffect, useMemo, useState } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { Search } from 'lucide-react'
import { Link } from 'react-router-dom'

import { PageHeader } from '@/components/layout/page-header'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { ConsolePanel, HudBadge, HudRow, HudSection, StatusLed } from '@/components/ui/hud'
import { Input } from '@/components/ui/input'
import { apiClient } from '@/lib/api/client'
import { useAddPolymarketWatched, usePolymarketAccounts, usePolymarketDiscoveryLast, usePolymarketJobsStatus, usePolymarketRecentSignals, usePolymarketRecentTrades, usePolymarketWatched, useRemovePolymarketWatched, useRunPolymarketDiscovery, useSetPolymarketAccountTracked, useSetPolymarketWatchedEnabled } from '@/hooks/use-polymarket'
import { useWebSocketClient } from '@/hooks/use-websocket-client'
import type { ResearchOpportunity, StoredSignal, TradeDecisionRiskStatus, TradeDecisionStatus } from '@/lib/api/types'

function formatRelativeTime(iso?: string): string {
  if (!iso) return '--'
  const delta = Date.now() - new Date(iso).getTime()
  if (!Number.isFinite(delta)) return '--'
  const s = Math.floor(delta / 1000)
  if (s < 60) return `${s}s ago`
  const m = Math.floor(s / 60)
  if (m < 60) return `${m}m ago`
  const h = Math.floor(m / 60)
  return h < 24 ? `${h}h ago` : `${Math.floor(h / 24)}d ago`
}

function safeDateLabel(value?: string | null) {
  if (!value) return '—'
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return '—'
  return date.toLocaleString()
}

function safeMoneyLabel(value: unknown) {
  const parsed = typeof value === 'number' ? value : typeof value === 'string' ? Number(value) : Number.NaN
  if (!Number.isFinite(parsed)) return '—'
  return new Intl.NumberFormat('en-US', { style: 'currency', currency: 'USD', maximumFractionDigits: 2 }).format(parsed)
}

function safeNumberLabel(value: unknown, maximumFractionDigits = 0) {
  const parsed = typeof value === 'number' ? value : typeof value === 'string' ? Number(value) : Number.NaN
  if (!Number.isFinite(parsed)) return '—'
  return new Intl.NumberFormat('en-US', { maximumFractionDigits }).format(parsed)
}

function splitReasons(reasons?: string[] | null) {
  if (!Array.isArray(reasons) || reasons.length === 0) return []
  return reasons.map((reason) => reason.trim()).filter(Boolean)
}

const money = new Intl.NumberFormat('en-US', { style: 'currency', currency: 'USD', maximumFractionDigits: 0 })
const money2 = new Intl.NumberFormat('en-US', { style: 'currency', currency: 'USD', maximumFractionDigits: 2 })
const shortAddress = (address: string) => `${address.slice(0, 6)}…${address.slice(-4)}`
const marketUrl = (slug: string) => `https://polymarket.com/market/${encodeURIComponent(slug)}`
const eventUrl = (slug: string) => `https://polymarket.com/event/${encodeURIComponent(slug)}`
const profileUrl = (address: string) => `https://polymarket.com/profile/${encodeURIComponent(address)}`
type AccountSort = 'consistency_score' | 'bayesian_win_rate' | 'resolved_markets' | 'win_rate' | 'volume' | 'last_active' | 'trades'

type ScannerMode = 'preset' | 'manual'

type ScannerPreset = {
  id: string
  label: string
  note: string
  draft: {
    slug?: string
    tokenId?: string
    outcome?: string
    probability?: string
    bestBid?: string
    bestAsk?: string
    askDepthUsd?: string
    askSize?: string
    strategyId?: string
  }
}

const SCANNER_PRESETS: ScannerPreset[] = [
  {
    id: 'liquid-tail',
    label: 'Liquid tail',
    note: 'Cheap contract with enough depth to paper first.',
    draft: { probability: '0.18', bestBid: '0.14', bestAsk: '0.19', askDepthUsd: '7500', askSize: '250' },
  },
  {
    id: 'event-momentum',
    label: 'Event momentum',
    note: 'Near-fair pricing with room for a quick move.',
    draft: { outcome: 'Yes', probability: '0.54', bestBid: '0.51', bestAsk: '0.56', askSize: '150' },
  },
  {
    id: 'strategy-filter',
    label: 'Strategy filter',
    note: 'Narrow to a single strategy and shared signal field.',
    draft: { probability: '0.63', bestBid: '0.60', bestAsk: '0.66' },
  },
]

const WATCHED_MARKET_SUGGESTIONS = [
  {
    id: 'fed-cut',
    slug: 'fed-rate-cut-december-2026',
    note: 'Macro catalyst with frequent repricing around CPI and FOMC data.',
  },
  {
    id: 'btc-150k',
    slug: 'bitcoin-above-150k-2026',
    note: 'High-beta crypto slug that tends to move with headlines.',
  },
  {
    id: 'election-control',
    slug: 'us-house-control-2026',
    note: 'Election control market with persistent event-driven flow.',
  },
]

function formatScheduleDisplay(schedule: string) {
  return schedule.trim() || 'Unscheduled'
}

function hasActiveJobFailure(job: { last_error?: string | null; consecutive_failures?: number | null }) {
  return Boolean(job.last_error?.trim()) || (job.consecutive_failures ?? 0) > 0
}

function hasHistoricalJobFailures(job: { error_count: number }) {
  return job.error_count > 0
}

function safeJsonPreview(value: unknown, maxLength = 180) {
  if (value == null) return '—'
  try {
    const text = typeof value === 'string' ? value : JSON.stringify(value, null, 2)
    if (!text) return '—'
    return text.length > maxLength ? `${text.slice(0, maxLength)}…` : text
  } catch {
    return '—'
  }
}

function ScannerPresetButton({ preset, watched }: { preset: ScannerPreset; watched: boolean }) {
  return (
    <div className="border border-border bg-muted/30 p-3 text-sm">
      <div className="flex items-start justify-between gap-2">
        <div>
          <div className="font-medium">{preset.label}</div>
          <div className="text-xs text-muted-foreground">{preset.note}</div>
        </div>
        {watched ? <Badge variant="secondary">watched</Badge> : <Badge variant="outline">preset</Badge>}
      </div>
      <div className="mt-3 flex flex-wrap gap-2 text-xs text-muted-foreground">
        {preset.draft.slug ? <Badge variant="outline">slug</Badge> : null}
        {preset.draft.tokenId ? <Badge variant="outline">token</Badge> : null}
        {preset.draft.outcome ? <Badge variant="outline">outcome</Badge> : null}
        {preset.draft.probability ? <Badge variant="outline">p={preset.draft.probability}</Badge> : null}
        {preset.draft.bestBid ? <Badge variant="outline">bid={preset.draft.bestBid}</Badge> : null}
        {preset.draft.bestAsk ? <Badge variant="outline">ask={preset.draft.bestAsk}</Badge> : null}
      </div>
    </div>
  )
}

const POLYMARKET_SECTIONS = [
  { id: 'overview', label: 'Overview' },
  { id: 'intelligence', label: 'Intelligence' },
  { id: 'accounts', label: 'Accounts' },
  { id: 'watched-markets', label: 'Watched Markets' },
  { id: 'operations', label: 'Operations' },
  { id: 'discovery', label: 'Discovery' },
]

function PolymarketSectionNav() {
  return (
    <nav aria-label="Polymarket sections" className="hud-panel rounded-none px-3 py-2.5">
      <div className="flex flex-wrap gap-2">
        {POLYMARKET_SECTIONS.map((section) => (
          <a
            key={section.id}
            href={`#${section.id}`}
            className="inline-flex items-center border border-border bg-panel px-3 py-1.5 text-[11px] font-medium uppercase tracking-[0.14em] text-ink-dim transition-colors hover:border-border-strong hover:bg-panel-raised hover:text-ink focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-pulse"
          >
            {section.label}
          </a>
        ))}
      </div>
    </nav>
  )
}

function SectionHeading({ title, note }: { title: string; note?: string }) {
  return (
    <div className="flex flex-wrap items-end justify-between gap-2">
      <div>
        <h2 className="text-sm font-semibold uppercase tracking-[0.12em] text-ink">{title}</h2>
        {note ? <p className="mt-1 text-xs text-ink-dim">{note}</p> : null}
      </div>
    </div>
  )
}

function QuickLinkButton({ to, label }: { to: string; label: string }) {
  return (
    <Button asChild size="sm" variant="outline" className="justify-start">
      <Link to={to}>{label}</Link>
    </Button>
  )
}

function SignalCard({ signal }: { signal: StoredSignal }) {
  const metadata = signal.metadata
  const affectedStrategyIds = signal.affected_strategy_ids ?? []

  return (
    <article className="space-y-3 border border-border bg-panel p-4" data-testid={`polymarket-signal-${signal.id}`}>
      <div className="flex flex-wrap items-start justify-between gap-2">
        <div>
          <div className="text-xs uppercase tracking-[0.18em] text-muted-foreground">{signal.source}</div>
          <div className="mt-1 font-medium">{signal.title}</div>
        </div>
        <div className="flex flex-wrap gap-1.5">
          <Badge variant="outline">Urgency {signal.urgency}</Badge>
          <Badge variant="secondary">{affectedStrategyIds.length} strategies</Badge>
        </div>
      </div>

      {signal.summary ? (
        <div>
          <div className="text-[11px] uppercase tracking-[0.18em] text-muted-foreground">Summary</div>
          <p className="mt-1 text-sm text-foreground/90">{signal.summary}</p>
        </div>
      ) : null}

      {signal.body ? (
        <div>
          <div className="text-[11px] uppercase tracking-[0.18em] text-muted-foreground">Body</div>
          <p className="mt-1 whitespace-pre-wrap text-sm text-muted-foreground">{signal.body}</p>
        </div>
      ) : null}

      {signal.recommended_action ? (
        <div>
          <div className="text-[11px] uppercase tracking-[0.18em] text-muted-foreground">Recommended action</div>
          <p className="mt-1 text-sm">{signal.recommended_action}</p>
        </div>
      ) : null}

      <div className="flex flex-wrap items-center gap-2 text-xs text-muted-foreground">
        <span>Received {safeDateLabel(signal.received_at)}</span>
        {signal.urgency >= 4 ? <Badge variant="destructive">high urgency</Badge> : signal.urgency >= 2 ? <Badge variant="warning">elevated</Badge> : <Badge variant="outline">monitor</Badge>}
      </div>

      {affectedStrategyIds.length > 0 ? (
        <div>
          <div className="text-[11px] uppercase tracking-[0.18em] text-muted-foreground">Related strategies</div>
          <div className="mt-1 flex flex-wrap gap-1.5">
            {affectedStrategyIds.map((strategyId) => (
              <Link key={strategyId} to={`/strategies/${strategyId}`} className="text-sm text-primary underline-offset-4 hover:underline">
                {strategyId.slice(0, 8)}
              </Link>
            ))}
          </div>
        </div>
      ) : null}

      <div className="border border-border bg-muted/30 p-2 text-xs">
        <div className="text-[11px] uppercase tracking-[0.18em] text-muted-foreground">Metadata preview</div>
        <pre className="mt-1 overflow-hidden whitespace-pre-wrap break-words font-mono text-[11px] text-muted-foreground">{safeJsonPreview(metadata)}</pre>
      </div>
    </article>
  )
}

const decisionStatusVariants: Record<TradeDecisionStatus, 'secondary' | 'outline' | 'success' | 'warning' | 'destructive'> = {
  candidate: 'outline',
  rejected: 'destructive',
  paper_ordered: 'warning',
  live_ordered: 'success',
  closed: 'secondary',
}

const riskStatusVariants: Record<TradeDecisionRiskStatus, 'success' | 'destructive'> = {
  approved: 'success',
  rejected: 'destructive',
}

function parseOptionalNumber(value: string) {
  const trimmed = value.trim()
  if (!trimmed) return undefined
  const parsed = Number(trimmed)
  return Number.isFinite(parsed) ? parsed : undefined
}

function ScannerOpportunityCard({ opportunity }: { opportunity: ResearchOpportunity }) {
  const decision = opportunity.decision
  const reasons = splitReasons(opportunity.reasons ?? decision.risk_reasons)

  return (
    <article className="border border-border bg-panel p-4">
      <div className="flex items-start justify-between gap-3">
        <div className="space-y-1">
          <div className="font-mono text-xs text-muted-foreground break-all">{decision.instrument_key || '—'}</div>
          <div className="flex flex-wrap gap-1.5">
            <Badge variant={riskStatusVariants[decision.risk_status] ?? 'secondary'}>{decision.risk_status}</Badge>
            <Badge variant={decisionStatusVariants[decision.status]}>{decision.status}</Badge>
            {opportunity.accepted == null ? null : <Badge variant={opportunity.accepted ? 'success' : 'destructive'}>{opportunity.accepted ? 'accepted' : 'rejected'}</Badge>}
          </div>
        </div>
        <div className={decision.net_ev > 0 ? 'text-right font-semibold text-emerald-500' : decision.net_ev < 0 ? 'text-right font-semibold text-destructive' : 'text-right font-semibold text-muted-foreground'}>
          {safeMoneyLabel(decision.net_ev)}
          <div className="mt-1 text-xs font-normal text-muted-foreground">Net EV</div>
        </div>
      </div>

      <div className="mt-4 grid gap-3 text-sm sm:grid-cols-2">
        <div>
          <div className="text-[11px] uppercase tracking-[0.18em] text-muted-foreground">Approved size</div>
          <div className="mt-1 font-medium">{safeNumberLabel(decision.approved_size, 0)}</div>
        </div>
        <div>
          <div className="text-[11px] uppercase tracking-[0.18em] text-muted-foreground">Risk status</div>
          <div className="mt-1 font-medium">{decision.risk_status}</div>
        </div>
        <div className="sm:col-span-2">
          <div className="text-[11px] uppercase tracking-[0.18em] text-muted-foreground">Risk reasons</div>
          <div className="mt-1 text-muted-foreground">{reasons.length > 0 ? reasons.join(' · ') : 'No risk reasons recorded'}</div>
        </div>
      </div>

      <div className="mt-4 flex items-center justify-between gap-3 text-xs text-muted-foreground">
        <span>Updated {safeDateLabel(decision.updated_at)}</span>
        <Link to="/journal" className="text-primary underline-offset-4 hover:underline">
          Journal
        </Link>
      </div>
    </article>
  )
}

export function PolymarketPage() {
  const qc = useQueryClient()
  const [slug, setSlug] = useState('')
  const [err, setErr] = useState('')
  const [trackedOnly, setTrackedOnly] = useState(true)
  const [minWinRate, setMinWinRate] = useState('')
  const [minResolved, setMinResolved] = useState('5')
  const [sort, setSort] = useState<AccountSort>('consistency_score')
  const [offset, setOffset] = useState(0)

  const [scannerSlugDraft, setScannerSlugDraft] = useState('')
  const [scannerTokenIdDraft, setScannerTokenIdDraft] = useState('')
  const [scannerOutcomeDraft, setScannerOutcomeDraft] = useState('')
  const [scannerProbabilityDraft, setScannerProbabilityDraft] = useState('')
  const [scannerBestBidDraft, setScannerBestBidDraft] = useState('')
  const [scannerBestAskDraft, setScannerBestAskDraft] = useState('')
  const [scannerAskDepthUsdDraft, setScannerAskDepthUsdDraft] = useState('')
  const [scannerAskSizeDraft, setScannerAskSizeDraft] = useState('')
  const [scannerStrategyIdDraft, setScannerStrategyIdDraft] = useState('')
  const [scannerMode, setScannerMode] = useState<ScannerMode | null>(null)
  const [scannerPresetLabel, setScannerPresetLabel] = useState('')

  const [scannerSlug, setScannerSlug] = useState('')
  const [scannerTokenId, setScannerTokenId] = useState('')
  const [scannerOutcome, setScannerOutcome] = useState('')
  const [scannerProbability, setScannerProbability] = useState<number | undefined>()
  const [scannerBestBid, setScannerBestBid] = useState<number | undefined>()
  const [scannerBestAsk, setScannerBestAsk] = useState<number | undefined>()
  const [scannerAskDepthUsd, setScannerAskDepthUsd] = useState<number | undefined>()
  const [scannerAskSize, setScannerAskSize] = useState<number | undefined>()
  const [scannerStrategyId, setScannerStrategyId] = useState('')

  const accounts = usePolymarketAccounts({
    tracked: trackedOnly || undefined,
    min_win_rate: minWinRate ? Number(minWinRate) : undefined,
    min_resolved: minResolved ? Number(minResolved) : undefined,
    sort,
    limit: 25,
    offset,
  })
  const watched = usePolymarketWatched()
  const jobs = usePolymarketJobsStatus()
  const trades = usePolymarketRecentTrades(50)
  const signals = usePolymarketRecentSignals({ limit: 25 })
  const tracked = usePolymarketAccounts({ tracked: true, limit: 1000 })
  const strategies = useQuery({
    queryKey: ['strategies', { market_type: 'polymarket' }],
    queryFn: () => apiClient.listStrategies({ market_type: 'polymarket' }),
  })
  const add = useAddPolymarketWatched()
  const remove = useRemovePolymarketWatched()
  const enable = useSetPolymarketWatchedEnabled()
  const track = useSetPolymarketAccountTracked()
  const discoveryLast = usePolymarketDiscoveryLast()
  const runDiscovery = useRunPolymarketDiscovery()

  const resetScanner = () => {
    setScannerSlugDraft('')
    setScannerTokenIdDraft('')
    setScannerOutcomeDraft('')
    setScannerProbabilityDraft('')
    setScannerBestBidDraft('')
    setScannerBestAskDraft('')
    setScannerAskDepthUsdDraft('')
    setScannerAskSizeDraft('')
    setScannerStrategyIdDraft('')
    setScannerSlug('')
    setScannerTokenId('')
    setScannerOutcome('')
    setScannerProbability(undefined)
    setScannerBestBid(undefined)
    setScannerBestAsk(undefined)
    setScannerAskDepthUsd(undefined)
    setScannerAskSize(undefined)
    setScannerStrategyId('')
    setScannerMode(null)
    setScannerPresetLabel('')
  }

  const commitScanner = () => {
    setScannerSlug(scannerSlugDraft.trim())
    setScannerTokenId(scannerTokenIdDraft.trim())
    setScannerOutcome(scannerOutcomeDraft.trim())
    setScannerProbability(parseOptionalNumber(scannerProbabilityDraft))
    setScannerBestBid(parseOptionalNumber(scannerBestBidDraft))
    setScannerBestAsk(parseOptionalNumber(scannerBestAskDraft))
    setScannerAskDepthUsd(parseOptionalNumber(scannerAskDepthUsdDraft))
    setScannerAskSize(parseOptionalNumber(scannerAskSizeDraft))
    setScannerStrategyId(scannerStrategyIdDraft.trim())
    setScannerMode(scannerPresetLabel ? 'preset' : 'manual')
  }

  const applyScannerPreset = (preset: ScannerPreset) => {
    setScannerSlugDraft(preset.draft.slug ?? '')
    setScannerTokenIdDraft(preset.draft.tokenId ?? '')
    setScannerOutcomeDraft(preset.draft.outcome ?? '')
    setScannerProbabilityDraft(preset.draft.probability ?? '')
    setScannerBestBidDraft(preset.draft.bestBid ?? '')
    setScannerBestAskDraft(preset.draft.bestAsk ?? '')
    setScannerAskDepthUsdDraft(preset.draft.askDepthUsd ?? '')
    setScannerAskSizeDraft(preset.draft.askSize ?? '')
    setScannerStrategyIdDraft(preset.draft.strategyId ?? '')
    setScannerMode('preset')
    setScannerPresetLabel(preset.label)
  }

  const hasScannerInputs = Boolean(
    scannerSlug ||
      scannerTokenId ||
      scannerOutcome ||
      scannerProbability != null ||
      scannerBestBid != null ||
      scannerBestAsk != null ||
      scannerAskDepthUsd != null ||
      scannerAskSize != null ||
      scannerStrategyId,
  )

  const scannerQuery = useQuery({
    queryKey: [
      'polymarket-opportunities',
      scannerSlug,
      scannerTokenId,
      scannerOutcome,
      scannerProbability,
      scannerBestBid,
      scannerBestAsk,
      scannerAskDepthUsd,
      scannerAskSize,
      scannerStrategyId,
    ],
    queryFn: () =>
      apiClient.listPolymarketOpportunities({
        slug: scannerSlug || undefined,
        token_id: scannerTokenId || undefined,
        outcome: scannerOutcome || undefined,
        probability: scannerProbability,
        best_bid: scannerBestBid,
        best_ask: scannerBestAsk,
        ask_depth_usd: scannerAskDepthUsd,
        ask_size: scannerAskSize,
        strategy_id: scannerStrategyId || undefined,
        limit: 12,
      }),
    enabled: hasScannerInputs,
  })

  const ws = useWebSocketClient({
    enabled: true,
    onMessage: (m) => {
      if ('type' in m && (m.type === 'polymarket_whale_trade' || m.type === 'polymarket_price_move' || m.type === 'polymarket_account_tracked')) {
        qc.invalidateQueries({ queryKey: ['polymarket-accounts'] })
        qc.invalidateQueries({ queryKey: ['polymarket-account'] })
        qc.invalidateQueries({ queryKey: ['polymarket-recent-trades'] })
        qc.invalidateQueries({ queryKey: ['polymarket-recent-signals'] })
        qc.invalidateQueries({ queryKey: ['polymarket-watched'] })
      }
    },
  })

  const { status: wsStatus, sendCommand } = ws
  useEffect(() => {
    if (wsStatus === 'open') sendCommand({ action: 'subscribe_polymarket' })
  }, [sendCommand, wsStatus])

  const totalVolume = useMemo(() => (tracked.data?.data ?? []).reduce((sum, account) => sum + account.total_volume, 0), [tracked.data])
  const displayedTrackedCount = accounts.data?.data?.length ?? 0
  const doAdd = async () => {
    setErr('')
    try {
      await add.mutateAsync({ slug: slug.trim() })
      setSlug('')
    } catch (error) {
      setErr(error instanceof Error ? error.message : 'Failed')
    }
  }
  const accountsData = accounts.data?.data ?? []
  const watchedData = watched.data?.data ?? []
  const tradesData = trades.data?.data ?? []
  const signalsData = signals.data?.data ?? []
  const jobsData = jobs.data ?? []
  const strategiesData = (strategies.data?.data ?? []).filter((strategy) => strategy.market_type === 'polymarket')
  const scannerOpps = scannerQuery.data?.data ?? []
  const watchedSlugSet = new Set(watchedData.map((market) => market.slug))
  const whaleTradesCount = tradesData.length
  const whaleSignalCount = signalsData.filter((signal) => signal.source === 'polymarket-whale').length
  const jobSummary = {
    total: jobsData.length,
    running: jobsData.filter((job) => job.running).length,
    enabled: jobsData.filter((job) => job.enabled).length,
    failing: jobsData.filter((job) => hasActiveJobFailure(job)).length,
  }

  const addSuggestedMarket = async (slugValue: string, note?: string) => {
    setErr('')
    setSlug(slugValue)
    if (watchedSlugSet.has(slugValue)) return
    try {
      await add.mutateAsync({ slug: slugValue, note })
      setSlug('')
    } catch (error) {
      setErr(error instanceof Error ? error.message : 'Failed')
    }
  }

  const scannerStatus = !hasScannerInputs
    ? 'Scanner not configured'
    : scannerQuery.isLoading
      ? 'Loading opportunities…'
      : scannerQuery.isError
        ? (scannerQuery.error as { status?: number } | null | undefined)?.status === 501
          ? 'Scanner not configured'
          : 'Unable to load opportunities'
        : scannerOpps.length === 0
          ? 'No opportunities met the paper-first filters.'
          : `${scannerOpps.length} opportunities`
  const scannerModeCopy = scannerMode === 'preset'
    ? `Preset draft: ${scannerPresetLabel}; submit to scan`
    : !hasScannerInputs
      ? 'No scan configured yet'
      : 'Manual scan from form inputs'

  return (
    <div className="space-y-5" data-testid="polymarket-page">
      <PageHeader
        eyebrow="Event Markets"
        title="Polymarket"
        description="Prediction market hub: intelligence, accounts, watched markets, operations, and discovery — grouped with the Kalshi data-first hub."
        meta={<StatusLed state={ws.status === 'open' ? 'live' : ws.status === 'connecting' ? 'sync' : 'warn'} label={scannerStatus} />}
      />

      <PolymarketSectionNav />

      <section id="overview" className="scroll-mt-24 space-y-4" data-testid="polymarket-overview-section">
        <SectionHeading title="Overview" note="Health, status, and fast routes into the Polymarket stack" />
        <div className="grid gap-4 xl:grid-cols-[1.5fr_1fr]">
          <ConsolePanel className="space-y-4 p-4">
            <HudSection label="Summary" note="Wallets, volume, strategies, and whale signals" />
            <div className="grid gap-3 md:grid-cols-4">
              <HudRow label="Displayed wallets" value={displayedTrackedCount} />
              <HudRow label="Total volume" value={money.format(totalVolume)} />
              <HudRow label="Open strategies" value={strategiesData.length} />
              <HudRow label="Whale signals" value={whaleSignalCount} />
            </div>
          </ConsolePanel>

          <Card>
            <CardHeader>
              <CardTitle>Quick Links</CardTitle>
            </CardHeader>
            <CardContent className="space-y-4">
              <div className="grid gap-2 sm:grid-cols-2">
                <QuickLinkButton to="/surfers/ops" label="Open Surfers Ops" />
                <QuickLinkButton to="/kalshi" label="Open Kalshi hub" />
                <QuickLinkButton to="/journal" label="Decision Journal" />
                <QuickLinkButton to="/strategies" label="Strategy Library" />
                <QuickLinkButton to="/discovery" label="Discovery Hub" />
              </div>
              <div className="grid gap-3 sm:grid-cols-2">
                <div className="rounded-none border border-border bg-muted/30 p-3 text-sm">
                  <div className="text-[11px] uppercase tracking-[0.18em] text-muted-foreground">Job health</div>
                  <div className="mt-1 font-medium">{jobSummary.total} jobs · {jobSummary.failing} failing</div>
                  <div className="text-xs text-muted-foreground">{jobSummary.enabled} enabled · {jobSummary.running} running</div>
                </div>
                <div className="rounded-none border border-border bg-muted/30 p-3 text-sm">
                  <div className="text-[11px] uppercase tracking-[0.18em] text-muted-foreground">Feed</div>
                  <div className="mt-1 font-medium">{ws.status}</div>
                  <div className="text-xs text-muted-foreground">Live websocket feed status for Polymarket events and whale trades.</div>
                </div>
              </div>
            </CardContent>
          </Card>
        </div>
      </section>

      <section id="intelligence" className="scroll-mt-24 space-y-4" data-testid="polymarket-intelligence-section">
        <SectionHeading title="Intelligence" note="Scanner output, signals, and whale trade flow" />
        <ConsolePanel className="space-y-4 p-4">
          <HudSection label="Research scanner" note="Paper-first market scanning and opportunity review" />
          <div className="flex flex-wrap items-center gap-2 text-xs text-muted-foreground">
            <HudBadge tone={hasScannerInputs ? 'confirm' : 'caution'}>{scannerStatus}</HudBadge>
            <span>{scannerModeCopy}</span>
            {scannerSlug ? <span>· {scannerSlug}</span> : null}
            {scannerTokenId ? <span>· token {scannerTokenId}</span> : null}
            {scannerOutcome ? <span>· {scannerOutcome}</span> : null}
          </div>

          <div className="grid gap-2 md:grid-cols-3">
            {SCANNER_PRESETS.map((preset) => (
              <button
                key={preset.id}
                type="button"
                className="text-left"
                onClick={() => applyScannerPreset(preset)}
                data-testid={`polymarket-scanner-preset-${preset.id}`}
              >
                <ScannerPresetButton preset={preset} watched={false} />
              </button>
            ))}
          </div>

          <div className="flex flex-wrap items-center gap-2">
            <Button type="button" variant="outline" size="sm" onClick={resetScanner} data-testid="polymarket-scanner-reset">
              Clear scanner
            </Button>
            <span className="text-xs text-muted-foreground">Preset chips seed the draft only; submit the form to run the scanner.</span>
          </div>

          <form
            className="grid gap-3 lg:grid-cols-2 xl:grid-cols-4"
            onSubmit={(event) => {
              event.preventDefault()
              commitScanner()
            }}
          >
            <Input value={scannerSlugDraft} onChange={(event) => setScannerSlugDraft(event.target.value)} placeholder="slug" aria-label="Market slug" />
            <Input value={scannerTokenIdDraft} onChange={(event) => setScannerTokenIdDraft(event.target.value)} placeholder="token id" aria-label="Token ID" />
            <Input value={scannerOutcomeDraft} onChange={(event) => setScannerOutcomeDraft(event.target.value)} placeholder="outcome" aria-label="Outcome" />
            <Input value={scannerStrategyIdDraft} onChange={(event) => setScannerStrategyIdDraft(event.target.value)} placeholder="strategy id" aria-label="Strategy ID" />
            <Input value={scannerProbabilityDraft} onChange={(event) => setScannerProbabilityDraft(event.target.value)} inputMode="decimal" placeholder="probability" aria-label="Probability" />
            <Input value={scannerBestBidDraft} onChange={(event) => setScannerBestBidDraft(event.target.value)} inputMode="decimal" placeholder="best bid" aria-label="Best bid" />
            <Input value={scannerBestAskDraft} onChange={(event) => setScannerBestAskDraft(event.target.value)} inputMode="decimal" placeholder="best ask" aria-label="Best ask" />
            <Input value={scannerAskDepthUsdDraft} onChange={(event) => setScannerAskDepthUsdDraft(event.target.value)} inputMode="decimal" placeholder="ask depth usd" aria-label="Ask depth USD" />
            <Input value={scannerAskSizeDraft} onChange={(event) => setScannerAskSizeDraft(event.target.value)} inputMode="decimal" placeholder="ask size" aria-label="Ask size" />
            <div className="flex items-end">
              <Button type="submit" className="w-full">Scan</Button>
            </div>
          </form>

          <div className="flex flex-wrap items-center gap-2 text-xs text-muted-foreground">
            {scannerProbability != null ? <span>· p={scannerProbability}</span> : null}
            {scannerBestBid != null ? <span>· bid={scannerBestBid}</span> : null}
            {scannerBestAsk != null ? <span>· ask={scannerBestAsk}</span> : null}
            {scannerAskDepthUsd != null ? <span>· depth=${scannerAskDepthUsd}</span> : null}
            {scannerAskSize != null ? <span>· size={scannerAskSize}</span> : null}
          </div>

          {!hasScannerInputs ? (
            <div className="flex flex-col items-center gap-2 py-8 text-center">
              <Search className="size-8 text-muted-foreground" />
              <p className="text-sm text-muted-foreground">Scanner not configured.</p>
            </div>
          ) : scannerQuery.isLoading ? (
            <div className="space-y-3" data-testid="polymarket-opportunities-loading">
              {Array.from({ length: 3 }).map((_, index) => (
                <div key={index} className="h-20 animate-pulse rounded-none border border-border bg-muted/40" />
              ))}
            </div>
          ) : scannerQuery.isError ? (
            (scannerQuery.error as { status?: number } | null | undefined)?.status === 501 ? (
              <div className="flex flex-col items-center gap-2 py-8 text-center">
                <Search className="size-8 text-muted-foreground" />
                <p className="text-sm text-muted-foreground">Scanner not configured.</p>
              </div>
            ) : (
              <div className="space-y-3 text-sm" data-testid="polymarket-opportunities-error">
                <p className="text-muted-foreground">Unable to load opportunities.</p>
                <Button type="button" variant="outline" size="sm" onClick={() => void scannerQuery.refetch()}>
                  Retry
                </Button>
              </div>
            )
          ) : scannerOpps.length === 0 ? (
            <div className="flex flex-col items-center gap-2 py-8 text-center" data-testid="polymarket-opportunities-empty">
              <Search className="size-8 text-muted-foreground" />
              <p className="text-sm text-muted-foreground">No opportunities met the paper-first filters.</p>
            </div>
          ) : (
            <>
              <div className="hidden overflow-hidden border border-border md:block">
                <table className="w-full text-left text-sm">
                  <thead>
                    <tr className="border-b border-border bg-muted/40 font-mono text-[11px] uppercase tracking-[0.16em] text-muted-foreground">
                      <th className="px-3 py-2 font-medium">Instrument</th>
                      <th className="px-3 py-2 font-medium">Net EV</th>
                      <th className="px-3 py-2 font-medium">Approved size</th>
                      <th className="px-3 py-2 font-medium">Risk / status</th>
                      <th className="px-3 py-2 font-medium">Risk reasons</th>
                      <th className="px-3 py-2 font-medium">Journal</th>
                    </tr>
                  </thead>
                  <tbody>
                    {scannerOpps.map((opportunity) => (
                      <tr key={opportunity.decision.id} className="border-b border-border last:border-0">
                        <td className="px-3 py-3 align-top font-mono text-xs text-muted-foreground break-all">
                          <div className="space-y-1">
                            <div>{opportunity.decision.instrument_key || '—'}</div>
                            {opportunity.decision.strategy_id ? (
                              <Link to={`/strategies/${opportunity.decision.strategy_id}`} className="text-[11px] text-primary underline-offset-4 hover:underline">
                                Strategy
                              </Link>
                            ) : null}
                          </div>
                        </td>
                        <td className={opportunity.decision.net_ev > 0 ? 'px-3 py-3 align-top font-semibold text-emerald-500' : opportunity.decision.net_ev < 0 ? 'px-3 py-3 align-top font-semibold text-destructive' : 'px-3 py-3 align-top font-semibold text-muted-foreground'}>
                          {safeMoneyLabel(opportunity.decision.net_ev)}
                        </td>
                        <td className="px-3 py-3 align-top">{safeNumberLabel(opportunity.decision.approved_size, 0)}</td>
                        <td className="px-3 py-3 align-top">
                          <div className="flex flex-wrap gap-1.5">
                            <Badge variant={riskStatusVariants[opportunity.decision.risk_status] ?? 'secondary'}>{opportunity.decision.risk_status}</Badge>
                            <Badge variant={decisionStatusVariants[opportunity.decision.status]}>{opportunity.decision.status}</Badge>
                            {opportunity.accepted == null ? null : <Badge variant={opportunity.accepted ? 'success' : 'destructive'}>{opportunity.accepted ? 'accepted' : 'rejected'}</Badge>}
                          </div>
                        </td>
                        <td className="px-3 py-3 align-top text-muted-foreground">
                          {splitReasons(opportunity.reasons ?? opportunity.decision.risk_reasons).join(' · ') || 'No risk reasons recorded'}
                        </td>
                        <td className="px-3 py-3 align-top text-sm">
                          <Link to="/journal" className="text-primary underline-offset-4 hover:underline">
                            Journal
                          </Link>
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>

              <div className="space-y-3 md:hidden">
                {scannerOpps.map((opportunity) => (
                  <ScannerOpportunityCard key={opportunity.decision.id} opportunity={opportunity} />
                ))}
              </div>
            </>
          )}
        </ConsolePanel>

        <div className="grid gap-4 xl:grid-cols-2">
          <Card>
            <CardHeader>
              <CardTitle>Recent Polymarket Signals</CardTitle>
            </CardHeader>
            <CardContent>
              {signalsData.length === 0 ? (
                <p className="text-sm text-muted-foreground">No recent signals yet.</p>
              ) : (
                <div className="space-y-3">
                  {signalsData.map((signal) => (
                    <SignalCard key={signal.id} signal={signal} />
                  ))}
                </div>
              )}
            </CardContent>
          </Card>

          <Card>
            <CardHeader>
              <CardTitle>Recent Whale Trades</CardTitle>
            </CardHeader>
            <CardContent>
              <div className="mb-2 flex items-center gap-2">
                <span className={ws.status === 'open' ? 'size-2 rounded-full bg-emerald-400' : 'size-2 rounded-full bg-zinc-500'} />
                {ws.status}
              </div>
              {whaleTradesCount === 0 ? (
                <p className="text-sm text-muted-foreground">No whale trades recorded yet. When they arrive, they will appear here.</p>
              ) : (
                <table className="w-full text-sm">
                  <tbody>
                    {tradesData.map((trade) => (
                      <tr key={trade.id}>
                        <td>{formatRelativeTime(trade.timestamp)}</td>
                        <td>
                          <div className="flex flex-col">
                            <Link to={`/polymarket/accounts/${trade.account_address}`}>{shortAddress(trade.account_address)}</Link>
                            <a className="text-xs text-muted-foreground" href={profileUrl(trade.account_address)} target="_blank" rel="noreferrer">profile</a>
                          </div>
                        </td>
                        <td>
                          <div className="flex flex-col">
                            <a href={marketUrl(trade.market_slug)} target="_blank" rel="noreferrer">{trade.market_slug}</a>
                            <a className="text-xs text-muted-foreground" href={eventUrl(trade.market_slug)} target="_blank" rel="noreferrer">event</a>
                          </div>
                        </td>
                        <td><Badge variant={String(trade.side).toUpperCase() === 'YES' ? 'success' : 'destructive'}>{trade.side}</Badge></td>
                        <td>{money2.format(trade.size_usdc)}</td>
                        <td>{trade.price.toFixed(3)}</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              )}
            </CardContent>
          </Card>
        </div>
      </section>

      <section id="accounts" className="scroll-mt-24 space-y-4" data-testid="polymarket-accounts-section">
        <SectionHeading title="Accounts" note="Tracked wallets and direct links to account detail pages" />
        <Card>
          <CardHeader>
            <CardTitle>Tracked Wallets</CardTitle>
          </CardHeader>
          <CardContent>
            <div className="mb-3 flex flex-wrap gap-2">
              <label>
                <input type="checkbox" checked={trackedOnly} onChange={(e) => setTrackedOnly(e.target.checked)} /> tracked-only
              </label>
              <Input type="number" step="0.01" min="0" max="1" value={minWinRate} onChange={(e) => setMinWinRate(e.target.value)} placeholder="min raw win rate" />
              <Input type="number" step="1" min="0" value={minResolved} onChange={(e) => setMinResolved(e.target.value)} placeholder="min resolved" />
              <select value={sort} onChange={(e) => setSort(e.target.value as AccountSort)}>
                <option value="consistency_score">consistency</option>
                <option value="bayesian_win_rate">bayesian win rate</option>
                <option value="resolved_markets">resolved markets</option>
                <option value="win_rate">raw win rate</option>
                <option value="volume">volume</option>
                <option value="last_active">last active</option>
                <option value="trades">trades</option>
              </select>
              <span className="text-xs text-muted-foreground">score = adjusted win rate × sample size</span>
            </div>
            <p className="mb-2 text-xs text-muted-foreground">Tags are ingestion-provided; there is no tag editor in this view.</p>
            {accountsData.length === 0 ? (
              <p className="text-sm text-muted-foreground">No wallets match the current filters yet.</p>
            ) : (
              <table className="w-full text-sm">
                <thead>
                  <tr>
                    <th>address</th><th>score</th><th>adj win</th><th>raw win</th><th>won/lost</th><th>volume</th><th>max_position</th><th>last_active</th><th>tracked</th><th>tags</th>
                  </tr>
                </thead>
                <tbody>
                  {accountsData.map((account) => (
                    <tr key={account.address}>
                      <td>
                        <div className="flex flex-col">
                          <Link to={`/polymarket/accounts/${account.address}`}>{shortAddress(account.address)}</Link>
                          <a className="text-xs text-muted-foreground" href={profileUrl(account.address)} target="_blank" rel="noreferrer">profile</a>
                        </div>
                      </td>
                      <td>{(account.consistency_score ?? 0).toFixed(3)}</td>
                      <td>{((account.bayesian_win_rate ?? 0) * 100).toFixed(1)}%</td>
                      <td>{(account.win_rate * 100).toFixed(1)}%</td>
                      <td>{`${account.markets_won}/${account.markets_lost} (${account.resolved_markets ?? account.markets_won + account.markets_lost})`}</td>
                      <td>{money.format(account.total_volume)}</td>
                      <td>{money.format(account.max_position)}</td>
                      <td>{formatRelativeTime(account.last_active)}</td>
                      <td>
                        <input
                          type="checkbox"
                          checked={account.tracked}
                          onChange={(e) =>
                            track.mutate(
                              { address: account.address, tracked: e.target.checked },
                              { onSuccess: () => qc.invalidateQueries({ queryKey: ['polymarket-account', account.address] }) },
                            )
                          }
                        />
                      </td>
                      <td>
                        <div className="flex flex-wrap gap-1.5">
                          {account.tags?.length ? account.tags.map((tag) => <Badge key={tag} variant="outline">{tag}</Badge>) : <span className="text-muted-foreground">No tags</span>}
                        </div>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            )}
            <div className="mt-3 flex gap-2">
              <Button
                size="sm"
                variant="outline"
                disabled={offset === 0}
                onClick={() => setOffset((current) => Math.max(0, current - 25))}
              >
                Prev
              </Button>
              <Button
                size="sm"
                variant="outline"
                disabled={accountsData.length < 25}
                onClick={() => setOffset((current) => current + 25)}
              >
                Next
              </Button>
            </div>
          </CardContent>
        </Card>
      </section>

      <section id="watched-markets" className="scroll-mt-24 space-y-4" data-testid="polymarket-watched-section">
        <SectionHeading title="Watched Markets" note="Watchlist entries, suggestions, and external market links" />
        <Card>
          <CardHeader>
            <CardTitle>Watched Markets</CardTitle>
          </CardHeader>
          <CardContent>
            <div className="mb-4 grid gap-3 md:grid-cols-3">
              {WATCHED_MARKET_SUGGESTIONS.map((suggestion) => {
                const alreadyWatched = watchedSlugSet.has(suggestion.slug)
                return (
                  <button
                    key={suggestion.id}
                    type="button"
                    className="text-left"
                    data-testid={`polymarket-watched-suggestion-${suggestion.id}`}
                    onClick={() => void addSuggestedMarket(suggestion.slug, suggestion.note)}
                  >
                    <div className="border border-border bg-muted/30 p-3 text-sm">
                      <div className="flex items-start justify-between gap-2">
                        <div>
                          <div className="font-medium">{suggestion.slug}</div>
                          <div className="text-xs text-muted-foreground">{suggestion.note}</div>
                        </div>
                        <Badge variant={alreadyWatched ? 'secondary' : 'outline'}>{alreadyWatched ? 'watched' : 'suggested'}</Badge>
                      </div>
                    </div>
                  </button>
                )
              })}
            </div>
            <div className="mb-3 flex gap-2">
              <Input value={slug} onChange={(e) => setSlug(e.target.value)} placeholder="market slug" />
              <Button onClick={doAdd}>Add</Button>
            </div>
            {err ? <p className="text-sm text-destructive">{err}</p> : null}
            <p className="mb-2 text-xs text-muted-foreground">Watched market tags/note fields are ingestion-provided.</p>
            {watchedData.length === 0 ? (
              <p className="text-sm text-muted-foreground" data-testid="polymarket-watched-empty">No watched markets yet. Use a suggestion above or add a slug manually.</p>
            ) : (
              <table className="w-full text-sm">
                <tbody>
                  {watchedData.map((market) => (
                    <tr key={market.slug}>
                      <td>
                        <div className="flex flex-col">
                          <a href={marketUrl(market.slug)} target="_blank" rel="noreferrer">{market.slug}</a>
                          <a className="text-xs text-muted-foreground" href={eventUrl(market.slug)} target="_blank" rel="noreferrer">event</a>
                        </div>
                      </td>
                      <td><input type="checkbox" checked={market.enabled} onChange={(e) => enable.mutate({ slug: market.slug, enabled: e.target.checked })} /></td>
                      <td>{formatRelativeTime(market.added_at)}</td>
                      <td>{market.note ?? '--'}</td>
                      <td><Button size="sm" variant="outline" onClick={() => remove.mutate(market.slug)}>Remove</Button></td>
                    </tr>
                  ))}
                </tbody>
              </table>
            )}
          </CardContent>
        </Card>
      </section>

      <section id="operations" className="scroll-mt-24 space-y-4" data-testid="polymarket-operations-section">
        <SectionHeading title="Operations" note="Feed health, job health, and the Polymarket ops console" />
        <div className="grid gap-4 xl:grid-cols-[1fr_1.5fr]">
          <Card>
            <CardHeader>
              <CardTitle>Feed Status</CardTitle>
            </CardHeader>
            <CardContent className="space-y-3 text-sm">
              <div className="flex items-center gap-2">
                <StatusLed state={ws.status === 'open' ? 'ok' : ws.status === 'connecting' ? 'warn' : 'dead'} label={ws.status} />
                <Badge variant={ws.status === 'open' ? 'success' : 'outline'}>{ws.status}</Badge>
              </div>
              <p className="text-muted-foreground">Cross-check the live Polymarket feed and breaker status in the dedicated ops console.</p>
              <div className="flex flex-wrap gap-2">
                <QuickLinkButton to="/surfers/ops" label="Open Surfers Ops" />
                <QuickLinkButton to="/journal" label="Decision Journal" />
              </div>
            </CardContent>
          </Card>

          <Card>
            <CardHeader>
              <CardTitle>Polymarket Jobs</CardTitle>
            </CardHeader>
            <CardContent>
              {jobsData.length === 0 ? (
                <p className="text-sm text-muted-foreground">No Polymarket jobs are configured yet.</p>
              ) : (
                <div className="grid gap-3 md:grid-cols-2 xl:grid-cols-3">
                  {jobsData.map((job) => {
                    const hasActiveFailure = hasActiveJobFailure(job)
                    const hasHistoricalFailures = hasHistoricalJobFailures(job)
                    const stateTone: 'success' | 'outline' | 'warning' | 'secondary' = job.running ? 'success' : !job.enabled ? 'outline' : hasActiveFailure ? 'warning' : 'secondary'
                    return (
                      <article key={job.name} className="space-y-2 border border-border bg-panel p-3 text-sm" data-testid={`polymarket-job-${job.name}`}>
                        <div className="flex items-start justify-between gap-2">
                          <div>
                            <div className="font-medium">{job.name}</div>
                            <div className="text-xs text-muted-foreground">{job.description}</div>
                          </div>
                          <div className="flex flex-wrap justify-end gap-1.5">
                            <Badge variant={stateTone}>{job.running ? 'running' : job.enabled ? 'enabled' : 'disabled'}</Badge>
                            {hasActiveFailure ? <Badge variant="warning">active failure</Badge> : <Badge variant="secondary">stable</Badge>}
                          </div>
                        </div>

                        <div className="text-xs text-muted-foreground">
                          <div>{formatScheduleDisplay(job.schedule)}</div>
                          <div className="font-mono text-[11px]">Backend schedule description</div>
                        </div>

                        <div className="grid grid-cols-2 gap-2 text-xs">
                          <div><span className="text-muted-foreground">Run count</span><div className="font-medium">{job.run_count}</div></div>
                          <div><span className="text-muted-foreground">Error count</span><div className="font-medium">{job.error_count}</div></div>
                          {hasHistoricalFailures && !hasActiveFailure ? <div className="col-span-2 text-muted-foreground">Historical errors: {job.error_count}. Latest state is not failing.</div> : null}
                          <div className="col-span-2"><span className="text-muted-foreground">Last run</span><div className="font-medium">{job.last_run ? `${safeDateLabel(job.last_run)} · ${formatRelativeTime(job.last_run)}` : 'Never run'}</div></div>
                          <div className="col-span-2"><span className="text-muted-foreground">Last result</span><div className="font-medium">{job.last_result || '—'}</div></div>
                          {job.last_error ? <div className="col-span-2 text-destructive"><span className="text-muted-foreground">Last error</span><div className="font-medium">{job.last_error}</div></div> : null}
                        </div>

                        <Button size="sm" variant="outline" onClick={() => { void apiClient.runAutomationJob(job.name).then(() => qc.invalidateQueries({ queryKey: ['polymarket-jobs'] })) }}>
                          Run now
                        </Button>
                      </article>
                    )
                  })}
                </div>
              )}
            </CardContent>
          </Card>
        </div>
      </section>

      <section id="discovery" className="scroll-mt-24 space-y-4" data-testid="polymarket-discovery-section">
        <SectionHeading title="Discovery" note="Generated strategies and the latest discovery run output" />
        <div className="grid gap-4 xl:grid-cols-[1fr_1.2fr]">
          <Card data-testid="polymarket-discovery">
            <CardHeader>
              <CardTitle>Auto-generated Strategies</CardTitle>
            </CardHeader>
            <CardContent>
              <div className="mb-3 flex flex-wrap items-center gap-2">
                <Button size="sm" disabled={runDiscovery.isPending} onClick={() => runDiscovery.mutate()}>
                  {runDiscovery.isPending ? 'Running…' : 'Run discovery now'}
                </Button>
                <QuickLinkButton to="/discovery" label="Open discovery hub" />
              </div>
              <span className="text-xs text-muted-foreground">Cron: every 6 hours. Generates paper strategies from open Polymarket markets.</span>
              {runDiscovery.isError ? <p className="mb-2 mt-3 text-sm text-destructive">{(runDiscovery.error as Error)?.message ?? 'Failed to start discovery'}</p> : null}
              {(() => {
                const last = discoveryLast.data?.last
                if (!last) return <p className="mt-3 text-sm text-muted-foreground">No discovery run yet.</p>
                const deployed = last.deployed ?? []
                return (
                  <div className="mt-3 space-y-2">
                    <div className="text-xs text-muted-foreground">
                      Last run {formatRelativeTime(last.started_at)} · fetched {last.fetched_all} · screened {last.screened} · proposed {last.proposed} · skipped {last.skipped} · deployed {deployed.length}{last.dry_run ? ' (dry run)' : ''}
                    </div>
                    {deployed.length === 0 ? (
                      <p className="text-sm">No strategies deployed in the last run.</p>
                    ) : (
                      <table className="w-full text-sm">
                        <thead>
                          <tr>
                            <th className="text-left">name</th><th className="text-left">slug</th><th className="text-left">template</th><th className="text-left">side</th><th className="text-left">conviction</th><th />
                          </tr>
                        </thead>
                        <tbody>
                          {deployed.map((deployedStrategy) => (
                            <tr key={deployedStrategy.strategy_id}>
                              <td><Link to={`/strategies/${deployedStrategy.strategy_id}`}>{deployedStrategy.name}</Link></td>
                              <td><a href={marketUrl(deployedStrategy.slug)} target="_blank" rel="noreferrer">{deployedStrategy.slug}</a></td>
                              <td><Badge variant="secondary">{deployedStrategy.template}</Badge></td>
                              <td><Badge variant={deployedStrategy.direction === 'YES' ? 'success' : 'destructive'}>{deployedStrategy.direction}</Badge></td>
                              <td>{(deployedStrategy.conviction * 100).toFixed(0)}%</td>
                              <td>{deployedStrategy.reused ? <Badge variant="secondary">reused</Badge> : <Badge variant="success">new</Badge>}</td>
                            </tr>
                          ))}
                        </tbody>
                      </table>
                    )}
                    {last.errors && last.errors.length > 0 ? (
                      <details className="text-xs">
                        <summary>{last.errors.length} errors</summary>
                        <ul className="mt-1 list-disc pl-4">
                          {last.errors.map((error, index) => (
                            <li key={index}>{error}</li>
                          ))}
                        </ul>
                      </details>
                    ) : null}
                  </div>
                )
              })()}
            </CardContent>
          </Card>

          <Card>
            <CardHeader>
              <CardTitle>Polymarket Strategies</CardTitle>
            </CardHeader>
            <CardContent>
              {strategiesData.length === 0 ? (
                <p className="text-sm text-muted-foreground">No Polymarket strategies yet.</p>
              ) : (
                <div className="space-y-2">
                  {strategiesData.map((strategy) => (
                    <div key={strategy.id} className="flex items-center justify-between gap-2 border border-border bg-muted/30 px-3 py-2 text-sm">
                      <div className="flex flex-col">
                        <Link to={`/strategies/${strategy.id}`}>{strategy.name}</Link>
                        <span className="text-xs text-muted-foreground">{strategy.ticker}</span>
                      </div>
                      <Badge variant="outline">{strategy.market_type}</Badge>
                    </div>
                  ))}
                </div>
              )}
            </CardContent>
          </Card>
        </div>
      </section>
    </div>
  )
}
