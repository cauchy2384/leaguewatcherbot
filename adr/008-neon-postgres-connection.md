# ADR 008: Neon PostgreSQL Connection on Startup

## Status

**Proposed** - 2026-08-12

## Context

ADR 007 (branch `adr/007-pidors-relational-db-migration`) selected **PostgreSQL on Neon free tier** as the future persistence layer for pidor game state. Before any repository migration can begin, the bot needs a reliable, feature-flagged way to open and verify a database connection at startup.

The Neon project is already provisioned and the following secrets exist in Doppler (`leaguewatcherbot` / `prd`):

| Secret | Purpose |
|---|---|
| `DATABASE_URL` | Direct Neon connection string (used for migrations / one-off tools) |
| `DATABASE_URL_POOLED` | Pooled connection string (PgBouncer endpoint) for runtime traffic |
| `NEON_AUTH_BASE_URL` | Base URL of Neon Auth (Better Auth-based managed auth) |

A skeleton `internal/leaguewatcher/db/db.go` (untracked) already contains a `Connect(ctx, url, logger)` helper returning a `*pgxpool.Pool` with a startup ping.

### Requirements

1. **Connect on startup**: open the pool and verify it with `Ping` before the watcher/bot start.
2. **Feature-flagged**: empty `DATABASE_URL` keeps the bot running in legacy JSON-file mode (per ADR-007, step 4).
3. **Fail fast when configured**: invalid or unreachable `DATABASE_URL` must abort startup with a clear error (ADR-007, step 4 exit criteria).
4. **No secrets in logs**: connection strings must never be logged; only pool metadata.
5. **Graceful shutdown**: pool must be closed during the existing shutdown sequence.
6. **Neon Auth plumbed, not consumed**: `NEON_AUTH_BASE_URL` must be loaded and validated, but full auth integration is out of scope (see below).

## Decision

### 1. Configuration (`internal/leaguewatcher/config.go`)

Add three secret fields, all `yaml:"-"`, loaded exclusively from Doppler:

| Field | Doppler secret | Required | Notes |
|---|---|---|---|
| `DatabaseURL` | `DATABASE_URL` | optional (feature flag) | Direct Neon endpoint; used by migration tools |
| `DatabaseURLPooled` | `DATABASE_URL_POOLED` | optional | Pooled endpoint; preferred for runtime traffic |
| `NeonAuthBaseURL` | `NEON_AUTH_BASE_URL` | optional | Base URL of the Neon Auth (Better Auth-based) service |

- All three are **optional**: empty `DATABASE_URL_POOLED`/`DATABASE_URL` keeps the bot in legacy JSON-file mode (ADR-007 feature flag).
- `IsValid()` stays unchanged — DB remains optional until the pidor migration cutover.
- `LogValue` must redact all three values.

### 2. Connection package (`internal/leaguewatcher/db/db.go`)

A skeleton `Connect(ctx, url, logger) (*pgxpool.Pool, error)` already exists on this branch. Finalize it:

- Driver: `github.com/jackc/pgx/v5/pgxpool` (std lib `database/sql` via pgx stdlib is an acceptable alternative; pgx native pool preferred).
- Pool defaults: `MaxConns=10`, `MinConns=1`, `MaxConnLifetime=30m`, `MaxConnIdleTime=5m`.
- Startup verification: `pool.Ping(ctx)` with a 15 s timeout.
- Never log the URL; log only pool metadata (`max_conns`, `min_conns`).

### 3. Runtime selection & startup wiring (`cmd/leaguewatcher/main.go`)

After the initial `configMgr.Reload(ctx)` and before `watcher.Run`:

1. If `DATABASE_URL_POOLED` (falling back to `DATABASE_URL`) is **empty** → log "database disabled, using JSON persistence" and continue (legacy mode, per ADR-007 feature flag).
2. If set → `db.Connect(ctx, url, logger)` (connect + `Ping`, 15 s timeout).
   - Failure → log error and **exit** (fail fast per ADR-007 step 4: invalid URL = clear startup failure).
3. On shutdown: `pool.Close()` after `watcherDone`/`botDone` drain.

### 4. Neon Auth scope (`NEON_AUTH_BASE_URL`)

