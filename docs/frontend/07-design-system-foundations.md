# Frontend Design System Foundations

Source inputs: `docs/frontend-ui-api-brief.md`, `docs/frontend/02-user-workflows.md`, `docs/frontend/03-information-architecture.md`, `docs/frontend/05-p0-screen-specifications.md`, `docs/frontend/wireframes/README.md`, and `docs/frontend/06-interaction-operational-safety.md`.

This document defines the shared visual and interaction foundations for the Augr trading operations UI. It is a specification only. It does not implement screens, components, styling, or library choices.

## 1. Design principles

1. **Operational clarity before aesthetics**: make safety state, freshness, paper/live mode, and action risk visible before decoration.
2. **Dense but calm**: optimize for scanning many rows and indicators without using saturated color everywhere.
3. **Evidence-first navigation**: IDs, timestamps, linked entities, raw payloads, and server-state verification must remain easy to inspect and copy.
4. **Conservative uncertainty**: stale, unknown, partial, or offline data must never read as safe.
5. **Accessible by default**: text labels, icons, shapes, and layout must carry meaning without color alone.
6. **Themeable tokens, two approved themes**: the operator shell implements
   light and dark themes with the same semantic tokens. Both must preserve
   status contrast, chart legibility, and paper/live safety cues.

## 2. Semantic token model

Use primitive tokens only as implementation detail. Components consume semantic tokens.

```ts
type SemanticColorToken =
  | 'color.bg.app'
  | 'color.bg.surface'
  | 'color.bg.surfaceRaised'
  | 'color.bg.surfaceSunken'
  | 'color.bg.rowHover'
  | 'color.bg.rowSelected'
  | 'color.text.primary'
  | 'color.text.secondary'
  | 'color.text.muted'
  | 'color.text.inverse'
  | 'color.border.subtle'
  | 'color.border.default'
  | 'color.border.strong'
  | 'color.focus.ring'
  | 'color.action.primary'
  | 'color.action.danger'
  | 'color.status.*';

type Density = 'compact' | 'comfortable' | 'spacious';
```

Baseline theme intent: neutral, high-contrast, low-saturation in both light and
dark modes. Status colors are accents, not page backgrounds, except critical
banners. Theme selection is persisted locally and does not change operational
meaning.

Chart policy: a chart must represent a genuine ordered series or a correctly
defined quantitative composition. A current P&L snapshot must not be drawn as
an equity curve, and P&L magnitude must not be labeled as allocation. Until a
historical series is returned by an API, show an explicitly labeled snapshot;
open exposure composition uses absolute notional (`quantity × current price ×
contract multiplier`) from position data.

## 3. Typography hierarchy

Use a restrained system-font stack for UI text and a tabular-capable monospace stack for numbers, IDs, JSON, timestamps, and code-like values.

| Role | Size | Weight | Line height | Use |
| --- | ---: | ---: | ---: | --- |
| Display | 24px | 650 | 32px | Rare: login/app title, not routine pages. |
| Page title | 20px | 650 | 28px | `PageHeader` title, entity detail title. |
| Section title | 16px | 650 | 24px | Panels, table groups, modal sections. |
| Body | 14px | 400 | 20px | Default labels, descriptions, table cells in comfortable density. |
| Body strong | 14px | 600 | 20px | Important values, row primary text. |
| Small | 12px | 400 | 16px | Secondary metadata, helper text, timestamps. |
| Micro | 11px | 600 | 14px | Badges, column utility labels, uppercase risk labels. Use sparingly. |

Rules:

- Avoid all-caps for long labels. Use uppercase only for compact badges such as `LIVE`, `FAILED`, `STALE`.
- Keep page titles short and pair with breadcrumbs/context instead of long headings.
- Error and safety text must use normal sentence case for readability.
- Body text must not drop below 12px. Micro text is only for repeated metadata with an accessible label.

## 4. Numeric and tabular typography

Trading and operational screens require stable numeric alignment.

- Use tabular numerals for prices, P&L, quantities, percentages, durations, counts, and timestamps.
- Right-align comparable numeric columns: quantity, price, fee, P&L, exposure, confidence, duration.
- Keep IDs, hashes, broker external IDs, cron strings, and JSON in monospace.
- Use sign-aware formatting for P&L and deltas: `+$1,234.56`, `-$125.00`, `0.00`; do not rely on green/red alone.
- Use consistent decimal precision per domain:
  - Currency: 2 decimals by default; more only when backend values require precision.
  - Percent/exposure: 1–2 decimals.
  - Quantities: trim insignificant decimals but preserve backend precision in accessible label/tooltips.
  - Confidence: percentage with 0–1 decimal or raw decimal only when source data is ambiguous.
