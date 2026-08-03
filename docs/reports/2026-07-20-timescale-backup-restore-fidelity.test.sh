#!/usr/bin/env bash
set -euo pipefail

sql_file="${1:-docs/reports/2026-07-20-timescale-backup-restore-fidelity.sql}"

if [[ ! -f "$sql_file" ]]; then
  printf 'missing SQL file: %s\n' "$sql_file" >&2
  exit 1
fi

if grep -Eqi '\b(UPDATE|DELETE|INSERT|ALTER|DROP|TRUNCATE|CREATE|GRANT|REVOKE|VACUUM|REINDEX|REFRESH|CALL|DO|COPY)\b' "$sql_file"; then
  printf 'forbidden mutation keyword found in %s\n' "$sql_file" >&2
  exit 1
fi

if ! grep -Eq '^\s*(SELECT|WITH|\\pset|\\set|\\gexec|--)' "$sql_file"; then
  printf 'SQL file does not appear to be a read-only parity script: %s\n' "$sql_file" >&2
  exit 1
fi

required_patterns=(
  'timescaledb_information\.hypertables'
  'timescaledb_information\.chunks'
  'pg_indexes'
  'pg_constraint'
  'pg_get_function_identity_arguments'
  '\\gexec'
)

for pattern in "${required_patterns[@]}"; do
  if ! grep -Eq "$pattern" "$sql_file"; then
    printf 'missing required parity pattern %s in %s\n' "$pattern" "$sql_file" >&2
    exit 1
  fi
done

printf 'ok: read-only parity SQL validation passed for %s\n' "$sql_file"
