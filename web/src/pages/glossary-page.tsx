import { BookOpen, Search } from 'lucide-react'
import { useMemo, useState } from 'react'

import { PageHeader } from '@/components/layout/page-header'
import { Badge } from '@/components/ui/badge'
import { Input } from '@/components/ui/input'

// ---- term data ----

export interface GlossaryTerm {
  term: string
  slug: string
  category: string
  definition: string
}

export const GLOSSARY_TERMS: GlossaryTerm[] = [
  {
    term: 'AMC',
    slug: 'amc',
    category: 'earnings',
    definition:
      'After Market Close — an earnings report released after the trading day ends. Prices may gap at the next open.',
  },
  {
    term: 'Automation',
    slug: 'automation',
    category: 'system',
    definition:
      'Scheduled or event-driven workflows that run strategies, send alerts, or perform maintenance tasks without manual intervention.',
  },
  {
    term: 'Backtest',
    slug: 'backtest',
    category: 'strategy',
    definition:
      'A simulated test of a strategy against historical price data to evaluate how it would have performed in the past.',
  },
  {
    term: 'BMO',
    slug: 'bmo',
    category: 'earnings',
    definition:
      'Before Market Open — an earnings report released before the trading day starts. Prices may gap at the open.',
  },
  {
    term: 'Circuit Breaker',
    slug: 'circuit-breaker',
    category: 'risk',
    definition:
      'An automated control that pauses trading when specific risk thresholds are breached, preventing further losses until manually reviewed.',
  },
  {
    term: 'Discovery',
    slug: 'discovery',
    category: 'system',
    definition:
      'An AI-powered scan that analyzes the universe of tracked securities and surfaces the most promising opportunities based on configured criteria.',
  },
  {
    term: 'EPS',
    slug: 'eps',
    category: 'earnings',
    definition:
      "Earnings Per Share — a company's net profit divided by the number of outstanding shares. A key metric for comparing earnings reports against analyst estimates.",
  },
  {
    term: 'Fill Price',
    slug: 'fill-price',
    category: 'orders',
    definition:
      'The actual price at which an order was executed, which may differ from the limit or market price requested.',
  },
  {
    term: 'Gap',
    slug: 'gap',
    category: 'price',
    definition:
      "A price difference between a stock's prior close and the next open, caused by overnight news or earnings releases.",
  },
  {
    term: 'Kill Switch',
    slug: 'kill-switch',
    category: 'risk',
    definition:
      'An emergency control that immediately halts all trading activity system-wide. Requires manual reactivation to resume.',
  },
  {
    term: 'Long',
    slug: 'long',
    category: 'positions',
    definition:
      'A position that profits when the underlying security rises in price. The standard direction for most equity trades.',
  },
  {
    term: 'Memory',
    slug: 'memory',
    category: 'system',
    definition:
      'Stored knowledge — facts, summaries, and decisions — that agents reference across sessions to inform future analysis.',
  },
  {
    term: 'Options Chain',
    slug: 'options-chain',
    category: 'options',
    definition:
      'A listing of all available option contracts (calls and puts) for a given security, organized by expiry date and strike price.',
  },
  {
    term: 'P&L',
    slug: 'pnl',
    category: 'positions',
    definition:
      'Profit and Loss — the difference between realized or unrealized revenue and the cost of entering a position. Displayed as both a dollar amount and percentage.',
  },
  {
    term: 'Paper Trading',
    slug: 'paper-trading',
    category: 'strategy',
    definition:
      'Simulated trading with no real money at risk. Used to validate strategy behavior before live deployment.',
  },
  {
    term: 'Pipeline Run',
    slug: 'pipeline-run',
    category: 'system',
    definition:
      "A single execution of a strategy's analysis pipeline, encompassing all agent phases from data ingestion through final signal generation.",
  },
  {
    term: 'Position',
    slug: 'position',
    category: 'positions',
    definition:
      'An open trade holding shares or contracts. Shows current price, unrealized P&L, and stop/take-profit levels.',
  },
  {
    term: 'Realtime',
    slug: 'realtime',
    category: 'system',
    definition:
      'A live event feed that streams pipeline runs and agent activity as they happen via WebSocket, with conversation context for each event.',
  },
  {
    term: 'Reliability',
    slug: 'reliability',
    category: 'system',
    definition:
      'System health metrics tracking uptime, latency percentiles, and error rates across pipeline components.',
  },
  {
    term: 'SEC Filing',
    slug: 'sec-filing',
    category: 'fundamentals',
    definition:
      'Documents required by the Securities and Exchange Commission, including 10-K annual reports, 10-Q quarterly reports, and 8-K material event disclosures. Can be analyzed by the AI pipeline for trading signals.',
  },
  {
    term: 'Sentiment',
    slug: 'sentiment',
    category: 'signals',
    definition:
      'An analyst assessment of whether a signal or filing is bullish (positive), bearish (negative), or neutral. Used to classify pipeline outputs.',
  },
  {
    term: 'Short',
    slug: 'short',
    category: 'positions',
    definition:
      'A position that profits when the underlying security falls in price. Involves borrowing shares to sell, then buying them back at a lower price.',
  },
  {
    term: 'Signal',
    slug: 'signal',
    category: 'signals',
    definition:
      'A trading recommendation generated at the end of a pipeline run: buy, sell, or hold. Accompanied by confidence score and agent reasoning.',
  },
  {
    term: 'Stop Loss',
    slug: 'stop-loss',
    category: 'risk',
    definition:
      'A price level at which a losing position is automatically closed to cap downside. Set per-position based on strategy risk parameters.',
  },
  {
    term: 'Strategy',
    slug: 'strategy',
    category: 'strategy',
    definition:
      'A configured set of agents, tickers, schedule, and risk parameters that define how the system analyzes and trades a security.',
  },
  {
    term: 'Take Profit',
    slug: 'take-profit',
    category: 'risk',
    definition:
      'A price level at which a profitable position is automatically closed to lock in gains. Works alongside stop loss for symmetric risk management.',
  },
  {
    term: 'Ticker',
    slug: 'ticker',
    category: 'price',
    definition:
      'A unique symbol identifying a publicly traded security (e.g., AAPL for Apple Inc.). Used across the system to reference stocks, ETFs, and other instruments.',
  },
  {
    term: 'Universe',
    slug: 'universe',
    category: 'system',
    definition:
      'The full set of securities tracked and scored by the system. Securities are ranked by watch score to surface the most relevant candidates.',
  },
  {
    term: 'Watch Score',
    slug: 'watch-score',
    category: 'signals',
    definition:
      'A composite ranking score indicating how much attention a ticker deserves. Higher scores reflect stronger technical and fundamental signals.',
  },
  {
    term: 'Watchlist',
    slug: 'watchlist',
    category: 'system',
    definition:
      'The active subset of the universe currently under close monitoring. Populated by the Discovery scan and scored tickers above configured thresholds.',
  },
]

