# Hypothesis and Critic Workflow Runbook

## Boundary

This runbook inspects OVR-602 schema-96 research evidence. The retained data is
synthetic and locally qualified. It proves deterministic hypothesis authoring,
complete source/search/test lineage, independent advisory criticism, immutable
persistence, and exact OVR-301/305/601 parent binding. It does not call a model
or search provider, acquire licensed material, perform independent human
review, declare or run an experiment, change strategy lifecycle state, propose
or activate a deployment, schedule work, reserve capital, create an intent,
route an order, or trade.

Use only the dedicated loopback database:

```bash
export RESEARCH_WORKFLOW_QUALIFICATION_DB_URL='postgres://USER:PASSWORD@127.0.0.1:PORT/augr_ovr602_qual_20260820_v3?sslmode=disable'
psql "$RESEARCH_WORKFLOW_QUALIFICATION_DB_URL" -Atc \
  "select current_database(),current_schema()"
```

Require database `augr_ovr602_qual_20260820_v3` and schema `public`. Never run
qualification writes against a shared or production database.

## Inspect the hypothesis graph

```bash
psql "$RESEARCH_WORKFLOW_QUALIFICATION_DB_URL" -x -c \
  "select id,workflow_key,manifest_id,manifest_sha256,robustness_policy_id,
          robustness_policy_sha256,robustness_family_id,
          robustness_family_sha256,assessment_id,assessment_sha256,spec_id,
          spec_sha256,version_id,version_sha256,receipt_id,receipt_sha256,
          source_count,search_count,test_count,sha256,canonical_json
     from research_hypotheses"

psql "$RESEARCH_WORKFLOW_QUALIFICATION_DB_URL" -c \
  "select hypothesis_id,sequence,source_key,canonical_row
     from research_hypothesis_sources order by sequence;
   select hypothesis_id,search_sequence,sequence,source_key,rank,selected,
          canonical_row from research_hypothesis_search_results
     order by search_sequence,sequence;
   select hypothesis_id,sequence,test_key,test_type,canonical_row
     from research_hypothesis_tests order by sequence"
```

Require hypothesis `5bebfd98-db48-be22-c564-b925f3a4289c`, SHA-256
`f3fd8a1b759c4fa70c44082fdf4920122c34459160dcb0d601014e03d9f5b58d`,
two sources, two searches, four search results, and ten tests. Every retained
source must match an exact manifest observation key, content hash, and
availability timestamp. Every selected result must have a retained source;
all five generated property tests, the generated example, and leakage, cost,
baseline, and refutation tests must be present.

## Inspect independent critic evidence

```bash
psql "$RESEARCH_WORKFLOW_QUALIFICATION_DB_URL" -x -c \
  "select id,review_key,hypothesis_id,hypothesis_sha256,recommendation,
          finding_count,check_count,sha256,canonical_json
     from research_critics order by review_key;
   select critic_id,sequence,finding_key,category,severity,status,canonical_row
     from research_critic_findings order by critic_id,sequence;
   select critic_id,sequence,check_name,check_state,canonical_row
     from research_critic_checks order by critic_id,sequence"
```

Require the review-ready critic
`c20cb649-7fc1-f449-98fd-c780d139da29`, SHA-256
`79d920e7062df2bb15395b69f3b284a154ce7ae15aaf321541bb17278bc258a3`,
and the critical-finding rejection
`9eb14937-416c-9612-d5bb-5f642e8e6092`, SHA-256
`cff11770ad3288b97bd40ceb19a057dd5dad6a8a8a456491c14924211d64a312`.
The review-ready artifact requires six explicit passes. Unknown or failed
checks produce `revise`; an open high or critical finding produces `reject`.
No recommendation is lifecycle, experiment, deployment, scheduling, capital,
or trading authority.

## Retry, recovery, and rollback

```bash
go test -race ./internal/researchworkflow/...
RESEARCH_WORKFLOW_QUALIFICATION_DB_URL="$RESEARCH_WORKFLOW_QUALIFICATION_DB_URL" \
  go test -race ./internal/repository/postgres \
  -run '^TestResearchWorkflowRetainedQualification$' -count=1 -v
```

The retained test proves eight-writer convergence, changed-review conflict,
all seven injected transaction rollbacks, append-only enforcement, forged
normalized-row rejection, exact restart reconstruction (including nested
references/results), and nonempty rollback refusal. Its exact parent rows are
synthetic canonical artifacts inserted without replaying the independently
qualified upstream child graphs; migration-96 foreign keys and digest checks
remain enabled for the OVR-602 write itself.

Migration 96 is empty-only reversible. An empty database passed
`96 -> 95 -> 96`; retained evidence intentionally refuses rollback. Inspect
all retained tables before recovery. Never delete or update evidence to make a
rollback pass; restore a verified pre-migration backup instead.

## Qualification status

- `VERIFIED_LOCAL`: deterministic artifacts, closed vocabularies, exact parent
  IDs/digests, normal migration-96 FK/digest enforcement, normalized graph
  reconstruction, concurrency, atomicity, append-only behavior, empty rollback,
  retained rollback refusal, and local build/test/static/production-verifier
  gates recorded in the OVR-602 closure.
- `BLOCKED_EXTERNAL`: provider/model/search invocation, licensed source
  acquisition, independent human review, experiment declaration/execution,
  shared migration, deployment, scheduling, capital reservation, broker
  routing, and live trading.
