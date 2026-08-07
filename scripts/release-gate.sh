#!/bin/sh
set -eu

repo_dir=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
cd "$repo_dir"

promtool_image="${PROMTOOL_IMAGE:-prom/prometheus@sha256:339ce86a59413be18d0e445472891d022725b4803fab609069110205e79fb2f1}"

go test ./...
go vet ./...
npm --prefix web test -- --run --pool=threads --maxWorkers=1
npm --prefix web run lint
npm --prefix web run build
docker compose config --quiet
./scripts/verify-prod-build.sh
docker run --rm --entrypoint promtool \
  -v "$repo_dir/monitoring/prometheus:/etc/prometheus:ro" \
  "$promtool_image" check rules /etc/prometheus/alerts.yml
./scripts/verify-secret-history.sh

echo "Paper release gate passed. Complete the deployment soak before setting RELEASE_DRILLS_VERIFIED=true."