// ---- category labels ----

const CATEGORY_LABELS: Record<string, string> = {
  earnings: 'Earnings',
  fundamentals: 'Fundamentals',
  options: 'Options',
  orders: 'Orders',
  positions: 'Positions',
  price: 'Price',
  risk: 'Risk',
  signals: 'Signals',
  strategy: 'Strategy',
  system: 'System',
}

// ---- component ----

export function GlossaryPage() {
  const [query, setQuery] = useState('')

  const filtered = useMemo(() => {
    const q = query.toLowerCase().trim()
    if (!q) return GLOSSARY_TERMS
    return GLOSSARY_TERMS.filter(
      (t) =>
        t.term.toLowerCase().includes(q) ||
        t.definition.toLowerCase().includes(q) ||
        t.category.toLowerCase().includes(q),
    )
  }, [query])

  // group by first letter
  const groups = useMemo(() => {
    const map = new Map<string, GlossaryTerm[]>()
    for (const term of filtered) {
      const letter = term.term[0].toUpperCase()
      if (!map.has(letter)) map.set(letter, [])
      map.get(letter)!.push(term)
    }
    return Array.from(map.entries()).sort(([a], [b]) => a.localeCompare(b))
  }, [filtered])

  return (
    <div className="space-y-4" data-testid="glossary-page">
      <PageHeader
        eyebrow="Reference"
        title="Glossary"
        description="Definitions for terms used throughout Augr"
      />

      {/* Search */}
      <div className="relative max-w-sm">
        <Search className="pointer-events-none absolute left-2.5 top-1/2 size-3.5 -translate-y-1/2 text-muted-foreground" />
        <Input
          value={query}
          onChange={(e) => setQuery(e.target.value)}
          placeholder="Filter terms…"
          className="pl-8 text-sm"
          data-testid="glossary-search"
        />
      </div>

      {/* Results */}
      {groups.length === 0 ? (
        <p className="text-sm text-muted-foreground" data-testid="glossary-empty">
          No terms match "{query}".
        </p>
      ) : (
        <div className="space-y-8">
          {groups.map(([letter, terms]) => (
            <section key={letter} aria-labelledby={`glossary-letter-${letter}`}>
              <h2
                id={`glossary-letter-${letter}`}
                className="mb-3 font-mono text-[11px] font-medium uppercase tracking-[0.18em] text-muted-foreground"
              >
                {letter}
              </h2>
              <div className="space-y-3">
                {terms.map((t) => (
                  <div
                    key={t.slug}
                    id={`term-${t.slug}`}
                    className="rounded-lg border border-border bg-card p-4"
                    data-testid={`glossary-term-${t.slug}`}
                  >
                    <div className="mb-1.5 flex flex-wrap items-center gap-2">
                      <h3 className="font-semibold">
                        <a
                          href={`#term-${t.slug}`}
                          className="hover:text-primary"
                        >
                          {t.term}
                        </a>
                      </h3>
                      <Badge variant="secondary" className="text-[10px]">
                        {CATEGORY_LABELS[t.category] ?? t.category}
                      </Badge>
                    </div>
                    <p className="text-sm leading-relaxed text-muted-foreground">
                      {t.definition}
                    </p>
                  </div>
                ))}
              </div>
            </section>
          ))}
        </div>
      )}

      {/* Footer with BookOpen icon */}
      <p className="flex items-center gap-1.5 pt-2 text-xs text-muted-foreground">
        <BookOpen className="size-3.5" />
        {GLOSSARY_TERMS.length} terms defined
      </p>
    </div>
  )
}
