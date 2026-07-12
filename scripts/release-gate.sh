#!/bin/sh
set -eu

repo_dir=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
cd "$repo_dir"

go test ./...
go vet ./...
npm --prefix web test -- --run --pool=threads --maxWorkers=1
npm --prefix web run lint
npm --prefix web run build
docker compose config --quiet
docker run --rm --entrypoint promtool \
  -v "$repo_dir/monitoring/prometheus:/etc/prometheus:ro" \
  prom/prometheus:v3.3.0 check rules /etc/prometheus/alerts.yml

echo "Paper release gate passed. Complete the deployment soak before setting RELEASE_DRILLS_VERIFIED=true."
