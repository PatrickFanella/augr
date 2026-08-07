#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
COMPOSE_FILE="${ROOT_DIR}/docker-compose.prod.yml"
PROJECT_NAME="${VERIFY_PROJECT_NAME:-augr-prod-verify-$$}"
MIGRATE_IMAGE="${MIGRATE_IMAGE:-migrate/migrate:v4.18.3}"
VERIFY_ROLLBACK_SCHEMA_VERSION="${VERIFY_ROLLBACK_SCHEMA_VERSION:-60}"
VERIFY_ROLLBACK_IMAGE="${VERIFY_ROLLBACK_IMAGE:-}"

case "$VERIFY_ROLLBACK_IMAGE" in
    *[!A-Za-z0-9._/@:-]*)
        echo "VERIFY_ROLLBACK_IMAGE contains unsupported characters" >&2
        exit 1
        ;;
esac

case "$PROJECT_NAME" in
    augr-prod-verify-*) ;;
    *)
        echo "VERIFY_PROJECT_NAME must start with augr-prod-verify-" >&2
        exit 1
        ;;
esac
VERIFY_WEB_IMAGE="${PROJECT_NAME}-web:latest"

if [ -n "$(docker ps -aq --filter "label=com.docker.compose.project=${PROJECT_NAME}")" ]; then
    echo "refusing to reuse existing Compose project ${PROJECT_NAME}" >&2
    exit 1
fi

VERIFY_DIR="$(mktemp -d /tmp/augr-prod-verify.XXXXXX)"
APP_ENV_FILE="${VERIFY_DIR}/app.env"
NETWORK_OVERRIDE_FILE="${VERIFY_DIR}/network-override.yml"
ROLLBACK_IMAGE_OVERRIDE_FILE="${VERIFY_DIR}/rollback-image-override.yml"

# Docker's automatic bridge address pools can be exhausted on shared hosts even
# when these short-lived networks are cleaned up correctly. Use small, explicit,
# caller-overridable subnets so the smoke stack does not depend on that allocator.
VERIFY_PUBLIC_SUBNET="${VERIFY_PUBLIC_SUBNET:-10.252.0.0/28}"
VERIFY_BACKEND_SUBNET="${VERIFY_BACKEND_SUBNET:-10.252.0.16/28}"
export VERIFY_BACKEND_SUBNET VERIFY_PUBLIC_SUBNET

cat >"$NETWORK_OVERRIDE_FILE" <<'EOF'
networks:
  public:
    ipam:
      config:
        - subnet: ${VERIFY_PUBLIC_SUBNET}
  backend:
    ipam:
      config:
        - subnet: ${VERIFY_BACKEND_SUBNET}
EOF

VERIFY_APP_PORT="${VERIFY_APP_PORT:-$(python3 - <<'PY'
import socket
with socket.socket() as sock:
    sock.bind(("127.0.0.1", 0))
    print(sock.getsockname()[1])
PY
)}"

POSTGRES_USER="augr_verify"
POSTGRES_DB="augr_verify"
POSTGRES_PASSWORD="$(python3 -c 'import secrets; print(secrets.token_hex(24))')"
SMOKE_JWT_SECRET="$(python3 -c 'import secrets; print(secrets.token_hex(32))')"
APP_BIND="127.0.0.1"
APP_PORT="$VERIFY_APP_PORT"
APP_ENV="smoke"

export APP_BIND APP_ENV APP_ENV_FILE APP_PORT POSTGRES_DB POSTGRES_PASSWORD POSTGRES_USER

compose() {
    compose_files=(-f "$COMPOSE_FILE" -f "$NETWORK_OVERRIDE_FILE")
    if [ -s "$ROLLBACK_IMAGE_OVERRIDE_FILE" ]; then
        compose_files+=(-f "$ROLLBACK_IMAGE_OVERRIDE_FILE")
    fi
    docker compose --project-name "$PROJECT_NAME" "${compose_files[@]}" "$@"
}

cleanup() {
    compose down --volumes --remove-orphans --rmi local >/dev/null 2>&1 || true
    docker image rm "$VERIFY_WEB_IMAGE" >/dev/null 2>&1 || true
    rm -rf "$VERIFY_DIR"
}
trap cleanup EXIT HUP INT TERM