- Use em dash `—` for absent values and `Unknown` for unknown semantic state.

## 5. Spacing scale

Use a 4px base scale.

| Token | Value | Use |
| --- | ---: | --- |
| `space.0` | 0 | Flush table/grid edges. |
| `space.1` | 4px | Icon/text gap, tight cell padding. |
| `space.2` | 8px | Badge padding, compact form gap. |
| `space.3` | 12px | Default control gap, table cell horizontal padding. |
| `space.4` | 16px | Panel padding compact, section gap. |
| `space.5` | 20px | Form row gap, modal content gap. |
| `space.6` | 24px | Page section gap, drawer padding. |
| `space.8` | 32px | Major layout separation. |
| `space.10` | 40px | Login/public page vertical spacing. |

Density mapping:

- Compact: cell horizontal `8px`, vertical `4–6px`, panel padding `12px`.
- Comfortable: cell horizontal `12px`, vertical `8px`, panel padding `16px`.
- Spacious: reserved for login/empty states and confirmation copy, not dense operational tables.

## 6. Sizing scale

| Token | Value | Use |
| --- | ---: | --- |
| `size.control.sm` | 28px | Dense table filters, icon buttons. |
| `size.control.md` | 36px | Default inputs/buttons. |
| `size.control.lg` | 44px | Touch-first controls and high-risk confirmations. |
| `size.icon.sm` | 14px | Badge icons, table icons. |
| `size.icon.md` | 16px | Default icons. |
| `size.icon.lg` | 20px | Header/status icons. |
| `size.nav.rail` | 72px | Collapsed desktop/tablet nav rail. |
| `size.nav.sidebar` | 220–260px | Expanded desktop nav. |
| `size.header` | 56–72px | Authenticated shell header depending status rows. |

Touch targets should be at least 44px on narrow screens. Dense desktop controls may be 28–36px if label text and keyboard access are clear.

## 7. Border and surface hierarchy

Surfaces should separate information without heavy decoration.

| Level | Token intent | Usage |
| --- | --- | --- |
| App background | `bg.app` | Overall viewport. |
| Surface 1 | `bg.surface` + `border.subtle` | Panels, cards, tables. |
| Surface 2 | `bg.surfaceRaised` + `border.default` | Sticky headers, drawers, popovers. |
| Surface 3 | `bg.surfaceSunken` + `border.subtle` | JSON/code blocks, read-only raw payload wells. |
| Critical surface | status-specific subtle background + strong border | Risk banners, live-operation warnings, offline/stale blockers. |

Rules:

- Prefer 1px borders over shadows for ordinary grouping.
- Use stronger borders for sticky controls, selected rows, active tabs, and critical state.
- Do not use ornamental gradients, glass effects, or decorative glows.
- Rounded corners should be modest: `4px` for small controls, `6px` for panels/dialogs.

## 8. Focus indicators

Focus must be visible, consistent, and not color-only.

- Default focus: 2px outline using `color.focus.ring`, 2px offset when space allows.
- Inside dense tables, use an inset focus ring plus row/column highlight to avoid layout shift.
- Destructive/high-risk controls add a secondary shape cue such as thicker outline or warning icon while focused.
- Dialogs and drawers must trap focus and restore focus to the invoking control on close.
- Skip-to-content link appears on keyboard focus in `AppShell`.

## 9. Elevation usage

Elevation is functional, not decorative.

- Level 0: page background and static panels.
- Level 1: sticky headers, sticky table columns, hoverable row previews; border + minimal shadow if needed.
- Level 2: dropdowns, popovers, column picker, filter menus.
- Level 3: drawers and modals.
- Level 4: toast stack above dialogs only when non-blocking; blocking safety dialogs remain visually dominant.

Do not stack multiple shadows. Prefer scrims for modal context and borders for table stickiness.

## 10. Table density options

Default for operational views is compact.

| Density | Row height | Cell padding | Use |
| --- | ---: | --- | --- |
| Compact | 32px | 4–6px vertical, 8–12px horizontal | Strategies, runs, orders, trades, activity drawer. |
| Comfortable | 40px | 8px vertical, 12px horizontal | Portfolio and mixed content tables. |
| Spacious | 48px+ | 12px vertical | Rare: mobile cards or non-dense admin forms. |

