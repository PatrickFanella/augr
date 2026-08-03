#!/usr/bin/env bash
set -euo pipefail
repo="$(git rev-parse --show-toplevel)"
report="$repo/docs/reports/2026-07-20-git-history-inventory.md"
raw="$repo/docs/reports/2026-07-20-git-history-inventory.txt"
showref="$repo/docs/reports/2026-07-20-git-show-ref.txt"
[[ -f "$report" && -f "$raw" && -f "$showref" ]]
rows=$(grep -c '^| `[^`]*` |' "$report")
[[ "$rows" -eq 61 ]]
grep -q 'HEAD: `87aba28`' "$report"
grep -q 'origin/main: `f6d59bf`' "$report"
grep -Fq 'History contains an internal merge commit' "$report"
grep -q 'backup/wave0-20260720' "$report"
grep -q 'merge-base --is-ancestor origin/main HEAD' "$report"
grep -q 'git push origin backup/wave0-20260720:refs/heads/wave0-20260720' "$report"
grep -Fq 'docker-compose.prod.yml` (modified, untouched)' "$report"
grep -q 'substantive file/domain impact' "$report"
! grep -q "docker-compose.prod.yml" "$raw"
