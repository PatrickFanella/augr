# Typed Generative Strategy Compiler Runbook

## Boundary

This runbook inspects OVR-601 schema-95 research artifacts. The retained spec
is a synthetic generated candidate. It proves constrained validation,
deterministic compilation, and immutable OVR-302 version binding; it does not
invoke a model, acquire source/search lineage, declare or execute an experiment,
propose or activate a deployment, schedule work, create intents, route orders,
or trade live.

Use only the dedicated loopback database:

```bash
export GENERATIVE_STRATEGY_QUALIFICATION_DB_URL='postgres://USER:PASSWORD@127.0.0.1:PORT/augr_ovr601_qual_20260820?sslmode=disable'
psql "$GENERATIVE_STRATEGY_QUALIFICATION_DB_URL" -Atc \
  "select current_database(),current_schema(),version,dirty from schema_migrations"
```

Require `public`, version 95, and `dirty=false`. Never run qualification writes
against a shared or production database.

## Inspect the typed spec and normalized contract

```bash
psql "$GENERATIVE_STRATEGY_QUALIFICATION_DB_URL" -x -c \
  "select id,family_id,family_sha256,spec_key,input_count,instrument_count,
          prohibition_count,property_count,example_count,normalized_row_count,
          sha256,canonical_json
     from generated_strategy_specs"

psql "$GENERATIVE_STRATEGY_QUALIFICATION_DB_URL" -x -c \
  "select spec_id,kind,sequence,canonical_row
     from generated_strategy_spec_rows order by kind,sequence"
```

Require spec `146583ee-f990-51bf-f94f-d8416bd5ae94`, SHA-256
`e66fb26fcc33cd76dcce616543f8962ad7d2f821742d80fcb056169b51b45ca3`,
and seventeen normalized rows. Reconstruct every input, fixed instrument,
prohibition, property test, and example byte-for-byte from the canonical arrays.
The required prohibitions include live orders, risk mutation, evidence mutation,
promotion, secret/network access, and lookahead.

## Inspect compiled version and receipt

```bash
psql "$GENERATIVE_STRATEGY_QUALIFICATION_DB_URL" -x -c \
  "select r.id,r.state,r.spec_id,r.spec_sha256,r.version_id,r.version_sha256,
          r.compiler_kind,r.compiler_version,r.source_commit,
          r.source_tree_sha256,r.config_schema,r.decision_contract,
          r.config_sha256,r.sha256,v.config,v.required_kind_count
     from generated_strategy_compilation_receipts r
     join strategy_versions v on v.id=r.version_id"
```

Require version `147faf3b-9721-686f-9388-4c0c3b300f3c`, version SHA-256
`4f9705a44729d9591e0d4d07efdb476b181aab62d4ae33e7261e86f7bd5da8b9`,
receipt `0aa08924-7d7a-bf97-a413-9361dab85aa2`, and receipt SHA-256
`a3a873940fe594a25eebc37d17ef9a648ea0fe3c8ef1cba81597bec35329ae46`.
The config must bind the exact spec ID/SHA and contain no deployment state,
schedule, intent, order, arbitrary source, or executable function.

## Retry, recovery, and rollback

Exact retry converges. A semantic change produces a new content identity and
conflicts under the retained family/spec key. Never update or delete retained
evidence; append-only triggers intentionally reject both.

```bash
go test -race ./internal/generativestrategy/...
export NEW_EMPTY_GENERATIVE_DB_URL='postgres://USER:PASSWORD@127.0.0.1:PORT/new_empty_schema_95_database?sslmode=disable'
GENERATIVE_STRATEGY_QUALIFICATION_DB_URL="$NEW_EMPTY_GENERATIVE_DB_URL" \
  go test -race ./internal/repository/postgres \
  -run '^TestGenerativeStrategyRetainedQualification$' -count=1 -v
```

Migration 95 is empty-only reversible: `augr_ovr601_empty_20260820` passed
`95 -> 94 -> 95`, while retained evidence refused rollback. An expected failed
`golang-migrate down 1` can leave only version metadata dirty. On a dedicated
qualification database only, inspect every retained table first and then
restore metadata with `migrate ... force 95`. Never use `force` as an
operational rollback procedure; restore a verified pre-migration backup.
