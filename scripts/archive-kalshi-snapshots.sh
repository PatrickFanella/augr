#!/usr/bin/env bash
set -euo pipefail

usage() {
  echo "usage: $0 DEST_HOST:/absolute/path FROM_DATE THROUGH_DATE" >&2
  exit 64
}

[[ $# -eq 3 ]] || usage

destination=$1
from_date=$2
through_date=$3
db_container=${AUGR_POSTGRES_CONTAINER:-augr-postgres-1}

[[ $destination == *:* ]] || usage
destination_host=${destination%%:*}
destination_path=${destination#*:}

[[ $destination_host =~ ^[A-Za-z0-9._@-]+$ ]] || {
  echo "invalid destination host" >&2
  exit 64
}
[[ $destination_path =~ ^/[A-Za-z0-9._/-]+$ ]] || {
  echo "destination must be an absolute path containing only safe characters" >&2
  exit 64
}
[[ $from_date =~ ^[0-9]{4}-[0-9]{2}-[0-9]{2}$ ]] || usage
[[ $through_date =~ ^[0-9]{4}-[0-9]{2}-[0-9]{2}$ ]] || usage
date -u -d "$from_date" +%F >/dev/null
date -u -d "$through_date" +%F >/dev/null
[[ $from_date < $through_date || $from_date == "$through_date" ]] || {
  echo "FROM_DATE must not be later than THROUGH_DATE" >&2
  exit 64
}
docker inspect "$db_container" >/dev/null
command -v zstd >/dev/null

ssh "$destination_host" "install -d -m 0700 $destination_path"
manifest_tmp="$destination_path/manifest.csv.tmp"
manifest_final="$destination_path/manifest.csv"
printf '%s\n' 'utc_day,row_count,compressed_bytes,sha256' |
  ssh "$destination_host" "dd of=$manifest_tmp status=none"

current=$from_date
while :; do
  next=$(date -u -d "$current + 1 day" +%F)
  archive_name="kalshi_market_snapshots_${current}.csv.zst"
  archive_path="$destination_path/$archive_name"
  archive_tmp="$archive_path.tmp"
  predicate="captured_at >= TIMESTAMPTZ '$current 00:00:00+00' AND captured_at < TIMESTAMPTZ '$next 00:00:00+00'"
  count_sql="SELECT count(*) FROM public.kalshi_market_snapshots WHERE $predicate;"
  copy_sql="COPY (SELECT id, provider, environment, source_url, ticker, title, status, yes_bid, yes_ask, no_bid, no_ask, volume, open_interest, close_time, raw, captured_at FROM public.kalshi_market_snapshots WHERE $predicate) TO STDOUT WITH (FORMAT csv);"

  row_count=$(docker exec "$db_container" sh -ec \
    'exec psql -X -qAt -v ON_ERROR_STOP=1 -U "$POSTGRES_USER" -d "$POSTGRES_DB" -c "$1"' \
    sh "$count_sql" | tr -d '[:space:]')
  [[ $row_count =~ ^[0-9]+$ ]] || {
    echo "invalid row count for $current" >&2
    exit 1
  }

  if ! ssh "$destination_host" "test -s $archive_path"; then
    ssh "$destination_host" "rm -f -- $archive_tmp"
    docker exec "$db_container" sh -ec \
      'exec psql -X -q -v ON_ERROR_STOP=1 -U "$POSTGRES_USER" -d "$POSTGRES_DB" -c "$1"' \
      sh "$copy_sql" |
      zstd -T0 -6 |
      ssh "$destination_host" "dd of=$archive_tmp status=none"
    ssh "$destination_host" "test -s $archive_tmp && mv -- $archive_tmp $archive_path"
  fi

  archive_stats=$(ssh "$destination_host" \
    "bytes=\$(stat -c %s $archive_path); hash=\$(sha256sum $archive_path | awk '{print \$1}'); printf '%s,%s' \"\$bytes\" \"\$hash\"")
  printf '%s,%s,%s\n' "$current" "$row_count" "$archive_stats" |
    ssh "$destination_host" "dd of=$manifest_tmp oflag=append conv=notrunc status=none"
  echo "archived $current ($row_count rows)"

  [[ $current == "$through_date" ]] && break
  current=$next
done

ssh "$destination_host" "mv -- $manifest_tmp $manifest_final"
echo "archive complete: $destination_host:$manifest_final"
