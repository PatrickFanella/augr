import { Link } from 'react-router-dom'

import { PageHeader } from '@/components/layout/page-header'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { ConsolePanel, HudBadge, HudRow, HudSection, StatusLed } from '@/components/ui/hud'

const kalshiSections = [
  { id: 'overview', label: 'Overview' },
  { id: 'markets', label: 'Markets' },
  { id: 'paper-strategies', label: 'Paper Strategies' },
  { id: 'operations', label: 'Operations' },
  { id: 'setup', label: 'Setup' },
]

function KalshiSectionNav() {
  return (
    <nav aria-label="Kalshi sections" className="hud-panel rounded-none px-3 py-2.5">
      <div className="flex flex-wrap gap-2">
        {kalshiSections.map((section) => (
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

export function KalshiPage() {
  return (
    <div className="space-y-5" data-testid="kalshi-page">
      <PageHeader
        eyebrow="Event Markets"
        title="Kalshi"
        description="Paper/data-first Kalshi hub. Market data support can land here now; live trade execution stays disabled until backend endpoints exist."
        meta={<StatusLed state="warn" label="paper/data only" />}
        actions={
          <div className="flex flex-wrap items-center gap-2">
            <Button size="sm" variant="outline" asChild>
              <Link to="/polymarket">Polymarket hub</Link>
            </Button>
            <Button size="sm" variant="outline" asChild>
              <Link to="/surfers/ops">Surfers Ops</Link>
            </Button>
            <Button size="sm" variant="outline" asChild>
              <a href="/docs/runbooks/kalshi-paper-data.md" target="_blank" rel="noreferrer">Runbook</a>
            </Button>
          </div>
        }
      />

      <KalshiSectionNav />

      <section id="overview" className="scroll-mt-24 space-y-4" data-testid="kalshi-overview-section">
        <SectionHeading title="Overview" note="Phase and guardrails for the Kalshi rollout" />
        <div className="grid gap-4 xl:grid-cols-[1.5fr_1fr]">
          <ConsolePanel className="space-y-4 p-4">
            <HudSection label="Current phase" note="Data-ready, execution off" />
            <div className="grid gap-3 md:grid-cols-3">
              <HudRow label="Live trading" value="Disabled" />
              <HudRow label="Back-end status" value="Paper/data support" />
              <HudRow label="UI status" value="Hub ready" />
            </div>
            <div className="flex flex-wrap gap-2 text-xs text-muted-foreground">
              <HudBadge tone="caution">No live submit</HudBadge>
              <HudBadge tone="confirm">Event Markets grouped</HudBadge>
            </div>
          </ConsolePanel>

          <Card>
            <CardHeader>
              <CardTitle>Quick Links</CardTitle>
            </CardHeader>
            <CardContent className="space-y-4">
              <div className="grid gap-2 sm:grid-cols-2">
                <QuickLinkButton to="/polymarket" label="Open Polymarket hub" />
                <QuickLinkButton to="/surfers/ops" label="Open Surfers Ops" />
              </div>
              <p className="text-sm text-ink-dim">Kalshi sits beside Polymarket in the same Event Markets area, but this page stays explicit about paper/data-only status.</p>
            </CardContent>
          </Card>
        </div>
      </section>

      <section id="markets" className="scroll-mt-24 space-y-4" data-testid="kalshi-markets-section">
        <SectionHeading title="Markets" note="What the UI can represent without claiming live execution" />
        <div className="grid gap-4 xl:grid-cols-3">
          <Card>
            <CardHeader>
              <CardTitle>Market data</CardTitle>
            </CardHeader>
            <CardContent className="text-sm text-ink-dim">
              Surface event metadata, contract structure, and pricing context as backend reads become available.
            </CardContent>
          </Card>
          <Card>
            <CardHeader>
              <CardTitle>Order book</CardTitle>
            </CardHeader>
            <CardContent className="text-sm text-ink-dim">
              Reserve space for quote depth and spread views, but keep the page honest when live endpoints are missing.
            </CardContent>
          </Card>
          <Card>
            <CardHeader>
              <CardTitle>Positions</CardTitle>
            </CardHeader>
            <CardContent className="text-sm text-ink-dim">
              Show balances and positions when supported; otherwise fall back to a clear data-unavailable state.
            </CardContent>
          </Card>
        </div>
      </section>

      <section id="paper-strategies" className="scroll-mt-24 space-y-4" data-testid="kalshi-paper-strategies-section">
        <SectionHeading title="Paper Strategies" note="Paper-first strategy work for Kalshi" />
        <Card>
          <CardHeader>
            <CardTitle>Paper strategy lane</CardTitle>
          </CardHeader>
          <CardContent className="space-y-3 text-sm text-ink-dim">
            <p>Use this area for the Kalshi paper workflow: research, sizing, and decision review without implying execution.</p>
            <div className="flex flex-wrap gap-2">
              <Badge variant="outline">paper only</Badge>
              <Badge variant="outline">data-first</Badge>
              <Badge variant="outline">live disabled</Badge>
            </div>
          </CardContent>
        </Card>
      </section>

      <section id="operations" className="scroll-mt-24 space-y-4" data-testid="kalshi-operations-section">
        <SectionHeading title="Operations" note="Where to look when the hub needs support" />
        <div className="grid gap-4 xl:grid-cols-[1fr_1.2fr]">
          <Card>
            <CardHeader>
              <CardTitle>Ops routing</CardTitle>
            </CardHeader>
            <CardContent className="space-y-3 text-sm">
              <p className="text-ink-dim">Keep operational troubleshooting in Surfers Ops for shared feed and breaker visibility.</p>
              <div className="flex flex-wrap gap-2">
                <QuickLinkButton to="/surfers/ops" label="Surfers Ops" />
                <QuickLinkButton to="/polymarket" label="Polymarket hub" />
              </div>
            </CardContent>
          </Card>

          <Card>
            <CardHeader>
              <CardTitle>Phase notes</CardTitle>
            </CardHeader>
            <CardContent className="space-y-3 text-sm text-ink-dim">
              <p>Kalshi demo and production stay separate. This UI should not imply live order submission until backend work lands.</p>
              <p>Use the runbook for setup details, credential handling, and the current paper/data rollout plan.</p>
            </CardContent>
          </Card>
        </div>
      </section>

      <section id="setup" className="scroll-mt-24 space-y-4" data-testid="kalshi-setup-section">
        <SectionHeading title="Setup" note="Onboarding checklist and docs" />
        <Card>
          <CardHeader>
            <CardTitle>Runbook</CardTitle>
          </CardHeader>
          <CardContent className="flex flex-wrap items-center gap-3 text-sm">
            <span className="text-ink-dim">Use the Kalshi paper/data runbook for environment setup and operational guardrails.</span>
            <Button size="sm" variant="outline" asChild>
              <a href="/docs/runbooks/kalshi-paper-data.md" target="_blank" rel="noreferrer">Open runbook</a>
            </Button>
          </CardContent>
        </Card>
      </section>
    </div>
  )
}
