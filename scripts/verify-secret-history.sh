#!/bin/sh
set -eu

repo_dir=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
scan_dir=$(mktemp -d /tmp/augr-gitleaks.XXXXXX)
trap 'rm -rf "$scan_dir"' EXIT HUP INT TERM

image="${GITLEAKS_IMAGE:-zricethezav/gitleaks@sha256:cdbb7c955abce02001a9f6c9f602fb195b7fadc1e812065883f695d1eeaba854}"

docker run --rm \
  -v "$repo_dir:/repo:ro" \
  -v "$scan_dir:/out" \
  "$image" detect \
  --source=/repo \
  --no-banner \
  --redact \
  --exit-code=0 \
  --report-format=json \
  --report-path=/out/report.json >/dev/null

jq -r '.[] | [.RuleID, .File, (.StartLine | tostring), .Commit] | @tsv' \
  "$scan_dir/report.json" | LC_ALL=C sort >"$scan_dir/actual.tsv"

cat >"$scan_dir/reviewed.tsv" <<'EOF'
generic-api-key	docs/Augr Trading Research/01 Synthesis/Final Combined Automated Trading Synthesis.md	976	f2d478f69c5614efaf7e5636e4d7e9402a85fa68
generic-api-key	internal/config/validate_test.go	148	c15f824cf884e5f2f59b2fed1457751a132edbb8
generic-api-key	internal/config/validate_test.go	39	c15f824cf884e5f2f59b2fed1457751a132edbb8
generic-api-key	internal/execution/binance/client_test.go	55	d10c03f415ecb5be1ec05a0c562f08d85014e302
EOF

LC_ALL=C sort -o "$scan_dir/reviewed.tsv" "$scan_dir/reviewed.tsv"
if ! diff -u "$scan_dir/reviewed.tsv" "$scan_dir/actual.tsv"; then
  echo "Secret-history verification failed: findings differ from the exact reviewed fingerprints." >&2
  exit 1
fi

echo "Secret-history verification passed: no unreviewed findings."
