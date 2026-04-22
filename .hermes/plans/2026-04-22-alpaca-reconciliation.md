# Alpaca reconciliation implementation plan

Goal: add a real Alpaca broker reconciliation path that imports Alpaca paper/live positions, orders, and fills into augr’s local positions/orders/trades tables and exposes it as an automation job.

Architecture: introduce a dedicated reconciliation service under internal/automation that talks to Alpaca through a narrow snapshot interface, maps broker state into domain orders/positions/trades, and upserts local records with deterministic matching. Wire that service into the automation orchestrator as a new manual/scheduled job and add the minimal schema/repository support needed for idempotent fill tracking and reliable order matching.

Tech stack: Go, existing Alpaca HTTP client, existing postgres repositories, automation orchestrator, SQL migrations, Go tests.

Implementation slices:
1. Add failing unit tests for reconciliation service behavior and orchestrator registration.
2. Add schema support for durable Alpaca reconciliation metadata and bump required schema version.
3. Add repository capabilities needed for lookup by external order id and fill activity id.
4. Implement Alpaca snapshot fetch methods for orders and fills.
5. Implement reconciliation service.
6. Register/wire automation job in runtime.
7. Run targeted and broader verification.

Planned files to touch:
- create: internal/automation/alpaca_reconciliation.go
- create: migrations/000032_alpaca_reconciliation.up.sql
- create: migrations/000032_alpaca_reconciliation.down.sql
- create: migrations/alpaca_reconciliation_migration_test.go
- modify: internal/automation/alpaca_reconciliation_test.go
- modify: internal/automation/orchestrator.go
- modify: internal/automation/jobs_premarket.go or another automation job file for registration
- modify: internal/execution/alpaca/broker.go
- modify: internal/execution/alpaca/broker_test.go
- modify: internal/repository/interfaces.go
- modify: internal/repository/postgres/order.go
- modify: internal/repository/postgres/order_test.go
- modify: internal/repository/postgres/trade.go
- modify: internal/repository/postgres/trade_test.go
- modify: internal/repository/postgres/schema_version.go
- modify: cmd/tradingagent/runtime.go
- modify: cmd/tradingagent/schema_version_sync_test.go

Notes:
- Use strict TDD: write failing tests first, then minimal implementation.
- Keep behavior idempotent: repeated reconcile runs must not duplicate orders, positions, or fills.
- Prefer matching active strategies by ticker for imported positions/orders, but tolerate no matching strategy by leaving StrategyID nil.
- Fill dedupe should use Alpaca activity id, not just timestamp/qty/price.
- Existing DB currently cannot persist a broker-origin fill activity id, so migration will likely add one field to trades and maybe a unique index.
