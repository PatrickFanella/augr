#!/bin/sh
set -eu

repo_dir=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
db_container=${AUGR_DB_CONTAINER:-augr-postgres-1}
app_container=${AUGR_APP_CONTAINER:-augr-app-1}
db_user=${AUGR_DB_USER:-postgres}
db_name=${AUGR_DB_NAME:-tradingagent}
output=${1:-}

usage() {
  echo "usage: $0 [output-file]" >&2
  echo "environment overrides: AUGR_DB_CONTAINER AUGR_APP_CONTAINER AUGR_DB_USER AUGR_DB_NAME" >&2
}

case ${output:-} in
  -h|--help)
    usage
    exit 0
    ;;
esac

if ! docker inspect "$db_container" >/dev/null 2>&1; then
  echo "database container not found: $db_container" >&2
  exit 1
fi

scratch_dir=$(mktemp -d)
trap 'rm -rf "$scratch_dir"' EXIT HUP INT TERM
snapshot="$scratch_dir/phase-0-baseline.txt"

db() {
  docker exec "$db_container" psql -X -v ON_ERROR_STOP=1 \
    -U "$db_user" -d "$db_name" \
    --set=default_transaction_read_only=on \
    -P pager=off "$@"
}

has_table() {
  table_name=$1
  [ "$(db -Atc "select to_regclass('public.$table_name') is not null;" 2>/dev/null)" = "t" ]
}

safe_runtime_config() {
  if ! docker inspect "$app_container" >/dev/null 2>&1; then
    echo "app_container=unavailable:$app_container"
    return
  fi
  for key in \
    APP_ENV \
    ENABLE_LIVE_TRADING \
    ENABLE_SCHEDULER \
    ENABLE_TICKER_DISCOVERY \
    ENABLE_POLYMARKET_AUTOMATION \
    ALPACA_PAPER_MODE \
    BINANCE_PAPER_MODE \
    KALSHI_DEMO \
    KALSHI_DRY_RUN \
    KALSHI_AUTO_EXITS_ENABLED \
    PAPER_EVALUATION_MODE \
    PAPER_INITIAL_CAPITAL \
    PAPER_BUYING_POWER_MULTIPLIER \
    PAPER_SLIPPAGE_BPS \
    PAPER_FEE_PCT \
    RISK_MAX_POSITION_SIZE_PCT \
    RISK_MAX_DAILY_LOSS_PCT \
    RISK_MAX_DRAWDOWN_PCT \
    RISK_MAX_OPEN_POSITIONS \
    RISK_MAX_KALSHI_EXPOSURE_PCT \
    RISK_CIRCUIT_BREAKER_THRESHOLD \
    RISK_CIRCUIT_BREAKER_COOLDOWN
  do
    value=$(docker exec "$app_container" printenv "$key" 2>/dev/null || true)
    if [ -n "$value" ]; then
      echo "$key=$value"
    else
      echo "$key=<unset; application default applies>"
    fi
  done
}

