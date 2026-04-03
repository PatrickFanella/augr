# Getting Started: first run

This guide takes you from a fresh clone to a visible end-to-end run in the web UI.

It uses:
- Docker Compose for **Postgres + Redis only**
- a **native** Go backend build/start
- the Vite frontend in `web/`

Two current constraints matter:
- There is **no sign-up route or sign-up page** yet. The server exposes `POST /api/v1/auth/login` and `POST /api/v1/auth/refresh`, so you must create the first user directly in Postgres.
- Manual strategy runs are wired only when the backend starts with `APP_ENV=smoke`. Outside smoke, `POST /api/v1/strategies/{id}/run` returns `501 manual strategy runs are not configured`.

## 1. Prerequisites

Install these first:
- Docker + Docker Compose v2
- Go 1.25+
- Node.js 20+
- [Task](https://taskfile.dev/installation/)
- optionally [Ollama](https://ollama.com/download) if you want a local LLM instead of a cloud API key

## 2. Clone and create `.env`

```bash
git clone https://github.com/PatrickFanella/get-rich-quick.git
cd get-rich-quick
cp .env.example .env
```

Edit `.env` and set these values:

```dotenv
JWT_SECRET=replace-with-a-long-random-string
DATABASE_URL=postgres://postgres:postgres@localhost:5432/tradingagent?sslmode=disable
REDIS_URL=redis://localhost:6379/0

# choose one LLM path
LLM_DEFAULT_PROVIDER=ollama
OLLAMA_BASE_URL=http://localhost:11434
OLLAMA_MODEL=llama3.2
# or set one cloud key such as OPENAI_API_KEY=...

# set at least one market-data provider key or startup validation fails
POLYGON_API_KEY=...
# or ALPHA_VANTAGE_API_KEY=...
```

If you use Ollama, pull the model before starting the backend:

```bash
ollama serve
ollama pull llama3.2
```

This guide keeps the backend native, so the default `OLLAMA_BASE_URL=http://localhost:11434` works as-is.

## 3. Start Postgres and Redis

```bash
docker compose up -d postgres redis
```

## 4. Apply migrations

```bash
task migrate:up
```

## 5. Build and start the backend

Build once:

```bash
task build
```

For the first visible run, start the server in **smoke** mode.
`APP_ENV=smoke` enables the deterministic manual-run path, but `.env` is only auto-loaded in `development`, so export the file into your shell first:

```bash
set -a
source .env
set +a
export APP_ENV=smoke
./bin/tradingagent serve
```

In another terminal, confirm the API is up:

```bash
curl http://localhost:8080/healthz
```

Expected response:

```json
{"status":"all-ok"}
```

## 6. Create your first account

There is no registration endpoint yet, so insert a local dev user directly into Postgres.
The first migration enables `pgcrypto`, so `crypt(..., gen_salt('bf'))` generates a bcrypt-compatible hash.

```bash
docker compose exec postgres psql -U postgres -d tradingagent <<'SQL'
INSERT INTO users (username, password_hash)
VALUES ('demo', crypt('demo-pass', gen_salt('bf')))
ON CONFLICT (username) DO NOTHING;
SQL
```

You can now log in with:
- username: `demo`
- password: `demo-pass`

## 7. Start the frontend and log in

```bash
cd web
npm install
npm run dev
```

Open <http://localhost:5173/login>.

`VITE_API_BASE_URL` defaults to `http://localhost:8080`, so no extra frontend env var is needed for this local flow.

Log in with the `demo` account from the previous step.

## 8. Create a strategy

In the UI:
1. Open **Strategies**.
2. Click **New strategy**.
3. Fill the minimum fields:
   - **Name**: `Smoke Strategy`
   - **Ticker**: `SMOKE`
   - **Market type**: `stock`
4. Leave **Paper trading** enabled.
5. Enable **Active** if you want it to show up as active immediately.
6. Leave schedule empty for a manual-only run.
7. Click **Create strategy**.

## 9. Trigger a run

Use the strategy UI:
- click **Run** on the strategy row, or
- open the strategy detail page and click **Run now**

Because the backend is running with `APP_ENV=smoke`, the run uses the deterministic smoke pipeline and should complete quickly without live LLM calls.

## 10. View results

Use these pages:
- **Runs** — open the newest row to inspect the pipeline run at `/runs/:id`
- **Overview** — portfolio summary, active strategies, activity feed, and risk status
- **Strategies** / strategy detail — latest run history and current strategy status

On the run detail page you should see:
- phase progress
- analyst cards
- debate sections
- trader plan
- final signal

## 11. Troubleshooting FAQ

### Port already in use

If `docker compose up -d postgres redis`, `./bin/tradingagent serve`, or `npm run dev` says the address is already in use:

```bash
lsof -i :8080
lsof -i :5432
lsof -i :6379
lsof -i :5173
```

- Native backend/frontend: stop the conflicting process, or pick a new backend port and point Vite at it:

```bash
export APP_PORT=9090
cd web
VITE_API_BASE_URL=http://localhost:9090 npm run dev
```

- Docker Compose Postgres/Redis: set `POSTGRES_PORT` or `REDIS_PORT` in `.env`, then rerun `docker compose up -d postgres redis`.
- If you use `task dev` or `docker compose up` for the full stack instead of this guide's native backend flow, `APP_PORT` changes the Compose app port mapping too.

### Migration errors

If `task migrate:up` fails:

```bash
docker compose ps postgres
docker compose logs postgres --tail=50
task migrate:status
```

- `migrate: not found`: run `task tools`.
- `connect: connection refused`: Postgres is not healthy yet; wait for `docker compose ps postgres` to report it running, then retry.
- `Dirty database version` or another disposable-local-db failure: reset the local volumes and re-apply migrations:

```bash
docker compose down -v
docker compose up -d postgres redis
task migrate:up
```

- Native host commands must use `localhost:5432`. `postgres:5432` only works from inside Compose containers.

### Ollama is not running

If the backend starts but LLM calls fail while `LLM_DEFAULT_PROVIDER=ollama`:

```bash
curl http://localhost:11434/api/tags
ollama serve
ollama pull llama3.2
```

- Keep `OLLAMA_BASE_URL=http://localhost:11434` for this native-backend guide.
- If you do not want Ollama, switch `.env` to a cloud provider and set the matching API key instead of leaving `LLM_DEFAULT_PROVIDER=ollama`.

### Browser shows a CORS error

Local development uses permissive CORS, so a browser CORS error usually means the frontend is calling the wrong backend URL or the backend is down.

```bash
curl http://localhost:8080/healthz
```

- If you changed `APP_PORT`, restart Vite with the matching API base URL:

```bash
cd web
VITE_API_BASE_URL=http://localhost:9090 npm run dev
```

- Remove stale `VITE_API_BASE_URL` values from any `web/.env*` file if they still point at an old host or port.
- If you later lock CORS down to specific origins, include the exact frontend origin such as `http://localhost:5173` or browser API/WebSocket calls will fail.

### `invalid configuration: ...` on startup

In `APP_ENV=smoke`, `.env` is not auto-loaded. Export it before starting the backend:

```bash
set -a
source .env
set +a
export APP_ENV=smoke
./bin/tradingagent serve
```

Then re-check `JWT_SECRET`, `DATABASE_URL`, one LLM provider, and one market-data provider key.

### Login fails with `invalid username or password`

There is no sign-up route yet. Check whether the local user exists:

```bash
docker compose exec postgres psql -U postgres -d tradingagent -c "SELECT username FROM users;"
```

If `demo` is missing, re-run the `INSERT INTO users ...` step from section 6 or create a new user with a known password.

### `manual strategy runs are not configured`

The backend is not running with `APP_ENV=smoke`. Stop it and restart with the smoke-mode steps from section 5. Outside smoke, manual `POST /api/v1/strategies/{id}/run` currently returns `501 manual strategy runs are not configured`.