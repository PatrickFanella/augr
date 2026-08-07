#!/bin/sh
set -eu

repo_dir=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
cd "$repo_dir"

promtool_image="${PROMTOOL_IMAGE:-prom/prometheus@sha256:339ce86a59413be18d0e445472891d022725b4803fab609069110205e79fb2f1}"
candidate_commit=$(git rev-parse HEAD)

echo "Starting paper release gate for commit $candidate_commit."
./scripts/verify-release-tree.sh
sh -n \
  scripts/observe-automation-run.sh \
  scripts/observe-paper-boundary.sh \
  scripts/paper-week.sh \
  scripts/release-gate.sh \
  scripts/verify-release-tree.sh \
  scripts/verify-secret-history.sh
bash -n scripts/verify-prod-build.sh
go test -count=1 ./...
go vet ./...
golangci-lint run ./...
npm --prefix web test -- --run --pool=threads --maxWorkers=1
npm --prefix web run lint
npm --prefix web run build
docker compose config --quiet
docker compose -f docker-compose.nuc.yml config --quiet
docker compose -f docker-compose.nuc.yml -f deploy/docker-compose.nuc.rollback.yml config --quiet
MIGRATION_DOWN_STEPS=2 docker compose -f docker-compose.nuc.yml -f deploy/docker-compose.nuc.migrate-down.yml config --quiet
docker buildx build --check -f Dockerfile .
docker buildx build --check -f Dockerfile.web .
./scripts/verify-prod-build.sh
docker run --rm --entrypoint promtool \
  -v "$repo_dir/monitoring/prometheus:/etc/prometheus:ro" \
  "$promtool_image" check rules /etc/prometheus/alerts.yml
./scripts/verify-secret-history.sh

./scripts/verify-release-tree.sh
verified_commit=$(git rev-parse HEAD)
if ! [ "$verified_commit" = "$candidate_commit" ]; then
    echo "release candidate changed during gate: expected $candidate_commit, found $verified_commit" >&2
    exit 1
fi

echo "Paper release gate passed for commit $verified_commit. Complete the deployment soak before setting RELEASE_DRILLS_VERIFIED=true."