{
  echo "Augr Phase 0 baseline"
  echo "generated_at=$(date -u +%Y-%m-%dT%H:%M:%SZ)"
  echo "hostname=$(hostname)"
  echo
  echo "[repository]"
  echo "repository=$repo_dir"
  echo "commit=$(git -C "$repo_dir" rev-parse HEAD)"
  echo "branch=$(git -C "$repo_dir" branch --show-current)"
  echo "origin=$(git -C "$repo_dir" remote get-url origin)"
  echo "tracked_tree=$(git -C "$repo_dir" rev-parse HEAD^{tree})"
  echo "highest_code_migration=$(find "$repo_dir/migrations" -maxdepth 1 -type f -name '*.up.sql' -printf '%f\n' | sort -V | tail -1)"
  echo "worktree_status_begin"
  git -C "$repo_dir" status --short
  echo "worktree_status_end"
  echo
  echo "[runtime images]"
  if docker inspect "$app_container" >/dev/null 2>&1; then
    docker inspect -f 'app_container={{.Name}} image={{.Config.Image}} image_id={{.Image}} started={{.State.StartedAt}} status={{.State.Status}}' "$app_container"
  else
    echo "app_container=unavailable:$app_container"
  fi
  docker inspect -f 'db_container={{.Name}} image={{.Config.Image}} image_id={{.Image}} started={{.State.StartedAt}} status={{.State.Status}}' "$db_container"
  echo
  echo "[safe runtime configuration]"
  safe_runtime_config
  echo
  echo "[database identity and schema]"
  db -c "select current_database() as database, current_user as database_user, current_setting('server_version') as postgres_version;" \
    -c "select max(version) as schema_version, bool_or(dirty) as dirty from schema_migrations;" \
    -c "select extname, extversion from pg_extension order by extname;"
  echo
  echo "[strategy inventory]"
  db -c "
    select market_type, status, is_paper,
           count(*) as strategies,
           count(*) filter (where coalesce(schedule_cron,'') <> '') as scheduled,
           count(*) filter (where active_thesis is not null and active_thesis <> '{}'::jsonb) as with_thesis,
           count(*) filter (where name like 'discovery: %' or name like 'options: %' or name like 'auto: %') as generated
    from strategies
    group by market_type, status, is_paper
    order by market_type, status, is_paper;" \
    -c "
    select id, market_type, status, is_paper, ticker, name, schedule_cron
    from strategies
    where status='active' or coalesce(schedule_cron,'') <> ''
    order by market_type, name, id;"
  echo
  echo "[pipeline outcomes]"
  db -c "
    select coalesce(s.market_type::text,'unknown') as market_type,
           r.status, coalesce(nullif(r.signal,''),'none') as signal,
           count(*) as runs,
           min(r.started_at) as first_run,
           max(r.started_at) as last_run
    from pipeline_runs r
    left join strategies s on s.id=r.strategy_id
    group by coalesce(s.market_type::text,'unknown'), r.status, coalesce(nullif(r.signal,''),'none')
    order by market_type, r.status, signal;"
  echo
  echo "[orders, trades, and positions]"
  db -c "
    select market_type, status, broker, count(*) as orders,
           round(coalesce(sum(quantity),0),4) as requested_quantity,
           round(coalesce(sum(filled_quantity),0),4) as filled_quantity
    from orders
    group by market_type, status, broker
    order by market_type, broker, status;" \
    -c "
    select asset_class, coalesce(open_close,'unspecified') as open_close,
           count(*) as trades,
           round(coalesce(sum(quantity * price * case when asset_class='option' then contract_multiplier else 1 end),0),2) as notional,
           round(coalesce(sum(fee),0),4) as fees,
           min(executed_at) as first_trade,
           max(executed_at) as last_trade
    from trades
    group by asset_class, coalesce(open_close,'unspecified')
    order by asset_class, open_close;" \
    -c "
    select coalesce(s.market_type::text,'unknown') as market_type, p.asset_class,
           count(*) filter (where closed_at is null) as open_positions,
           count(*) filter (where closed_at is not null) as closed_positions,
           round(coalesce(sum(realized_pnl),0),2) as realized_pnl,
           round(coalesce(sum(unrealized_pnl) filter (where closed_at is null),0),2) as open_unrealized_pnl,
           round(coalesce(sum(quantity * coalesce(current_price,avg_entry) * case when asset_class='option' then contract_multiplier else 1 end) filter (where closed_at is null),0),2) as open_marked_notional
    from positions p
    left join strategies s on s.id=p.strategy_id
    group by coalesce(s.market_type::text,'unknown'), p.asset_class
    order by market_type, p.asset_class;"
  echo
  echo "[reconciliation indicators]"
  db -c "
    select count(*) as filled_orders_without_trade
    from orders o
    where o.status='filled'
      and not exists (select 1 from trades t where t.order_id=o.id);" \
    -c "
    select count(*) as trades_without_filled_order
    from trades t
    left join orders o on o.id=t.order_id
    where o.id is null or o.status <> 'filled';" \
    -c "
    select count(*) as open_positions_without_mark
    from positions
    where closed_at is null and current_price is null;"
  echo
  echo "[risk state]"
  db -c "select scope, tripped_at, reason, reset_at from risk_breaker_state order by scope;" \
    -c "select id, kill_switch, market_kill_switches, updated_at from risk_state order by id;"
  echo
  echo "[automation controls and recent runs]"
  db -c "select job_name, enabled, updated_by, updated_at from automation_job_controls order by job_name;" \
    -c "
    select job_name, status, count(*) as runs, max(started_at) as last_started_at,
           max(completed_at) as last_completed_at
    from automation_job_runs
    group by job_name, status
    order by job_name, status;"
  echo
  echo "[copy trading]"
  if has_table copy_leaders; then
    db -c "select status, count(*) as leaders from copy_leaders group by status order by status;" \
      -c "select status, count(*) as subscriptions from copy_subscriptions group by status order by status;" \
      -c "select status, count(*) as intents, round(coalesce(sum(target_notional),0),2) as target_notional from copy_trade_intents group by status order by status;"
  else
    echo "copy_trading_schema=not_deployed"
  fi
} > "$snapshot" 2>&1

# psql's aligned tables pad cells with spaces. Normalize only line endings so
# committed snapshots remain readable and pass strict Git whitespace checks.
normalized_snapshot="$scratch_dir/phase-0-baseline.normalized.txt"
sed 's/[[:space:]]*$//' "$snapshot" > "$normalized_snapshot"
mv "$normalized_snapshot" "$snapshot"

if [ -n "$output" ]; then
  output_dir=$(dirname -- "$output")
  mkdir -p "$output_dir"
  cp "$snapshot" "$output"
  sha256sum "$output" > "$output.sha256"
  cat "$output"
  echo "baseline=$output"
  echo "sha256=$output.sha256"
else
  cat "$snapshot"
fi
