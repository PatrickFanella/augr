#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
COMPOSE_FILE="${ROOT_DIR}/docker-compose.prod.yml"
ENV_FILE="${ROOT_DIR}/.env"
BACKUP_ENV=""
WAIT_TIMEOUT_SECONDS="${WAIT_TIMEOUT_SECONDS:-120}"
PROJECT_NAME="${VERIFY_PROD_PROJECT_NAME:-tradingagent-prod-verify-$$}"

POSTGRES_USER="${SMOKE_POSTGRES_USER:-postgres}"
POSTGRES_PASSWORD="${SMOKE_POSTGRES_PASSWORD:-postgres}"
POSTGRES_DB="${SMOKE_POSTGRES_DB:-tradingagent}"
JWT_SECRET="${SMOKE_JWT_SECRET:-}"
CONTAINER_DATABASE_URL="postgres://${POSTGRES_USER}:${POSTGRES_PASSWORD}@postgres:5432/${POSTGRES_DB}?sslmode=disable"

cleanup() {
	local exit_code=$?
	compose down -v >/dev/null 2>&1 || true
	if [[ -n "$BACKUP_ENV" && -f "$BACKUP_ENV" ]]; then
		mv "$BACKUP_ENV" "$ENV_FILE"
	else
		rm -f "$ENV_FILE"
	fi
	trap - EXIT
	exit "$exit_code"
}
trap cleanup EXIT

require_command() {
	if ! command -v "$1" >/dev/null 2>&1; then
		echo "required command not found: $1" >&2
		exit 1
	fi
}

compose() {
	docker compose --project-name "$PROJECT_NAME" -f "$COMPOSE_FILE" "$@"
}

random_token_hex() {
	python3 - <<'PY'
import secrets
print(secrets.token_hex(32))
PY
}

wait_for_postgres() {
	local deadline=$((SECONDS + WAIT_TIMEOUT_SECONDS))
	while (( SECONDS < deadline )); do
		if compose exec -T postgres pg_isready -U "$POSTGRES_USER" -d "$POSTGRES_DB" >/dev/null 2>&1; then
			return 0
		fi
		sleep 2
	done
	echo "timed out waiting for postgres readiness" >&2
	return 1
}

wait_for_app_health() {
	local deadline=$((SECONDS + WAIT_TIMEOUT_SECONDS))
	while (( SECONDS < deadline )); do
		local health_response
		if health_response="$(compose exec -T app wget -qO- http://127.0.0.1:8080/healthz 2>/dev/null)"; then
			python3 -c 'import json, sys; body = json.loads(sys.stdin.read()); raise SystemExit(0 if body.get("status") == "ok" and body.get("db") == "ok" and body.get("redis") == "ok" else 1)' <<<"$health_response" && return 0
		fi
		sleep 2
	done
	echo "timed out waiting for app health endpoint" >&2
	return 1
}

require_command docker
require_command python3

if [[ -z "$JWT_SECRET" ]]; then
	JWT_SECRET="$(random_token_hex)"
fi

if [[ -f "$ENV_FILE" ]]; then
	BACKUP_ENV="$(mktemp /tmp/tradingagent-prod-env.XXXXXX)"
	cp "$ENV_FILE" "$BACKUP_ENV"
fi

cat >"$ENV_FILE" <<EOF
APP_ENV=production
APP_HOST=0.0.0.0
APP_PORT=8080
JWT_SECRET=${JWT_SECRET}
DATABASE_URL=${CONTAINER_DATABASE_URL}
DATABASE_POOL_SIZE=10
DATABASE_SSL_MODE=disable
REDIS_URL=redis://redis:6379/0
LLM_DEFAULT_PROVIDER=ollama
LLM_DEEP_THINK_MODEL=smoke-deep
LLM_QUICK_THINK_MODEL=smoke-quick
LLM_TIMEOUT=30s
OLLAMA_BASE_URL=http://ollama.invalid/v1
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
POSTGRES_USER=${POSTGRES_USER}
POSTGRES_PASSWORD=${POSTGRES_PASSWORD}
POSTGRES_DB=${POSTGRES_DB}
EOF

cd "$ROOT_DIR"

compose build
compose up -d
wait_for_postgres
wait_for_app_health

mapfile -t migration_files < <(find "${ROOT_DIR}/migrations" -maxdepth 1 -type f -name '*.up.sql' -printf '%f\n' | sort)
if [[ "${#migration_files[@]}" -eq 0 ]]; then
	echo "no up migrations found in ${ROOT_DIR}/migrations" >&2
	exit 1
fi

for migration in "${migration_files[@]}"; do
	compose exec -T postgres \
		psql -U "$POSTGRES_USER" -d "$POSTGRES_DB" -v ON_ERROR_STOP=1 \
		< "${ROOT_DIR}/migrations/${migration}"
done

HEALTH_RESPONSE="$(compose exec -T app wget -qO- http://127.0.0.1:8080/healthz)"
python3 -c 'import json, sys; body = json.loads(sys.stdin.read()); assert body.get("status") == "ok", body; assert body.get("db") == "ok", body; assert body.get("redis") == "ok", body' <<<"$HEALTH_RESPONSE"

AUTH_TOKEN="$(
	JWT_SECRET="$JWT_SECRET" python3 - <<'PY'
import base64
import hashlib
import hmac
import json
import os
import time

secret = os.environ["JWT_SECRET"].encode()
issued_at = int(time.time())
header = {"alg": "HS256", "typ": "JWT"}
payload = {
    "token_type": "access",
    "sub": "prod-verify",
    "iat": issued_at,
    "exp": issued_at + 3600,
}

def encode_part(value):
    raw = json.dumps(value, separators=(",", ":")).encode()
    return base64.urlsafe_b64encode(raw).rstrip(b"=").decode()

message = f"{encode_part(header)}.{encode_part(payload)}"
signature = base64.urlsafe_b64encode(
    hmac.new(secret, message.encode(), hashlib.sha256).digest(),
).rstrip(b"=").decode()
print(f"{message}.{signature}")
PY
)"

STRATEGIES_RESPONSE="$(
	compose exec -T -e AUTH_TOKEN="$AUTH_TOKEN" app \
		sh -lc 'wget -qO- --header="Authorization: Bearer ${AUTH_TOKEN}" http://127.0.0.1:8080/api/v1/strategies'
)"
python3 -c 'import json, sys; body = json.loads(sys.stdin.read()); assert "data" in body and body.get("limit") == 50 and body.get("offset") == 0, body' <<<"$STRATEGIES_RESPONSE"

echo "Production docker-compose verification passed."