cat >"$APP_ENV_FILE" <<EOF
APP_ENV=smoke
APP_HOST=0.0.0.0
APP_PORT=8080
JWT_SECRET=${SMOKE_JWT_SECRET}
DATABASE_URL=postgres://${POSTGRES_USER}:${POSTGRES_PASSWORD}@postgres:5432/${POSTGRES_DB}?sslmode=disable
DATABASE_POOL_SIZE=10
DATABASE_SSL_MODE=disable
REDIS_URL=redis://redis:6379/0
LLM_DEFAULT_PROVIDER=ollama
LLM_DEEP_THINK_MODEL=smoke-deep
LLM_QUICK_THINK_MODEL=smoke-quick
LLM_TIMEOUT=30s
OLLAMA_BASE_URL=http://ollama.invalid/v1
OLLAMA_API_KEY=smoke-key
OLLAMA_MODEL=smoke-model
ALPHA_VANTAGE_API_KEY=smoke-key
ALPHA_VANTAGE_RATE_LIMIT_PER_MINUTE=5
FINNHUB_RATE_LIMIT_PER_MINUTE=60
RISK_MAX_POSITION_SIZE_PCT=0.10
RISK_MAX_DAILY_LOSS_PCT=0.02
RISK_MAX_DRAWDOWN_PCT=0.10
RISK_MAX_OPEN_POSITIONS=10
RISK_CIRCUIT_BREAKER_THRESHOLD=0.05
RISK_CIRCUIT_BREAKER_COOLDOWN=15m
ENABLE_SCHEDULER=false
ENABLE_REDIS_CACHE=false
ENABLE_AGENT_MEMORY=false
ENABLE_LIVE_TRADING=false
ALPACA_PAPER_MODE=true
BINANCE_PAPER_MODE=true
KALSHI_DRY_RUN=true
ENABLE_POLYMARKET_AUTOMATION=false
EOF

wait_for_postgres() {
    echo "Waiting for isolated PostgreSQL..."
    for _ in $(seq 1 30); do
        if compose exec -T postgres pg_isready -h postgres -U "$POSTGRES_USER" -d "$POSTGRES_DB" >/dev/null 2>&1; then
            return 0
        fi
        sleep 1
    done
    echo "PostgreSQL did not become ready" >&2
    exit 1
}

wait_for_app_health() {
    echo "Waiting for isolated app health..."
    for _ in $(seq 1 60); do
        response=$(curl -fsS "http://127.0.0.1:${VERIFY_APP_PORT}/healthz" 2>/dev/null || true)
        if python3 -c '
import json, sys
body = json.loads(sys.argv[1])
sys.exit(0 if body.get("status") == "ok" and body.get("db") == "ok" and body.get("redis") == "ok" else 1)
' "$response" 2>/dev/null; then
            return 0
        fi
        sleep 1
    done
    echo "App did not become healthy" >&2
    compose logs --no-color app >&2 || true
    exit 1
}

echo "=== Building production image for ${PROJECT_NAME} ==="
VERIFY_BUILD_VERSION="$(git -C "$ROOT_DIR" describe --tags --always --dirty)"
VERIFY_BUILD_COMMIT="$(git -C "$ROOT_DIR" rev-parse HEAD)"
VERIFY_BUILD_TIME="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
BUILD_VERSION="$VERIFY_BUILD_VERSION" \
BUILD_COMMIT="$VERIFY_BUILD_COMMIT" \
BUILD_TIME="$VERIFY_BUILD_TIME" \
compose build app

BUILT_APP_IMAGE_ID=$(docker image inspect --format '{{.Id}}' "${PROJECT_NAME}-app:latest" 2>/dev/null || true)
if [ -z "$BUILT_APP_IMAGE_ID" ]; then
    echo "could not resolve built app image" >&2
    exit 1