Neon Auth is built on **Better Auth**, which is a TypeScript framework — it cannot be embedded in this Go service. Scope for this task:

- Load `NEON_AUTH_BASE_URL` into config (validated as non-empty **only if** DATABASE_URL is set).
- Startup performs a lightweight reachability check (`GET {NEON_AUTH_BASE_URL}/api/auth/ok` or equivalent well-known endpoint) with a 5 s timeout; failure is logged as a warning, **not fatal** (auth is not on the bot's critical path).
- Actual auth flows (session verification via `/api/auth/*` endpoints or JWKS validation) are **deferred** until a consumer exists (e.g., a web dashboard).

### 5. Shutdown

In `main.go` shutdown sequence, after `watcherDone`/`botDone` drain: `pool.Close()`.

### 6. Testing

- Unit (config): fake `SecretProvider` with the three new secrets → fields populated; missing → empty; redaction verified.
- Unit (db): `Connect` with empty/invalid URL returns clear errors.
- Integration (gated on env var, skipped by default): real ping against a Neon dev branch.

## Progress Plan

| Done | Step | Deliverable | Exit criteria |
|---|---|---|---|
| [x] | 0. Provision secrets | `DATABASE_URL`, `DATABASE_URL_POOLED`, `NEON_AUTH_BASE_URL` in Doppler dev/prd | Confirmed present by operator |
| [x] | 1. Branch | `feat/neon-postgres-connection` | Branch exists, based on `main` |
| [x] | 2. Config plumbing | Fields + Doppler parsing + redaction + table-driven tests with fake `SecretProvider` | `go test ./internal/leaguewatcher` passes; secrets never logged |
| [x] | 3. Dependency | `github.com/jackc/pgx/v5` in `go.mod` | `go mod tidy` clean |
| [x] | 4. `db` package | `internal/leaguewatcher/db` with `Connect(ctx, url)` pool + `Ping` (15 s timeout) | Unit tests: empty/invalid URL errors; integration test gated behind env var, skipped by default |
| [x] | 5. Startup wiring | `main.go`: after initial `configMgr.Reload`, before `watcher.Run` | No URL → log "database disabled, JSON mode", continue; URL set → connect+ping, **fail fast** on error; `pool.Close()` on shutdown |
| [x] | 6. Neon Auth plumbing | `NEON_AUTH_BASE_URL` loaded; reachability check (`GET {base}/api/auth/ok`, 5 s timeout) in `internal/leaguewatcher/neonauth` | Failure logs warning only (non-fatal); URL never logged with credentials |
| [ ] | 7. Validation | Run against Neon dev branch | Startup log shows `database connection established` with pool metadata; no credentials in logs |
| [x] | 8. Docs | Update `ARCHITECTURE.md`/`AGENTS.md` config tables | Docs list the three new Doppler secrets |

## Consequences

### Positive
- Unblocks ADR-007 steps 1–4 (migrations, Postgres pidor repo).
- Fail-fast behavior catches bad credentials at deploy time, not at first `!pidorday`.
- Pooled vs direct URL split ready for future migration tooling.

### Negative
- New hard dependency (pgx) and a new fatal-startup path when `DATABASE_URL` is set but wrong.
- Neon Auth integration is partial by design (see below).

### Neutral / Deferred
- **Better Auth is TypeScript-only.** This Go service cannot embed it; it will interact with Neon Auth over HTTP (session/JWKS verification) only when a real consumer exists. Until then `NEON_AUTH_BASE_URL` is validated but unused beyond a reachability check.
- No schema/migrations in this task — that remains ADR-007 step 1.

## References

- `internal/leaguewatcher/config.go` (Doppler `Reload`)
- `cmd/leaguewatcher/main.go` (startup/shutdown sequence)
- `adr/007-pidors-relational-db-migration.md` (branch `adr/007-pidors-relational-db-migration`), steps 0 and 4
- Neon pooled connections: https://neon.tech/docs/connect/connection-pooling
- Neon Auth (Better Auth-based): https://neon.tech/docs/auth/overview

## Reviewers

- **Author**: Kimi
- **Approver**: cauchy2384
- **Date**: 2026-08-12
