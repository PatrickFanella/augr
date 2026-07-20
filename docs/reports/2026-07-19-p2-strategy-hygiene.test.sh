#!/usr/bin/env bash
set -euo pipefail

sql_file="${1:-docs/reports/2026-07-19-p2-strategy-hygiene.sql}"

if [[ ! -f "$sql_file" ]]; then
  printf 'missing SQL file: %s\n' "$sql_file" >&2
  exit 1
fi

if grep -Eqi '\b(UPDATE|DELETE|INSERT|ALTER|DROP|TRUNCATE|CREATE|GRANT|REVOKE|VACUUM|REINDEX|REFRESH|CALL|DO)\b' "$sql_file"; then
  printf 'forbidden mutation keyword found in %s\n' "$sql_file" >&2
  exit 1
fi

if ! grep -Eqi '^\s*SELECT\b|^\s*WITH\b' "$sql_file"; then
  printf 'SQL file does not appear to contain a read-only query bundle: %s\n' "$sql_file" >&2
  exit 1
fi

printf 'ok: read-only SQL validation passed for %s\n' "$sql_file"
