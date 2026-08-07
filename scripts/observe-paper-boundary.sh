#!/bin/sh
set -eu

label="${1:-paper-boundary}"
case "$label" in
  ""|*[!A-Za-z0-9._-]*) echo "invalid observation label: $label" >&2; exit 2 ;;
esac

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
{
  echo "Augr paper-trading observation: $label"
  date -u '+Observed at: %Y-%m-%d %H:%M:%S UTC'
  TZ=America/New_York date '+Observed at: %Y-%m-%d %H:%M:%S %Z'
  echo
  echo "Service health"
  curl -fsS "$base_url/health"
  echo
  docker compose -f "$compose_file" ps
  echo
  echo "Operational database state"
  docker compose -f "$compose_file" exec -T postgres sh -lc \
    'psql -U "$POSTGRES_USER" -d "$POSTGRES_DB" -P pager=off -c "
      SELECT s.market_type::text, r.status, count(*)
      FROM pipeline_runs r
      JOIN strategies s ON s.id = r.strategy_id
      WHERE r.started_at > NOW() - INTERVAL '"'"'30 minutes'"'"'
      GROUP BY 1,2 ORDER BY 1,2;
      SELECT o.market_type::text, o.status, count(*) orders,
             COALESCE(sum(o.quantity * COALESCE(o.filled_avg_price, o.limit_price, 0)), 0) notional
      FROM orders o
      WHERE o.created_at > NOW() - INTERVAL '"'"'30 minutes'"'"'
      GROUP BY 1,2 ORDER BY 1,2;
      SELECT count(*) open_positions,
             COALESCE(sum(abs(quantity * COALESCE(current_price, avg_entry, 0))), 0) gross_notional
      FROM positions WHERE closed_at IS NULL;
      SELECT count(*) active_breakers FROM risk_breaker_state WHERE reset_at IS NULL;
      SELECT job_name, status, started_at, completed_at, consecutive_failures
      FROM automation_job_runs
      WHERE started_at > NOW() - INTERVAL '"'"'30 minutes'"'"'
      ORDER BY started_at DESC;"'
  echo
  echo "Recent warning/error metadata (raw errors and provider bodies omitted)"
  docker compose -f "$compose_file" logs --no-log-prefix --since=30m app 2>&1 |
    jq -Rrc '
      fromjson?
      | select(. != null)
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
