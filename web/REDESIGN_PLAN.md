# Augr Frontend Redesign Plan

## Direction

Augr should feel like a serious trading operations command center using a Neubrutal HUD system: flat instrumentation panels, hard borders, sharp corners, monospace hierarchy, uppercase micro-labels, purple/indigo signal accents, and restrained teal confirmation states. The implementation intentionally treats the prior UI as disposable and rebuilds the visual foundation around semantic CSS variables and stable shell/layout rules.

## Implemented

### Foundation
- Replaced theme tokens in `src/app/theme.css` with a flat Neubrutal HUD HSL palette (`void`, `panel`, `ink`, `signal`, `pulse`, `confirm`, `alert`) plus semantic product tokens.
- Kept backwards-compatible aliases so existing feature/business logic could remain intact while the design language changed underneath it.
- Replaced global styling in `src/index.css` with a strict public-facing HUD surface: no glassmorphism, no decorative gradients, square scrollbars, hard 3px borders, flat panels, visible focus rings, status glyphs plus labels, responsive grids, dialog/shell/table/form rules, and reduced-motion handling.

### App shell
- Updated `src/app/layout/AppShell.tsx` mobile detection to use `matchMedia` state instead of a render-time `window.innerWidth` snapshot.
- Reworked sidebar/topbar styling via CSS so collapse, mobile drawer, workspace scrolling, and activity drawer behavior are predictable.
- Added tooltip-style collapsed navigation labels, strong active states, stable `min-width: 0` containment, and no nested viewport-width layout dependency.

### Components and pages
- Standardized the existing primitives through the new global design contract: buttons, panels, status pills, alerts, inputs, tables, empty/loading states, dialogs, breadcrumbs, entity links, and shared query states.
- Existing page business logic and data fetching remain intact. High-impact pages inherit the new design system through shared classes: Cockpit, Risk, Runs, Strategies, Orders, Trades, Portfolio, Automation, Events, Stock, and Login.

## QA checklist

- [x] Dark and high-contrast light HUD tokens available through CSS variables.
- [x] Theme toggle remains CSS-variable based.
- [x] Shell uses grid with stable sidebar width variables.
- [x] Mobile sidebar is a drawer with backdrop.
- [x] Main workspace and content regions use `min-width: 0` and owned scrolling.
- [x] Tables scroll inside `.table-wrap` instead of forcing page overflow.
- [x] Status states use text plus glyph/dot, not color alone.
- [x] Focus states are visible.
- [x] Motion respects `prefers-reduced-motion`.
- [x] Public surface avoids gradients/glass/blur and uses flat HUD color blocks.

## Manual QA still recommended

- Browser widths: 320, 375, 640, 768, 1024, 1280, 1536+.
- Browser zoom at 200%.
- Long names/IDs/URLs in tables and entity links.
- Login, Cockpit, Risk controls, and at least one detail page in both themes.
- Mobile drawer open/close and desktop collapsed-sidebar tooltip behavior.

## Verification

Run from `web/`:

```bash
npm run lint
npm run test
npm run build
```

Deployment uses the nuc Compose stack and almaz Caddy route documented in the repository/homelab notes.
