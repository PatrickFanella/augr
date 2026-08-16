#!/usr/bin/env bash
set -euo pipefail

usage() {
  echo "usage: $0 SOURCE_HOST:/absolute/path" >&2
  exit 64
}

[[ $# -eq 1 ]] || usage

source_location=$1
source_host=${source_location%%:*}
source_path=${source_location#*:}
db_container=${AUGR_POSTGRES_CONTAINER:-augr-postgres-1}
app_container=${AUGR_APP_CONTAINER:-augr-app-1}
target_table=${AUGR_KALSHI_TARGET_TABLE:-public.kalshi_market_snapshots}
postgres_user=${AUGR_POSTGRES_USER:-}
postgres_db=${AUGR_POSTGRES_DB:-}
docker_env=()

[[ $source_location == *:* ]] || usage
[[ $source_host =~ ^[A-Za-z0-9._@-]+$ ]] || {
  echo "invalid source host" >&2
  exit 64
}
[[ $source_path =~ ^/[A-Za-z0-9._/-]+$ ]] || {
  echo "source must be an absolute path containing only safe characters" >&2
  exit 64
}
[[ $target_table =~ ^[a-z_][a-z0-9_]*\.[a-z_][a-z0-9_]*$ ]] || {
  echo "invalid target table" >&2
  exit 64
}
if [[ -n $postgres_user ]]; then
  [[ $postgres_user =~ ^[A-Za-z_][A-Za-z0-9_-]*$ ]] || usage
  docker_env+=(-e "POSTGRES_USER=$postgres_user")
fi
if [[ -n $postgres_db ]]; then
  [[ $postgres_db =~ ^[A-Za-z_][A-Za-z0-9_-]*$ ]] || usage
  docker_env+=(-e "POSTGRES_DB=$postgres_db")
fi

source_command() {
  if [[ $source_host == local ]]; then
    sh -c "$1"
  else
    ssh "$source_host" "$1"
  fi
}

if [[ $(docker inspect -f '{{.State.Running}}' "$app_container" 2>/dev/null || true) == true ]]; then
  echo "refusing restore while $app_container is running" >&2
  exit 1
fi
docker inspect "$db_container" >/dev/null
source_command "test -s $source_path/manifest.csv"

psql_query() {
  local sql=$1
  docker exec "${docker_env[@]}" "$db_container" sh -ec \
    'exec psql -X -qAt -v ON_ERROR_STOP=1 -U "$POSTGRES_USER" -d "$POSTGRES_DB" -c "$1"' \
    sh "$sql" | tr -d '[:space:]'
}

hypertable_count=$(psql_query \
  "SELECT count(*) FROM timescaledb_information.hypertables WHERE hypertable_schema || '.' || hypertable_name = '$target_table';")
[[ $hypertable_count == 1 ]] || {
  echo "$target_table is not a Timescale hypertable" >&2
  exit 1
}

while IFS=, read -r utc_day expected_rows expected_bytes expected_sha256; do
  [[ $utc_day == utc_day ]] && continue
  [[ $utc_day =~ ^[0-9]{4}-[0-9]{2}-[0-9]{2}$ ]] || {
    echo "invalid manifest date" >&2
    exit 1
  }
  [[ $expected_rows =~ ^[0-9]+$ && $expected_bytes =~ ^[0-9]+$ && $expected_sha256 =~ ^[0-9a-f]{64}$ ]] || {
    echo "invalid manifest record for $utc_day" >&2
    exit 1
  }

  next=$(date -u -d "$utc_day + 1 day" +%F)
  archive_path="$source_path/kalshi_market_snapshots_${utc_day}.csv.zst"
  remote_stats=$(source_command \
    "bytes=\$(stat -c %s $archive_path); hash=\$(sha256sum $archive_path | awk '{print \$1}'); printf '%s,%s' \"\$bytes\" \"\$hash\"")
  [[ $remote_stats == "$expected_bytes,$expected_sha256" ]] || {
    echo "archive verification failed for $utc_day" >&2
    exit 1
  }

  predicate="captured_at >= TIMESTAMPTZ '$utc_day 00:00:00+00' AND captured_at < TIMESTAMPTZ '$next 00:00:00+00'"
  existing_rows=$(psql_query "SELECT count(*) FROM $target_table WHERE $predicate;")
  if [[ $existing_rows == "$expected_rows" ]]; then
    echo "verified existing $utc_day ($existing_rows rows)"
    continue
  fi
  [[ $existing_rows == 0 ]] || {
    echo "target day $utc_day has $existing_rows rows; expected zero or $expected_rows" >&2
    exit 1
  }

  source_command "zstd -dc -- $archive_path" |
    docker exec -i "${docker_env[@]}" "$db_container" sh -ec \
      'exec psql -X -q -v ON_ERROR_STOP=1 -U "$POSTGRES_USER" -d "$POSTGRES_DB" -c "$1"' \
      sh "COPY $target_table (id, provider, environment, source_url, ticker, title, status, yes_bid, yes_ask, no_bid, no_ask, volume, open_interest, close_time, raw, captured_at) FROM STDIN WITH (FORMAT csv);"

  restored_rows=$(psql_query "SELECT count(*) FROM $target_table WHERE $predicate;")
  [[ $restored_rows == "$expected_rows" ]] || {
    echo "row parity failed for $utc_day: restored $restored_rows, expected $expected_rows" >&2
    exit 1
  }

  psql_query "SELECT count(compress_chunk(chunk, if_not_compressed => TRUE)) FROM show_chunks('$target_table', older_than => now() - INTERVAL '7 days', newer_than => TIMESTAMPTZ '$utc_day 00:00:00+00') AS chunk WHERE chunk::text IN (SELECT format('%I.%I', chunk_schema, chunk_name) FROM timescaledb_information.chunks WHERE hypertable_schema || '.' || hypertable_name = '$target_table' AND range_start >= TIMESTAMPTZ '$utc_day 00:00:00+00' AND range_end <= TIMESTAMPTZ '$next 00:00:00+00');" >/dev/null
  echo "restored $utc_day ($restored_rows rows)"
done < <(source_command "cat -- $source_path/manifest.csv")

echo "restore and row-parity verification complete"
