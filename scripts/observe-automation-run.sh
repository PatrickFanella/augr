#!/bin/sh
set -eu

job="${1:-}"
not_before="${2:-}"
label="${3:-${job:-automation}-run}"

case "$job" in
  ""|*[!A-Za-z0-9._-]*) echo "invalid automation job: $job" >&2; exit 2 ;;
esac
case "$label" in
  ""|*[!A-Za-z0-9._-]*) echo "invalid observation label: $label" >&2; exit 2 ;;
esac

if ! target_epoch=$(date -u -d "$not_before" +%s 2>/dev/null); then
  echo "invalid not-before timestamp: $not_before" >&2
  exit 2
fi
now_epoch=$(date -u +%s)
if [ "$now_epoch" -ge "$target_epoch" ]; then
  echo "not-before timestamp must be in the future" >&2
  exit 2
fi
since_sql=$(date -u -d "@$target_epoch" '+%Y-%m-%d %H:%M:%S+00')

timeout_seconds="${OBSERVATION_TIMEOUT_SECONDS:-7200}"
case "$timeout_seconds" in
  ""|*[!0-9]*) echo "invalid observation timeout: $timeout_seconds" >&2; exit 2 ;;
esac
test "$timeout_seconds" -gt 0 || { echo "observation timeout must be positive" >&2; exit 2; }
lead_seconds="${OBSERVATION_LEAD_SECONDS:-10}"
case "$lead_seconds" in
  ""|*[!0-9]*) echo "invalid observation lead: $lead_seconds" >&2; exit 2 ;;
esac
test "$lead_seconds" -gt 0 || { echo "observation lead must be positive" >&2; exit 2; }

repo=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
compose_file="${AUGR_COMPOSE_FILE:-$repo/docker-compose.nuc.yml}"
base_url="${AUGR_BASE_URL:-http://10.0.0.56:3030}"
observed_stamp=$(date -u '+%Y%m%dT%H%M%SZ')
report="${OBSERVATION_REPORT:-$repo/docs/reports/${observed_stamp}-${label}-observation.txt}"
report_dir=$(dirname -- "$report")
mkdir -p "$report_dir"
tmp=$(mktemp "${report}.tmp.XXXXXX")
trap 'rm -f "$tmp"' EXIT HUP INT TERM

cd "$repo"

db_query() {
  docker compose -f "$compose_file" exec -T postgres sh -lc \
    'psql -X -qAt -v ON_ERROR_STOP=1 -U "$POSTGRES_USER" -d "$POSTGRES_DB" -c "$1"' sh "$1"
}

snapshot() {
  phase="$1"
  db_query "
    SELECT '${phase}|schema|' || version || '|' || dirty FROM schema_migrations;
    SELECT '${phase}|queues|' ||
      (SELECT count(*) FROM automation_job_runs WHERE completed_at IS NULL) || '|' ||
      (SELECT count(*) FROM pipeline_runs WHERE completed_at IS NULL);
    SELECT '${phase}|financial|' ||
      (SELECT count(*) FROM orders) || '|' ||
      (SELECT count(*) FROM trades) || '|' ||
      (SELECT count(*) FROM positions WHERE closed_at IS NULL);
    SELECT '${phase}|domain_counts|' ||
      (SELECT count(*) FROM strategies) || '|' ||
      (SELECT count(*) FROM backtest_configs) || '|' ||
      (SELECT count(*) FROM trade_decisions);"
}

{
  echo "Augr prospective automation observation: $label"
  echo "job=$job"
  echo "not_before=$(date -u -d "@$target_epoch" '+%Y-%m-%dT%H:%M:%SZ')"
  echo "armed_at=$(date -u '+%Y-%m-%dT%H:%M:%SZ')"
  echo "raw errors, result values, message text, query strings, and provider bodies omitted"

  precheck_epoch=$((target_epoch - lead_seconds))
  while :; do
    current_epoch=$(date -u +%s)
    [ "$current_epoch" -ge "$precheck_epoch" ] && break
    remaining=$((precheck_epoch - current_epoch))
    if [ "$remaining" -gt 30 ]; then
      sleep 30
    else
      sleep "$remaining"
    fi
  done

  echo "precheck=$(date -u '+%Y-%m-%dT%H:%M:%SZ')"
  curl -fsS "$base_url/healthz"
  echo
  docker compose -f "$compose_file" ps --format json |
    jq -c '{name: .Name, service: .Service, image: .Image, state: .State, health: .Health, status: .Status}'
  snapshot before

  deadline=$((target_epoch + timeout_seconds))
  run_id=""
  while :; do
    row=$(db_query "
      SELECT id || '|' || status || '|' || started_at || '|' ||
        COALESCE(completed_at::text, '') || '|error_present=' ||
        (error IS NOT NULL)::text || '|result_shape=' ||
        COALESCE(jsonb_typeof(result), 'null') || '|result_keys=' ||
        CASE WHEN jsonb_typeof(result) = 'object' THEN COALESCE(
          (SELECT string_agg(key, ',' ORDER BY key)
           FROM jsonb_object_keys(result) AS key), '') ELSE '' END
      FROM automation_job_runs
      WHERE job_name = '$job' AND started_at >= TIMESTAMPTZ '$since_sql'
      ORDER BY started_at ASC LIMIT 1;")
    if [ -n "$row" ]; then
      observed_id=${row%%|*}
      if [ -z "$run_id" ]; then
        run_id="$observed_id"
      elif [ "$observed_id" != "$run_id" ]; then
        echo "run identity changed: expected $run_id got $observed_id" >&2
        exit 1
      fi
      echo "state|$row"
      completed=$(db_query "SELECT (completed_at IS NOT NULL)::text FROM automation_job_runs WHERE id = '$run_id'::uuid;")
      [ "$completed" = "true" ] && break
    fi
    if [ "$(date -u +%s)" -ge "$deadline" ]; then
      echo "timed out waiting for terminal run" >&2
      exit 1
    fi
    sleep 1
  done

  echo "postcheck=$(date -u '+%Y-%m-%dT%H:%M:%SZ')"
  snapshot after
  echo "sanitized_warning_error_metadata"
  docker compose -f "$compose_file" logs --no-log-prefix --since="$not_before" app 2>&1 |
    jq -Rrc --arg job "$job" '
      fromjson?
      | select(. != null)
      | select((.job_name // .job // "") == $job)
      | select(((.level // "") | ascii_upcase) == "WARN" or ((.level // "") | ascii_upcase) == "ERROR")
      | {
          time: (.time // .timestamp // null),
          level: (.level // null),
          component: (.component // null),
          job: (.job_name // .job // null),
          status: (.status // null),
          ticker: (.ticker // null),
          run_id: (.run_id // null),
          strategy_id: (.strategy_id // null)
        }' || true
} >"$tmp" 2>&1

mv "$tmp" "$report"
trap - EXIT HUP INT TERM
printf '%s\n' "$report"
