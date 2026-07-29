#!/bin/sh
set -eu

label="${1:-paper-boundary}"
case "$label" in
  kalshi-boundary|alpaca-sunday-open) ;;
  *) echo "unsupported observation label: $label" >&2; exit 2 ;;
esac

repo="/home/onnwee/Projects/patrickfanella/augr"
report="$repo/docs/reports/2026-07-26-${label}-observation.txt"
tmp="${report}.tmp"

cd "$repo"
{
  echo "Augr paper-trading observation: $label"
  date -u '+Observed at: %Y-%m-%d %H:%M:%S UTC'
  TZ=America/New_York date '+Observed at: %Y-%m-%d %H:%M:%S %Z'
  echo
  echo "Service health"
  curl -fsS http://10.0.0.56:3030/health
  echo
  docker compose -f docker-compose.nuc.yml ps
  echo
  echo "Operational database state"
  docker compose -f docker-compose.nuc.yml exec -T postgres sh -lc \
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
  echo "Recent warnings and errors"
  docker compose -f docker-compose.nuc.yml logs --since=30m app 2>&1 |
    grep -E '"level":"(WARN|ERROR)"|strategy execution failed|pipeline execution failed|job failed' || true
} >"$tmp" 2>&1

mv "$tmp" "$report"
