#!/bin/sh
set -eu

repo_dir=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
compose_file=${AUGR_COMPOSE_FILE:-$repo_dir/docker-compose.nuc.yml}
state_dir=${AUGR_EVALUATION_DIR:-$repo_dir/var/paper-week}
api_url=${AUGR_API_URL:-http://10.0.0.56:${AUGR_API_PORT:-3030}}
prometheus_url=${AUGR_PROMETHEUS_URL:-http://10.0.0.56:9090}
db_user=${POSTGRES_USER:-postgres}
db_name=${POSTGRES_DB:-tradingagent}

usage() {
  echo "usage: $0 init|status|snapshot"
}

db() {
  docker compose -f "$compose_file" exec -T postgres \
    psql -X -v ON_ERROR_STOP=1 -U "$db_user" -d "$db_name" "$@"
}

require_window() {
  if [ ! -f "$state_dir/start.env" ]; then
    echo "evaluation window is not initialized; run: $0 init" >&2
    exit 1
  fi
  # Generated locally by this script; contains only timestamp and commit SHA.
  . "$state_dir/start.env"
  if [ ! -s "$state_dir/cohort.ids" ]; then
    echo "evaluation cohort is missing: $state_dir/cohort.ids" >&2
    exit 1
  fi
  EVALUATION_COHORT=$(paste -sd, "$state_dir/cohort.ids")
}

init_window() {
  mkdir -p "$state_dir/snapshots"
  if [ -f "$state_dir/start.env" ]; then
    echo "evaluation window already exists: $state_dir/start.env" >&2
    exit 1
  fi
  start=$(date -u +%Y-%m-%dT%H:%M:%SZ)
  commit=$(git -C "$repo_dir" rev-parse HEAD)
  {
    echo "EVALUATION_START=$start"
    echo "EVALUATION_COMMIT=$commit"
  } > "$state_dir/start.env"
  db -Atc "select id from strategies where is_paper and status='active' order by id;" > "$state_dir/cohort.ids"
  echo "initialized paper evaluation at $start ($commit)"
  snapshot
}

status() {
  require_window
  echo "evaluation_start=$EVALUATION_START"
  echo "evaluation_commit=$EVALUATION_COMMIT"
  echo "current_commit=$(git -C "$repo_dir" rev-parse HEAD)"
  echo "cohort_size=$(wc -l < "$state_dir/cohort.ids" | tr -d ' ')"
  echo "cohort_sha256=$(sha256sum "$state_dir/cohort.ids" | awk '{print $1}')"
  echo "live_trading=$(awk -F= '$1=="ENABLE_LIVE_TRADING"{print $2}' "$repo_dir/.env" | tail -1)"
  curl -fsS "$api_url/healthz"
  echo
  target_json=$(curl -fsS "$prometheus_url/api/v1/targets")
  echo "$target_json" | jq -r '.data.activeTargets[] | select(.labels.job == "augr-api") | "prometheus_target=\(.health) error=\(.lastError)"'
  db -Atc "select 'schema_version=' || coalesce(max(version),0) from schema_migrations;"
}

snapshot() {
  require_window
  stamp=$(date -u +%Y%m%dT%H%M%SZ)
  output="$state_dir/snapshots/$stamp.txt"
  {
    echo "Augr paper evaluation snapshot"
    echo "generated_at=$stamp"
    echo "window_start=$EVALUATION_START"
    echo "window_commit=$EVALUATION_COMMIT"
    echo "current_commit=$(git -C "$repo_dir" rev-parse HEAD)"
    echo "cohort_size=$(wc -l < "$state_dir/cohort.ids" | tr -d ' ')"
    echo "cohort_sha256=$(sha256sum "$state_dir/cohort.ids" | awk '{print $1}')"
    echo
    echo "[runtime]"
    status
    echo
    echo "[strategy cohort]"
    db -P pager=off -c "
      select market_type, count(*) as active_paper_strategies,
             count(*) filter (where schedule_cron is not null and schedule_cron <> '') as scheduled
      from strategies where id = any(string_to_array('$EVALUATION_COHORT', ',')::uuid[])
      group by market_type order by market_type;"
    echo "[pipeline outcomes]"
    db -P pager=off -c "
      select s.market_type, r.status, nullif(r.signal,'') as signal, count(*)
      from pipeline_runs r join strategies s on s.id=r.strategy_id
      where r.started_at >= '$EVALUATION_START'::timestamptz
        and r.strategy_id = any(string_to_array('$EVALUATION_COHORT', ',')::uuid[])
      group by s.market_type,r.status,nullif(r.signal,'')
      order by s.market_type,r.status,signal;"
    echo "[orders and fills]"
    db -P pager=off -c "
      select market_type,status,count(*) as orders,
             round(sum(filled_quantity)::numeric,4) as filled_quantity,
             round(sum(quantity)::numeric,4) as requested_quantity,
             round((sum(filled_quantity)/nullif(sum(quantity),0)*100)::numeric,2) as fill_pct
      from orders where created_at >= '$EVALUATION_START'::timestamptz
        and strategy_id = any(string_to_array('$EVALUATION_COHORT', ',')::uuid[])
      group by market_type,status order by market_type,status;"
    echo "[trades and positions]"
    db -P pager=off -c "
      select t.asset_class,coalesce(t.open_close,'unspecified') as intent,count(*) as trades,
             round(sum(t.quantity*t.price*case when t.asset_class='option' then t.contract_multiplier else 1 end)::numeric,2) as notional,
             round(sum(t.fee)::numeric,2) as fees
      from trades t left join orders o on o.id=t.order_id left join positions p on p.id=t.position_id
      where t.executed_at >= '$EVALUATION_START'::timestamptz
        and coalesce(o.strategy_id,p.strategy_id) = any(string_to_array('$EVALUATION_COHORT', ',')::uuid[])
      group by t.asset_class,coalesce(t.open_close,'unspecified') order by t.asset_class,intent;" -c "
      select asset_class, count(*) filter(where closed_at is null) as open_positions,
             count(*) filter(where closed_at is not null) as closed_positions,
             round(coalesce(sum(realized_pnl),0)::numeric,2) as realized_pnl,
             round(coalesce(sum(unrealized_pnl) filter(where closed_at is null),0)::numeric,2) as unrealized_pnl
      from positions where opened_at >= '$EVALUATION_START'::timestamptz
        and strategy_id = any(string_to_array('$EVALUATION_COHORT', ',')::uuid[])
      group by asset_class order by asset_class;"
    echo "[decision journal]"
    db -P pager=off -c "
      select market_type,status,risk_status,count(*) as decisions,
             count(*) filter(where evidence='{}'::jsonb) as missing_evidence,
             count(*) filter(where paper_order_id is null and status='paper_ordered') as missing_paper_order
      from trade_decisions where created_at >= '$EVALUATION_START'::timestamptz
        and strategy_id = any(string_to_array('$EVALUATION_COHORT', ',')::uuid[])
      group by market_type,status,risk_status order by market_type,status,risk_status;" -c "
      select count(*) as decisions,
             count(distinct re.trade_decision_id) as decisions_with_replay,
             round(count(distinct re.trade_decision_id)::numeric/nullif(count(distinct td.id),0)*100,2) as replay_coverage_pct
      from trade_decisions td left join replay_events re on re.trade_decision_id=td.id
      where td.created_at >= '$EVALUATION_START'::timestamptz
        and td.strategy_id = any(string_to_array('$EVALUATION_COHORT', ',')::uuid[]);"
    echo "[reports and LLM provenance]"
    db -P pager=off -c "
      select status,count(*) as reports,
             round(avg(latency_ms)) as avg_latency_ms,
             sum(prompt_tokens+completion_tokens) as tokens,
             round(sum(coalesce((report_json->>'cost_usd')::numeric,0)),4) as recorded_cost_usd
      from report_artifacts where created_at >= '$EVALUATION_START'::timestamptz
        and strategy_id = any(string_to_array('$EVALUATION_COHORT', ',')::uuid[])
      group by status order by status;" -c "
      select coalesce(llm_provider,'none') as provider,coalesce(llm_model,'none') as model,
             count(*) as decisions,round(avg(latency_ms)) as avg_latency_ms,
             sum(coalesce(prompt_tokens,0)+coalesce(completion_tokens,0)) as tokens,
             round(sum(coalesce(cost_usd,0)),4) as cost_usd
      from trade_decisions where created_at >= '$EVALUATION_START'::timestamptz
        and strategy_id = any(string_to_array('$EVALUATION_COHORT', ',')::uuid[])
      group by coalesce(llm_provider,'none'),coalesce(llm_model,'none') order by decisions desc;"
    echo "[recent automation failures]"
    curl -fsS "$api_url/metrics" | awk '/^tradingagent_automation_job_errors_total|^tradingagent_generator_outcomes_total|^tradingagent_.*reconcile.*total|^tradingagent_data_source_.*unixtime/'
  } > "$output"
  cat "$output"
  echo "snapshot=$output"
}

case ${1:-} in
  init) init_window ;;
  status) status ;;
  snapshot) snapshot ;;
  *) usage; exit 2 ;;
esac