fi
BUILT_APP_REVISION=$(docker image inspect --format '{{ index .Config.Labels "org.opencontainers.image.revision" }}' "$BUILT_APP_IMAGE_ID")
BUILT_APP_VERSION=$(docker image inspect --format '{{ index .Config.Labels "org.opencontainers.image.version" }}' "$BUILT_APP_IMAGE_ID")
BUILT_APP_CREATED=$(docker image inspect --format '{{ index .Config.Labels "org.opencontainers.image.created" }}' "$BUILT_APP_IMAGE_ID")
if [ "$BUILT_APP_REVISION" != "$VERIFY_BUILD_COMMIT" ]; then
    echo "built app revision label mismatch: got ${BUILT_APP_REVISION}, expected ${VERIFY_BUILD_COMMIT}" >&2
    exit 1
fi
if [ "$BUILT_APP_VERSION" != "$VERIFY_BUILD_VERSION" ]; then
    echo "built app version label mismatch: got ${BUILT_APP_VERSION}, expected ${VERIFY_BUILD_VERSION}" >&2
    exit 1
fi
if [ "$BUILT_APP_CREATED" != "$VERIFY_BUILD_TIME" ]; then
    echo "built app creation label mismatch: got ${BUILT_APP_CREATED}, expected ${VERIFY_BUILD_TIME}" >&2
    exit 1
fi

docker buildx build --load \
    --tag "$VERIFY_WEB_IMAGE" \
    --build-arg "BUILD_VERSION=$VERIFY_BUILD_VERSION" \
    --build-arg "BUILD_COMMIT=$VERIFY_BUILD_COMMIT" \
    --build-arg "BUILD_TIME=$VERIFY_BUILD_TIME" \
    --file "${ROOT_DIR}/Dockerfile.web" \
    "$ROOT_DIR"
BUILT_WEB_REVISION=$(docker image inspect --format '{{ index .Config.Labels "org.opencontainers.image.revision" }}' "$VERIFY_WEB_IMAGE")
BUILT_WEB_VERSION=$(docker image inspect --format '{{ index .Config.Labels "org.opencontainers.image.version" }}' "$VERIFY_WEB_IMAGE")
BUILT_WEB_CREATED=$(docker image inspect --format '{{ index .Config.Labels "org.opencontainers.image.created" }}' "$VERIFY_WEB_IMAGE")
if [ "$BUILT_WEB_REVISION" != "$VERIFY_BUILD_COMMIT" ]; then
    echo "built web revision label mismatch: got ${BUILT_WEB_REVISION}, expected ${VERIFY_BUILD_COMMIT}" >&2
    exit 1
fi
if [ "$BUILT_WEB_VERSION" != "$VERIFY_BUILD_VERSION" ]; then
    echo "built web version label mismatch: got ${BUILT_WEB_VERSION}, expected ${VERIFY_BUILD_VERSION}" >&2
    exit 1
fi
if [ "$BUILT_WEB_CREATED" != "$VERIFY_BUILD_TIME" ]; then
    echo "built web creation label mismatch: got ${BUILT_WEB_CREATED}, expected ${VERIFY_BUILD_TIME}" >&2
    exit 1
fi

echo "=== Starting isolated dependencies ==="
compose up -d postgres redis
wait_for_postgres

backend_network=$(docker network ls \
    --filter "label=com.docker.compose.project=${PROJECT_NAME}" \
    --filter "label=com.docker.compose.network=backend" \
    --format '{{.Name}}')
if [ -z "$backend_network" ] || [ "$(printf '%s\n' "$backend_network" | wc -l)" -ne 1 ]; then
    echo "could not resolve the isolated backend network" >&2
    exit 1
fi

echo "=== Applying migrations through golang-migrate ==="
docker run --rm \
    --network "$backend_network" \
    -v "${ROOT_DIR}/migrations:/migrations:ro" \
    "$MIGRATE_IMAGE" \
    -path=/migrations \
    -database "postgres://${POSTGRES_USER}:${POSTGRES_PASSWORD}@postgres:5432/${POSTGRES_DB}?sslmode=disable" \
    up

echo "=== Verifying schema version ==="
SCHEMA_VERSION=$(compose exec -T postgres psql -U "$POSTGRES_USER" -d "$POSTGRES_DB" -tAc \
    "SELECT version FROM schema_migrations ORDER BY version DESC LIMIT 1" | tr -d '[:space:]')