Table controls must allow column visibility before adding spacious density. Do not shrink text below accessibility minimums to fit more columns.

## 11. Form density

- Default form control height: 36px desktop, 44px narrow/touch.
- Label always visible; placeholder is never the only label.
- Helper/error text below field at 12px/16px line-height.
- Dense inline filter bars may use compact controls with accessible labels.
- High-risk forms/dialog fields use comfortable density for accuracy: reason fields, typed confirmation token, admin credential, secret replacement.
- JSON editors/viewers need line numbers or stable row affordances, monospace text, parse errors linked to the failing line when possible.

## 12. Modal and drawer sizing

| Pattern | Desktop size | Mobile behavior | Use |
| --- | --- | --- | --- |
| Small dialog | 420–480px wide | Bottom sheet or full-width modal | Simple confirmation. |
| Standard dialog | 560–640px wide | Bottom sheet with max-height | Reason confirmation, error details. |
| High-risk dialog | 640–760px wide | Full-screen or tall sheet | Live/admin operations with context and typed token. |
| Drawer | 420–520px wide | Full-screen sheet | Activity drawer, detail preview. |
| Wide drawer | 720–960px wide | Full-screen | JSON-heavy detail, responsive detail panel. |

Rules:

- Critical mode/environment context stays visible near the confirm action.
- Scroll only the body; keep title, critical context, and actions pinned when content is long.
- Admin credentials/secrets must not appear in URL, toast, logs, or copyable diagnostic blocks.

## 13. Responsive breakpoints

Breakpoints align with approved wireframes.

| Token | Width | Behavior |
| --- | ---: | --- |
| `bp.mobile` | < 640px | Single column; bottom/slide nav; tables become priority cards/lists. |
| `bp.tablet` | 640–1023px | Collapsible nav rail; two-column where safe; dense tables may horizontal scroll. |
| `bp.compactDesktop` | 1024–1279px | Desktop chrome with tighter grids; column priority may hide low-value columns by default. |
| `bp.desktop` | 1280–1535px | Primary operational layout; persistent nav and dense tables. |
| `bp.wide` | >= 1536px | More columns and optional detail panels; avoid stretching line length beyond readability. |

Responsive rules:

- Never collapse away safety status, environment, paper/live mode, or stale/offline state.
- Dense tables may horizontally scroll on tablet/desktop, but mobile should use card/list summaries with drill-in.
- Keep critical controls visible at the top of Risk Console and Cockpit.

## 14. Motion principles

Motion should communicate state changes without distraction.

- Use short transitions: 100–160ms for hover/focus, 160–220ms for drawers/modals, 80–120ms for table row state changes.
- Use opacity/transform for overlays; avoid large bouncing, parallax, or decorative motion.
- Loading skeletons may shimmer subtly only if reduced motion is not requested.
- Realtime updates should not constantly animate entire rows; prefer a brief left-edge flash, timestamp update, or small `updated` marker.
- Critical alerts can pulse once on arrival, then remain static.

## 15. Reduced-motion behavior

Respect `prefers-reduced-motion: reduce`.

- Disable shimmer, pulsing, count-up animations, and row flash motion.
- Replace animated transitions with instant state changes or very short fades under 80ms.
- Do not auto-scroll realtime feeds unless the user is pinned to latest; announce updates via throttled live regions instead.
- Loading skeletons become static blocks.

## 16. First vertical-slice foundation requirements

The first vertical slice should include these foundations before feature screens are built:

1. Semantic design tokens for color, spacing, typography, borders, focus, z-index/elevation, and status.
2. App density configuration with compact table defaults.
3. Accessible focus and keyboard navigation conventions.
4. Status token mapping from `docs/frontend/09-status-and-data-display-conventions.md`.
5. Shared layout primitives from `docs/frontend/08-shared-component-inventory.md`: `AppShell`, `PageHeader`, `StatusBadge`, `DataTable`, `FilterBar`, `LastUpdated`, `InlineAlert`, `DataChart`, dialogs, and loading/error/empty states.

## 17. Decisions deferred to final libraries

- Exact token implementation format: CSS variables, TypeScript token object, or design-token build pipeline.
- Table virtualization mechanics, sticky column API, column pinning limitations, keyboard grid behavior, and column resizing model.
- Chart library rendering model, tooltip accessibility, and canvas/SVG tradeoffs.
- JSON viewer/editor implementation and line-level error APIs.
- Form library integration for validation summaries, field arrays, and dirty/stale conflict handling.
