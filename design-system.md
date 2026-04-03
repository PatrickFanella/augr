# Get Rich Quick Design System

Selected base preset: `03-modern-dark`

This file defines the visual system for the `web/` frontend. It replaces the current light-first scaffold with a permanent dark, information-dense trading console.

## Product intent

- **Project type:** trading dashboard / operator console
- **Users:** active traders, builders, and operators monitoring strategies, runs, portfolio, risk, and memory
- **Tone:** technical, fast, trustworthy, disciplined, nocturnal
- **Stack:** React 19 + Vite + Tailwind CSS 4 + shadcn-compatible primitives

## Non-negotiables

- **Dark mode only.** No theme toggle. No light tokens in live UI.
- **Information density over decoration.** Dense layouts, compact controls, readable tables, fast scanning.
- **Operator-grade clarity.** State, risk, and status must be legible at a glance.
- **Token-driven styling only.** No scattered raw hex values in components unless mapped back into tokens.
- **Charts and tables are first-class UI, not afterthoughts.**

## Visual direction

Use a modern, premium dark tool aesthetic derived from `03-modern-dark`, but adapted away from marketing-site spaciousness and toward a compact trading terminal feel.

The UI should feel like:

- a high-end execution console
- a serious night-mode analytics platform
- a system operators can keep open all day without eye fatigue

The UI should **not** feel like:

- a generic shadcn dashboard
- a cyberpunk demo
- a light app with colors inverted
- a landing page with giant hero sections and wasted vertical space

## Typography

Use a technical pairing that improves scanning and numeric readability.

- **UI sans:** `IBM Plex Sans`, `system-ui`, `sans-serif`
- **Data mono:** `JetBrains Mono`, `ui-monospace`, `monospace`

### Type scale

- Page title: `text-2xl` / `font-semibold`
- Section title: `text-lg` / `font-semibold`
- Panel title: `text-sm` / `font-semibold` / `tracking-wide`
- Body: `text-sm`
- Dense label: `text-[11px]` / `font-medium` / `uppercase` / `tracking-[0.16em]`
- Numeric metrics: `font-mono`

### Typography rules

- Use sans for navigation, forms, and prose.
- Use mono for prices, P&L, percentages, timestamps, IDs, schedule strings, and logs.
- Avoid oversized headings except on the login screen.
- Prefer concise labels over marketing copy.

## Color tokens

Keep the existing semantic token names where possible so implementation can stay idiomatic.

### Core dark surfaces

- `--background`: `#06080d`
- `--foreground`: `#e7edf7`
- `--card`: `#0d1321`
- `--card-foreground`: `#e7edf7`
- `--popover`: `#0b1120`
- `--popover-foreground`: `#e7edf7`
- `--secondary`: `#111827`
- `--secondary-foreground`: `#c4cfdd`
- `--muted`: `#0f172a`
- `--muted-foreground`: `#8b9bb3`
- `--accent`: `#14213d`
- `--accent-foreground`: `#dbe7ff`
- `--border`: `rgba(148, 163, 184, 0.18)`
- `--input`: `rgba(148, 163, 184, 0.16)`
- `--ring`: `rgba(96, 165, 250, 0.55)`

### Brand / interaction

- `--primary`: `#60a5fa`
- `--primary-foreground`: `#08111f`

### Trading semantics

- **success / long / profit:** `#10b981`
- **warning / pending:** `#f59e0b`
- **danger / short / loss:** `#ef4444`
- **info / live / streaming:** `#22d3ee`

### Usage rules

- Base surfaces should use layered blue-slate darks, not pure black.
- Primary blue is for interaction, selection, and focus.
- Green and red are reserved for performance and trade direction.
- Amber is for attention, queued work, paper mode, and warnings.

## Layout system

### Shell

- The application shell should feel like desktop software.
- Prefer a **persistent left navigation rail on desktop** and a compact top bar on smaller breakpoints.
- Page headers should be compact and utility-first, with status chips and actions aligned right.
- Avoid giant rounded hero containers on core app routes.

### Density rules

- Default panel padding: `p-4`
- Dense panel padding: `p-3`
- Section gaps: `gap-4` or `gap-5`, not `gap-8` unless intentionally separating major regions
- Table row height: `36px`–`40px`
- Form control heights: `h-9` default, `h-8` dense variant when appropriate

### Grid behavior

- Dashboard and detail pages should default to 12-column thinking on desktop.
- Important monitoring information should appear above the fold.
- Related panels should cluster tightly rather than float in isolated islands.

## Components

### Cards / panels

- Use `rounded-lg`, not oversized radii.
- Cards should read as panels, not soft marketing tiles.
- Borders should be subtle but visible.
- Elevated panels may use a faint top highlight and restrained shadow.

### Buttons

- Primary: blue fill, dark text, compact height, strong hover contrast.
- Secondary: dark filled panel button.
- Outline: low-contrast border, stronger hover fill.
- Ghost: only for tertiary actions.
- Add dense size support for table and toolbar actions.

### Badges / pills

- Use mono or narrow uppercase labels for states.
- Active, paused, paper, live, success, and risk states must be visually distinct.
- Prefer filled or tinted badges over neutral outlines for critical status.

### Forms

- Inputs use dark filled surfaces with visible borders.
- Labels are compact and high-contrast.
- Error text must use `role="alert"` where applicable.
- Support keyboard-first use and obvious focus rings.

### Tables / lists

- Sticky headers where useful.
- Numeric columns right-aligned.
- Prices, returns, sizes, timestamps, and IDs use mono.
- Row hover should highlight softly without layout shift.
- Allow denser scanning than the current card-stack-heavy presentation.

### Charts

- Use tokenized colors only.
- No hardcoded green hex values in chart components.
- Grid lines and axes should be subtle but readable.
- Tooltips should match panel styling.

## Motion

- Motion must be subtle and fast.
- Default transition range: `150ms`–`220ms`
- No decorative floating blobs inside core data panes.
- Use restrained ambient depth only on background layers and login surface.
- Respect `prefers-reduced-motion`.

## Accessibility

- Normal text must maintain at least WCAG AA contrast.
- Focus states must always be visible.
- Color cannot be the only indicator for trade or risk status.
- Error messages must be announced with `role="alert"` or equivalent.
- Dense UI must remain usable at `320px`, `768px`, `1024px`, and `1440px`.

## Route-level application priorities

Apply the system in this order:

1. **Global tokens and shell** — `index.css`, app shell, base primitives
2. **High-signal app pages** — dashboard, strategies, runs, portfolio
3. **Operational detail pages** — strategy detail, pipeline run, risk, realtime
4. **Supporting pages** — settings, memories, login

## Acceptance criteria for the overhaul

- No light background tokens remain in the live application theme.
- App shell, buttons, inputs, badges, cards, and tables all read as one system.
- Main pages surface more useful information above the fold than the current scaffold.
- Positive/negative/live/risk states are consistent everywhere.
- Charts and table visuals use tokens, not arbitrary colors.
- Keyboard focus, reduced motion, and contrast remain intact.

## Implementation notes

- Start by replacing the root tokens in `web/src/index.css`.
- Then refactor shared UI primitives in `web/src/components/ui/`.
- Then reshape page layouts around denser panel grids.
- If a page must keep generous spacing for readability, make that an explicit choice rather than the default.
