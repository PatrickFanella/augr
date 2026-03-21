# get-rich-quick

Autonomous trading agent built in Go.

## Local Development Setup

### Prerequisites

- [Docker](https://docs.docker.com/get-docker/) and [Docker Compose](https://docs.docker.com/compose/install/) (v2+)

### Quick Start

1. **Copy the example environment file:**

   ```bash
   cp .env.example .env
   ```

2. **Start all services:**

   ```bash
   docker compose up --build
   ```

   Or use the Makefile shortcut:

   ```bash
   make dev
   ```

3. **Verify the services are running:**

   - App: [http://localhost:8080](http://localhost:8080)
   - PostgreSQL: `localhost:5432` (database: `tradingagent`)
   - Redis: `localhost:6379`

### Services

| Service    | Port | Description                       |
|------------|------|-----------------------------------|
| `app`      | 8080 | Go application with hot-reload    |
| `postgres` | 5432 | PostgreSQL 17 database            |
| `redis`    | 6379 | Redis cache                       |

### Environment Variables

All configuration is managed via `.env` (copied from `.env.example`). See `.env.example` for the full list of variables and their defaults.

| Variable          | Default                                                            | Description                  |
|-------------------|--------------------------------------------------------------------|------------------------------|
| `APP_PORT`        | `8080`                                                             | Application listen port      |
| `APP_ENV`         | `development`                                                      | Runtime environment          |
| `POSTGRES_USER`   | `postgres`                                                         | PostgreSQL username           |
| `POSTGRES_PASSWORD` | `postgres`                                                       | PostgreSQL password           |
| `POSTGRES_DB`     | `tradingagent`                                                     | PostgreSQL database name      |
| `POSTGRES_PORT`   | `5432`                                                             | PostgreSQL host port          |
| `DATABASE_URL`    | `postgres://postgres:postgres@postgres:5432/tradingagent?sslmode=disable` | Full connection string |
| `REDIS_HOST`      | `redis`                                                            | Redis hostname                |
| `REDIS_PORT`      | `6379`                                                             | Redis host port               |
| `REDIS_URL`       | `redis://redis:6379/0`                                             | Full Redis connection string  |

### Useful Commands

```bash
# Start services in the background
docker compose up -d

# View logs
docker compose logs -f

# Stop services
docker compose down

# Stop services and remove volumes (wipes database)
docker compose down -v

# Run database migrations
make migrate-up

# Run unit tests
make test

# Lint
make lint
```

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md) for branch strategy, commit conventions, and the definition of done.
