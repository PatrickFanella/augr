# Security Audit Report

**Date:** 2026-03-29
**Scope:** Full application security review before live trading
**Repository:** PatrickFanella/get-rich-quick

---

## Executive Summary

A comprehensive security audit was performed covering SQL injection, secret
handling, input validation, CORS configuration, JWT/API-key security, rate
limiting, and dependency vulnerabilities. No critical or high-severity
vulnerabilities were found. Three medium-severity hardening items were
identified and remediated in this review.

---

## Findings

### 1. SQL Injection — **PASS**

All database queries in `internal/repository/postgres/` use parameterized
placeholders (`$1`, `$2`, …). Two patterns are present:

| Pattern | Files | Safe? |
|---------|-------|-------|
| Static `$N` in query string | All CRUD operations | ✅ |
| `nextArg` closure returning `$N` placeholders | 13 query-builder functions | ✅ |

The `nextArg` closure appends the actual value to a separate `args` slice and
returns only the positional placeholder string. No user-controlled values are
ever interpolated into query text.

### 2. Secret Handling — **PASS**

- All secrets (JWT secret, API keys, broker credentials, LLM keys) are loaded
  from environment variables via `internal/config/config.go`.
- No secrets are hard-coded in source files.
- The request logger (`RequestLogger` middleware) logs only method, path,
  status code, and duration — never request bodies, headers, or credentials.
- API keys are hashed with SHA-256 before storage; the plaintext is returned
  only once at creation time.

### 3. Input Validation — **PASS** (hardened)

- UUID parameters are validated via `uuid.Parse`.
- Pagination is bounded (`maxLimit = 100`).
- Domain objects validate required fields (`Strategy.Validate()`).
- **Remediated:** Request body size is now limited to 1 MiB via
  `http.MaxBytesReader` middleware, and `MaxHeaderBytes` is set to 1 MiB on
  the HTTP server.

### 4. CORS Configuration — **PASS**

The default CORS policy allows `*` (wildcard origin), which is appropriate for
local development. In production, operators must set specific allowed origins
via server configuration. The CORS middleware correctly:
- Echoes the matching `Origin` header when a specific allow-list is configured.
- Sets `Vary: Origin` for non-wildcard responses.
- Responds with `204 No Content` for preflight (`OPTIONS`) requests.
- Does **not** set `Access-Control-Allow-Credentials`, avoiding the dangerous
  `*` + credentials combination.

### 5. JWT Token Security — **PASS**

- Signing algorithm is `HS256` only, enforced via both a callback check and
  `jwt.WithValidMethods`.
- Access tokens expire after 1 hour; refresh tokens after 24 hours.
- Empty subjects and wrong token types are rejected.
- Algorithm confusion attacks are not possible because `WithValidMethods`
  restricts accepted algorithms.

### 6. API Key Hashing — **PASS**

- Keys are generated with `crypto/rand`.
- Stored as `SHA-256(plaintext)` with a separate prefix for lookup.
- Verification uses `crypto/subtle.ConstantTimeCompare`, preventing timing
  attacks.
- Expiration and revocation are checked before access is granted.

### 7. Rate Limiting — **PASS**

- Global rate limiting: fixed-window counter per client IP (default 100 req/min).
- Per-API-key rate limiting: token-bucket algorithm with configurable limits.
- `X-Forwarded-For` / `X-Real-IP` headers are trusted **only** when the
  immediate peer IP is within a configured CIDR allow-list. By default no
  proxies are trusted, preventing IP spoofing.
- Idle rate-limit buckets are periodically evicted to prevent memory growth.

### 8. Security Headers — **REMEDIATED**

Added `SecurityHeaders` middleware that sets on every response:

| Header | Value |
|--------|-------|
| `X-Content-Type-Options` | `nosniff` |
| `X-Frame-Options` | `DENY` |

### 9. Request Body Size Limits — **REMEDIATED**

- `MaxRequestBody` middleware limits all incoming request bodies to 1 MiB via
  `http.MaxBytesReader`.
- `http.Server.MaxHeaderBytes` is set to 1 MiB.
- `ReadHeaderTimeout` remains at 10 seconds, mitigating slow-header DoS.

### 10. Error Handling — **PASS**

- All error responses use generic messages with structured error codes.
- Internal errors are logged server-side; no stack traces or internal details
  are exposed to clients.

### 11. WebSocket Security — **PASS**

- Origin validation via `CheckOrigin` in the WebSocket upgrader, aligned with
  CORS allowed origins.
- Maximum incoming message size is capped at 4096 bytes.
- Ping/pong keepalive with 60-second timeout.
- Connection through the authentication middleware.

### 12. Docker Image — **PASS**

- Multi-stage build with minimal Alpine production image.
- Non-root user (`app:app`).
- `CGO_ENABLED=0` static binary.
- Health check configured.
- SSL certificates copied for HTTPS client connections.

---

## Dependency Audit

All direct Go dependencies were checked against the GitHub Advisory Database.

| Dependency | Version | Vulnerabilities |
|-----------|---------|-----------------|
| github.com/golang-jwt/jwt/v5 | v5.3.1 | None |
| github.com/gorilla/websocket | v1.5.3 | None |
| github.com/jackc/pgx/v5 | v5.9.1 | None |
| github.com/go-chi/chi/v5 | v5.2.5 | None |
| github.com/google/uuid | v1.6.0 | None |
| github.com/joho/godotenv | v1.5.1 | None |
| github.com/spf13/cobra | v1.10.2 | None |
| github.com/robfig/cron/v3 | v3.0.1 | None |
| golang.org/x/sync | v0.20.0 | None |

**Result:** No known vulnerabilities found.

---

## Remediation Summary

| # | Finding | Severity | Status |
|---|---------|----------|--------|
| 1 | Missing security headers | Medium | ✅ Fixed |
| 2 | No request body size limit | Medium | ✅ Fixed |
| 3 | No `MaxHeaderBytes` on HTTP server | Medium | ✅ Fixed |

---

## Recommendations for Production Deployment

1. **CORS origins:** Set explicit allowed origins instead of wildcard `*`.
2. **Database SSL:** Set `DATABASE_SSL_MODE=verify-full` for production
   PostgreSQL connections.
3. **JWT secret:** Use a cryptographically random secret of at least 32 bytes.
4. **HTTPS:** Deploy behind a TLS-terminating reverse proxy (e.g., nginx,
   Caddy, or a cloud load balancer).
5. **Secret rotation:** Establish a procedure for rotating JWT secrets and
   API keys; revoking old API keys via the existing revocation endpoint.
6. **Monitoring:** Enable audit logging for risk-related operations
   (kill-switch toggles, circuit-breaker trips).