EXPECTED_VERSION=$(find "${ROOT_DIR}/migrations" -maxdepth 1 -type f -name '*.up.sql' -printf '%f\n' | sort -V | tail -1 | cut -d_ -f1 | sed 's/^0*//')
if [ "$SCHEMA_VERSION" != "$EXPECTED_VERSION" ]; then
    echo "schema version mismatch after migrations: got ${SCHEMA_VERSION}, expected ${EXPECTED_VERSION}" >&2
    exit 1
fi
case "$VERIFY_ROLLBACK_SCHEMA_VERSION" in
    ''|*[!0-9]*)
        echo "VERIFY_ROLLBACK_SCHEMA_VERSION must be a non-negative integer" >&2
        exit 1
        ;;
esac
if [ "$VERIFY_ROLLBACK_SCHEMA_VERSION" -ge "$EXPECTED_VERSION" ]; then
    echo "VERIFY_ROLLBACK_SCHEMA_VERSION must be lower than ${EXPECTED_VERSION}" >&2
    exit 1
fi
ROLLBACK_STEPS=$((EXPECTED_VERSION - VERIFY_ROLLBACK_SCHEMA_VERSION))

echo "=== Starting isolated production app ==="
compose up -d app
wait_for_app_health

AUTH_TOKEN=$(JWT_SECRET="$SMOKE_JWT_SECRET" python3 - <<'PY'
import base64, hashlib, hmac, json, os, time
encode = lambda value: base64.urlsafe_b64encode(value).rstrip(b"=")
now = int(time.time())
header = encode(json.dumps({"alg": "HS256", "typ": "JWT"}, separators=(",", ":")).encode())
payload = encode(json.dumps({"sub": "production-smoke", "iat": now, "exp": now + 300, "token_type": "access"}, separators=(",", ":")).encode())
unsigned = header + b"." + payload
signature = encode(hmac.new(os.environ["JWT_SECRET"].encode(), unsigned, hashlib.sha256).digest())
print((unsigned + b"." + signature).decode())
PY
)

echo "=== Smoke-testing authenticated read-only API ==="
curl -fsS \
    -H "Authorization: Bearer ${AUTH_TOKEN}" \
    "http://127.0.0.1:${VERIFY_APP_PORT}/api/v1/strategies" | \
    python3 -c 'import json, sys; json.load(sys.stdin)'

echo "=== Rehearsing lossless schema rollback ==="
compose stop app >/dev/null
NEW_STRUCTURE_WRITES=$(compose exec -T postgres psql -U "$POSTGRES_USER" -d "$POSTGRES_DB" -tAc \
    "SELECT (SELECT count(*) FROM automation_job_controls)::text || '|' || (SELECT count(*) FROM trades WHERE exit_reason IS NOT NULL)::text" | tr -d '[:space:]')
if [ "$NEW_STRUCTURE_WRITES" != "0|0" ]; then
    echo "refusing rollback rehearsal with writes in schema 61/62 structures: ${NEW_STRUCTURE_WRITES}" >&2
    exit 1
fi

docker run --rm \
    --network "$backend_network" \
    -v "${ROOT_DIR}/migrations:/migrations:ro" \
    "$MIGRATE_IMAGE" \
    -path=/migrations \
    -database "postgres://${POSTGRES_USER}:${POSTGRES_PASSWORD}@postgres:5432/${POSTGRES_DB}?sslmode=disable" \
    down "$ROLLBACK_STEPS"

ROLLBACK_SCHEMA_VERSION=$(compose exec -T postgres psql -U "$POSTGRES_USER" -d "$POSTGRES_DB" -tAc \
    "SELECT version FROM schema_migrations ORDER BY version DESC LIMIT 1" | tr -d '[:space:]')
if [ "$ROLLBACK_SCHEMA_VERSION" != "$VERIFY_ROLLBACK_SCHEMA_VERSION" ]; then
    echo "schema rollback mismatch: got ${ROLLBACK_SCHEMA_VERSION}, expected ${VERIFY_ROLLBACK_SCHEMA_VERSION}" >&2
    exit 1
