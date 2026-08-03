#!/usr/bin/env bash
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
preflight="$root/docs/reports/2026-07-20-source-index-integrity-repair.preflight.sql"
execute="$root/docs/reports/2026-07-20-source-index-integrity-repair.execute.sql"
reindex="$root/docs/reports/2026-07-20-source-index-integrity-repair.reindex.sql"
fixture="$root/docs/reports/2026-07-20-source-index-integrity-repair.fixture.sql"

python3 - "$preflight" "$execute" "$reindex" <<'PY'
import pathlib, re, sys

preflight, execute, reindex = [pathlib.Path(p).read_text() for p in sys.argv[1:]]

def strip_comments(text: str) -> str:
    text = re.sub(r"--.*?$", "", text, flags=re.M)
    return re.sub(r"/\*.*?\*/", "", text, flags=re.S)

preflight, execute, reindex = map(strip_comments, (preflight, execute, reindex))
if re.search(r"\b(INSERT|UPDATE|DELETE|CREATE|ALTER|DROP|TRUNCATE|REINDEX|DO|COPY|CALL)\b", preflight, re.I):
    raise SystemExit("preflight is not read-only")
for text in (preflight, execute, reindex):
    if re.search(r"REFRESH\s+COLLATION|REINDEX\s+DATABASE", text, re.I):
        raise SystemExit("broader collation maintenance leaked into source repair")
required_execute = [
    "PARTITION BY h.ticker, h.provider, h.timeframe, h.bar_time",
    "PARTITION BY ticker",
    "PARTITION BY guid",
    "LOCK TABLE historical_ohlcv IN ACCESS EXCLUSIVE MODE",
    "repair_delete_counts",
    "external_audit_checksum",
    "source_index_integrity_repair",
]
for token in required_execute:
    if token not in execute:
        raise SystemExit(f"missing execute invariant: {token}")
if execute.index("DELETE FROM universe_tickers") > execute.index("UPDATE universe_tickers"):
    raise SystemExit("universe survivor is updated before duplicate deletion")
if execute.index("DELETE FROM news_feed") > execute.index("UPDATE news_feed"):
    raise SystemExit("news survivor is updated before duplicate deletion")
if execute.count("\\if") != execute.count("\\endif"):
    raise SystemExit("execute psql guards are unbalanced")
if reindex.count("REINDEX TABLE") != 3 or reindex.count("\\if") != reindex.count("\\endif"):
    raise SystemExit("reindex contract mismatch")
PY

bash -n "$root/docs/reports/2026-07-20-source-index-integrity-repair.test.sh"

if [[ "${RUN_DB_FIXTURE:-0}" != "1" ]]; then
  printf 'static validation passed; set RUN_DB_FIXTURE=1 for isolated DB execution\n'
  exit 0
fi

container="augr-index-repair-fixture-$$"
cleanup() { docker rm -f "$container" >/dev/null 2>&1 || true; }
trap cleanup EXIT

docker run -d --name "$container" --network none \
  -e POSTGRES_HOST_AUTH_METHOD=trust -e POSTGRES_DB=fixture \
  pgvector/pgvector:pg17 >/dev/null
for _ in $(seq 1 60); do
  if docker exec "$container" pg_isready -U postgres -d fixture >/dev/null 2>&1; then break; fi
  sleep 1
done
docker exec "$container" pg_isready -U postgres -d fixture >/dev/null
docker exec -i "$container" psql -U postgres -d fixture -v ON_ERROR_STOP=1 < "$fixture" >/dev/null
preflight_output="$(docker exec -i "$container" psql -U postgres -d fixture -v ON_ERROR_STOP=1 < "$preflight")"
[[ "$preflight_output" == *'|t'* ]]
docker exec -i "$container" psql -U postgres -d fixture -v ON_ERROR_STOP=1 \
  -v execute_repair=1 -v repair_approved=1 -v repair_write_quiesce_approved=1 \
  -v repair_physical_backup_approved=1 -v external_audit_verified=1 \
  -v backup_artifact_id=fixture-backup -v backup_checksum=fixture-checksum \
  -v external_audit_checksum=fixture-audit -v operator=fixture \
  -v script_git_hash=fixture-hash -v utc_started_at=2026-07-20T00:00:00Z \
  < "$execute" >/dev/null
docker exec "$container" psql -U postgres -d fixture -v ON_ERROR_STOP=1 \
  -c 'ALTER TABLE historical_ohlcv ADD PRIMARY KEY (ticker, provider, timeframe, bar_time)' \
  -c 'ALTER TABLE universe_tickers ADD PRIMARY KEY (ticker)' \
  -c 'CREATE UNIQUE INDEX idx_news_feed_guid ON news_feed (guid)' >/dev/null
docker exec -i "$container" psql -U postgres -d fixture -v ON_ERROR_STOP=1 \
  -v reindex_approved=1 -v repair_completed=1 -v operator=fixture < "$reindex" >/dev/null
result="$(docker exec "$container" psql -U postgres -d fixture -At -v ON_ERROR_STOP=1 -c \
  "select (select count(*) from historical_ohlcv)=1253 and (select count(*) from universe_tickers)=2 and (select count(*) from news_feed)=40 and (select watch_score from universe_tickers where ticker='BF.B')=8.6698 and (select row(name,exchange,index_group,watch_score,last_scanned,active,created_at,updated_at) from universe_tickers where ticker='BIO.B')=row('Bio-Rad Laboratories, Inc. Class B'::text,'XNYS'::text,'nyse'::text,0.0000::numeric,null::timestamptz,true,timestamptz '2026-05-30 UTC',timestamptz '2026-07-20 UTC') and (select count(*) from audit_log where event_type='source_index_integrity_repair')=1")"
[[ "$result" == "t" ]]
printf 'isolated fixture repair and reindex validation passed\n'
