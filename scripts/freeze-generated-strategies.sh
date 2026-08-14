#!/bin/sh
set -eu

db_container=${AUGR_DB_CONTAINER:-augr-postgres-1}
db_user=${AUGR_DB_USER:-postgres}
db_name=${AUGR_DB_NAME:-tradingagent}
mode=${1:-plan}
reason=${2:-phase-0 generated strategy quarantine}

usage() {
  echo "usage: $0 plan | apply [reason]" >&2
  echo "apply requires AUGR_FREEZE_CONFIRM=freeze-generated-strategies" >&2
}

db() {
  docker exec "$db_container" psql -X -v ON_ERROR_STOP=1 -U "$db_user" -d "$db_name" -P pager=off "$@"
}

selection="
  is_paper
  and (
    name like 'discovery: %'
    or name like 'options: %'
    or name like 'auto: %'
    or coalesce(config, '{}'::jsonb) ? 'research_lifecycle'
  )
  and (status <> 'inactive' or coalesce(schedule_cron, '') <> '')
"

case "$mode" in
  plan)
    db --set=default_transaction_read_only=on \
      -c "select market_type, status, count(*) as candidates, count(*) filter (where coalesce(schedule_cron,'') <> '') as scheduled from strategies where $selection group by market_type,status order by market_type,status;" \
      -c "select id, market_type, status, ticker, name, schedule_cron from strategies where $selection order by market_type,name,id;"
    echo "plan only; no strategy rows changed"
    ;;
  apply)
    if [ "${AUGR_FREEZE_CONFIRM:-}" != "freeze-generated-strategies" ]; then
      echo "refusing apply without AUGR_FREEZE_CONFIRM=freeze-generated-strategies" >&2
      exit 2
    fi
    db -v freeze_reason="$reason" -c "
      begin;
      update strategies
      set config = jsonb_set(
            case when jsonb_typeof(config) = 'object' then config else '{}'::jsonb end,
            '{research_lifecycle}',
            jsonb_build_object(
              'stage', 'idea',
              'activation', 'manual_promotion_only',
              'auto_activation_blocked', true,
              'quarantined_at', now(),
              'quarantined_reason', :'freeze_reason',
              'quarantined_from_status', status,
              'quarantined_from_schedule', coalesce(schedule_cron, '')
            ),
            true
          ),
          status = 'inactive',
          is_active = false,
          schedule_cron = '',
          skip_next_run = false,
          updated_at = now()
      where $selection;
      commit;" \
      -c "select count(*) as remaining_generated_active_or_scheduled from strategies where $selection;"
    echo "generated paper strategies quarantined; prior status and schedule are preserved in config.research_lifecycle"
    ;;
  *)
    usage
    exit 2
    ;;
esac
