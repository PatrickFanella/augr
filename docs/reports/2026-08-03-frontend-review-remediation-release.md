# Frontend review remediation release evidence

Date: 2026-08-03
Release commit: `735b344bf7ab404fefa37e6ba643dc63f07d5704`
Source branch: `agent/augr-review-remediation`
Target: `https://augr.subcult.tv`

## Publication

- `origin/main` and `origin/agent/augr-review-remediation` both resolved to the release commit.
- Local database dumps are excluded through `backups/`; durable reports, SQL evidence, test scripts, runbooks, and plans are versioned.

## Pre-deploy verification

- `go test ./...`: passed.
- Frontend Vitest suite: 10 files and 162 tests passed with one serial worker.
- `npm run lint`: passed.
- `npm run build`: passed; 2,165 modules transformed.
- `docker compose -f docker-compose.nuc.yml config --quiet`: passed with immutable release image names and build metadata.

## Database migration

- Before: schema `59`, dirty `false`.
- Applied: `000060_kalshi_snapshot_provenance` in 7m52s.
- After: schema `60`, dirty `false`.
- Verified columns: `provider`, `environment`, and `source_url` on `kalshi_market_snapshots`.
- Historical rows retain the truthful `unknown` environment/default source; newly ingested rows receive configured provenance.
- The existing July 26 recovery dump was retained. A new full logical dump was stopped because the production database is 75 GB and the additive migration was waiting on its snapshot lock; the incomplete archive was removed before migration continued.

## Immutable images

- App: `augr-app:review-735b344bf7ab`
  - Image ID: `sha256:4123aff8c8abf56d7fd69abadfd1835fbe57a19933dc5f0741ae462413104f34`
  - Version: `review-remediation`
  - Commit: `735b344bf7ab404fefa37e6ba643dc63f07d5704`
  - Build time: `2026-08-03T18:49:17Z`
- Web: `augr-web:review-735b344bf7ab`
  - Image ID: `sha256:77bc10880c962db89faf99186c177c28108f374d2a7bf215f6fc6ba8e78f1971`

## Live verification

- App, web, PostgreSQL, and Redis containers healthy/running.
- Public readiness: `{"status":"ok","db":"ok","redis":"ok"}`.
- Public title: `Augr — Paper Operations`.
- Live assets matched the release build: `index-DSqMzqB-.js` and `index-DcJe_J__.css`.
- No panic, fatal error, schema mismatch, or migration failure appeared in post-deploy app/web logs.
- Settings showed environment `production`, version `review-remediation`, the full release SHA, schema `60 / required 60`, and explicit Polymarket live/Kalshi demo market-data badges.

## Authenticated visual audit

Chromium audited all 14 authenticated top-level routes at:

- Desktop: 1453 × 1069
- Tablet: 900 × 1024
- Mobile: 390 × 844

The audit produced and inspected 42 full-page screenshots. It asserted:

- authentication survived the complete route sequence;
- no page-level horizontal overflow;
- no browser runtime/console errors;
- the production document title on every route;
- mutually exclusive table/card layouts on Strategies and Orders;
- truthful incomplete P&L, 0/10 valuation coverage, five-market exposure including Kalshi, and `Showing 5 of 10` disclosure;
- closed overlay activity behavior and full-width desktop tables.

A uniquely named temporary audit account was used because the documented demo password no longer matched production. The temporary user was deleted immediately after the audit; no credential was retained.

## Residual operational signals

- The cockpit correctly reports incomplete valuation because all 10 open positions remain unmarked.
- Automation remains degraded: `kalshi_settlement` auto-disabled after six gate-ineligible failures, and the configured Ollama endpoint refused a signal-evaluator connection during verification. These are now visible operator conditions, not hidden or misclassified UI state.
- The container build reported six npm dependency advisories: five high and one low. Dependency remediation requires a separate compatibility-reviewed update.
- PostgreSQL continues to warn that the database has a recorded collation version but no actual collation version; this predates the release and remains operational maintenance work.