fi

echo "=== Verifying schema-${VERIFY_ROLLBACK_SCHEMA_VERSION} predeployment backup and restore ==="
BACKUP_FILE="${VERIFY_DIR}/predeploy-schema-${VERIFY_ROLLBACK_SCHEMA_VERSION}.dump"
RESTORE_DB="${POSTGRES_DB}_restore"
compose exec -T postgres pg_dump \
    -U "$POSTGRES_USER" \
    -d "$POSTGRES_DB" \
    --format=custom \
    --no-owner >"$BACKUP_FILE"
if [ ! -s "$BACKUP_FILE" ]; then
    echo "isolated predeployment backup is empty" >&2
    exit 1
fi
compose exec -T postgres createdb -U "$POSTGRES_USER" "$RESTORE_DB"
compose exec -T postgres pg_restore \
    -U "$POSTGRES_USER" \
    -d "$RESTORE_DB" \
    --exit-on-error \
    --no-owner <"$BACKUP_FILE"
RESTORED_SCHEMA_VERSION=$(compose exec -T postgres psql -U "$POSTGRES_USER" -d "$RESTORE_DB" -tAc \
    "SELECT version FROM schema_migrations ORDER BY version DESC LIMIT 1" | tr -d '[:space:]')
if [ "$RESTORED_SCHEMA_VERSION" != "$VERIFY_ROLLBACK_SCHEMA_VERSION" ]; then
    echo "restored backup schema mismatch: got ${RESTORED_SCHEMA_VERSION}, expected ${VERIFY_ROLLBACK_SCHEMA_VERSION}" >&2
    exit 1
fi
compose exec -T postgres dropdb -U "$POSTGRES_USER" "$RESTORE_DB"

if [ -n "$VERIFY_ROLLBACK_IMAGE" ]; then
    echo "=== Verifying exact rollback image with scheduler disabled ==="
    cat >"$ROLLBACK_IMAGE_OVERRIDE_FILE" <<EOF
services:
  app:
    image: ${VERIFY_ROLLBACK_IMAGE}
EOF
    compose up -d --no-build app
    wait_for_app_health
    rollback_container=$(compose ps -q app)
    actual_rollback_image=$(docker inspect -f '{{.Config.Image}}' "$rollback_container")
    if [ "$actual_rollback_image" != "$VERIFY_ROLLBACK_IMAGE" ]; then
        echo "rollback image mismatch: got ${actual_rollback_image}, expected ${VERIFY_ROLLBACK_IMAGE}" >&2
        exit 1
    fi
    rollback_automation_code=$(curl -sS -o /dev/null -w '%{http_code}' \
        -H "Authorization: Bearer ${AUTH_TOKEN}" \
        "http://127.0.0.1:${VERIFY_APP_PORT}/api/v1/automation/status")
    if [ "$rollback_automation_code" != "503" ]; then
        echo "rollback scheduler check returned HTTP ${rollback_automation_code}, expected 503" >&2
        exit 1
    fi
    compose stop app >/dev/null
    rm -f "$ROLLBACK_IMAGE_OVERRIDE_FILE"
fi

docker run --rm \
    --network "$backend_network" \
    -v "${ROOT_DIR}/migrations:/migrations:ro" \
    "$MIGRATE_IMAGE" \
    -path=/migrations \
    -database "postgres://${POSTGRES_USER}:${POSTGRES_PASSWORD}@postgres:5432/${POSTGRES_DB}?sslmode=disable" \
    up

REAPPLIED_SCHEMA_VERSION=$(compose exec -T postgres psql -U "$POSTGRES_USER" -d "$POSTGRES_DB" -tAc \
    "SELECT version FROM schema_migrations ORDER BY version DESC LIMIT 1" | tr -d '[:space:]')
if [ "$REAPPLIED_SCHEMA_VERSION" != "$EXPECTED_VERSION" ]; then
    echo "schema reapply mismatch: got ${REAPPLIED_SCHEMA_VERSION}, expected ${EXPECTED_VERSION}" >&2
    exit 1
fi

compose up -d app
wait_for_app_health

echo "=== Production build verification passed; isolated stack will be removed ==="
