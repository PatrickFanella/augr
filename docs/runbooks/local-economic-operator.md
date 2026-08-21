# Local economic operator

`augr-economic` is an explicitly invoked local command. It is not wired into
HTTP, automation, providers, brokers, deployment, or live trading. It requires
the exact current schema and fails before mutation on a schema mismatch.

Bootstrap the six reviewed scored-paper capital tiers, including $500 and
$5 million. The namespace and timestamp are part of the immutable request;
an exact retry returns the same deterministic accounts and a conflicting retry
fails closed.

```bash
go run ./cmd/augr-economic bootstrap-scored-tiers --input bootstrap.json
```

```json
{
  "namespace_prefix": "local/overhaul-v1",
  "created_at": "2026-08-20T12:00:00Z"
}
```

Record one operator-authorized local deposit or withdrawal:

```bash
go run ./cmd/augr-economic capital-flow --input flow.json
```

```json
{
  "account_id": "00000000-0000-4000-8000-000000000000",
  "type": "withdrawal",
  "amount": "25.00",
  "idempotency_key": "local-withdrawal-1",
  "external_reference": "operator-ticket-1",
  "metadata": {"reason": "local qualification"},
  "effective_at": "2026-08-20T13:00:00Z",
  "observed_at": "2026-08-20T13:00:01Z"
}
```

The command derives currency from the retained account, appends the capital
flow idempotently, and returns both the unchanged opening-capital summary and
the schema-65 ledger transaction. PostgreSQL creates the two balanced postings
atomically with the flow. Reusing the key with different economics is rejected.

Do not point this command at production or a shared database without separate
authorization. Do not use it to fabricate elapsed OVR-702/703 evidence.
