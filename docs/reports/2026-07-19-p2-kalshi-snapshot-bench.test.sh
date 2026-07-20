#!/usr/bin/env bash
set -euo pipefail

sql_file=${1:?usage: $0 path/to/sql}

if [[ ! -f "$sql_file" ]]; then
  printf 'missing SQL file: %s\n' "$sql_file" >&2
  exit 1
fi

for forbidden in 'INSERT' 'UPDATE' 'DELETE' 'ALTER' 'DROP' 'CREATE' 'REFRESH COLLATION VERSION' 'add_retention_policy' 'drop_chunks' 'compress'; do
  if grep -Ev '^[[:space:]]*--' "$sql_file" | grep -Eiq "(^|[^[:alpha:]])${forbidden}([^[:alpha:]]|$)"; then
    printf 'forbidden token found: %s\n' "$forbidden" >&2
    exit 1
  fi
done

if [[ $(grep -Ec '^EXPLAIN \(ANALYZE, BUFFERS\)$' "$sql_file") -lt 3 ]]; then
  printf 'missing expected EXPLAIN statements\n' >&2
  exit 1
fi

printf 'ok: read-only benchmark SQL validated\n'
