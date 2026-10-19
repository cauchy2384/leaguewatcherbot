# ADR 010: Standalone Migration Runner (`tools/migrator`)

## Status

**Accepted** - 2026-10-19

Builds on [ADR-009](009-neon-postgres-connection.md).

## Context

ADR-009 added a feature-flagged Neon connection pool opened by the bot
at startup. The database is empty — no schema. Before any repo code can
migrate off `pidors.json`, we need a deterministic way to:

1. Track *which* schema changes have been applied to *which*
   environment.
2. Apply new schema changes atomically and reversibly.
3. Do this **outside** the bot's normal startup path — schema changes
   are operator actions, not pod-lifecycle side effects.

## Decision

We ship a standalone Go CLI, **`tools/migrator`**, that opens a Neon
connection and applies SQL migration files. Migrations live in
`internal/leaguewatcher/db/migrations/` and are compiled into the
binary via `embed.FS`.

### Why a standalone CLI (and not bot-startup auto-apply)

Auto-applying migrations on bot startup is the obvious alternative.
Rejected for three reasons:

1. **Blast radius**: a bad migration breaks the bot pod before any
   traffic is served. Operator-running means a bad migration is
   isolated to a build-time check, not a runtime probe.
2. **Concurrency ambiguity**: when CI builds N bot replicas
   concurrently (or an autoscaler spins up extra pods), all of them
   race to apply. Advisory locks mitigate but do not eliminate the
   coordination problem.
3. **Observability**: release-time `migrator status` output is a
   crisp, explicit signal; runtime migration success requires reading
   startup logs to detect.

The bot does **not** check schema version on startup, nor does it
run migrations on startup. Coordination between code and schema
versions is operator responsibility for now (revisit when the pidor
migration lands).

### Why single-digit integer versions

`0001`, `0002`, … `0010` (zero-padded 4-digit). Sequential ascending
integers are trivially ordered, easy to reason about for code reviews,
and line up with the way release-please already versions the bot.
Rejected: timestamped migrations (`20261019120000_init.up.sql`) —
overkill for a single-author project; we'd just be answering
"which date is newer" with extra digits.

### File layout

```
internal/leaguewatcher/db/migrations/{NNNN}_{snake_name}.{up|down}.sql
```

Each migration is **a pair**: `up.sql` and `down.sql`. We do not
allow up-only migrations. The pairing is enforced by the loader —
an orphan file produces a hard error.

`embed.FS` (not filesystem path) so the binary contains the exact
migrations it was built with. Rerunning an old binary against a
newer database therefore attempts an older plan, which is detectable
and safe (it will refuse to roll back migrations it doesn't know).

### The meta-migration is `0001_schema_migrations`

```sql
CREATE TABLE schema_migrations (
    version    INTEGER     PRIMARY KEY,
    name       TEXT        NOT NULL,
    applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
```

Per the user's requirement, the bookkeeping table is itself created
by **migration 0001** rather than by bootstrap Go code. The runner
inserts the bookkeeping row for `0001` after the DDL executes,
inside the same transaction. PK on `version` is the natural
idempotency guard. `applied_at` uses `now()` so we never need the
binary to format timestamps.

### Concurrency

- **Advisory lock** on `pg_try_advisory_lock(0x6D69677261)` (ASCII
  `"migra"`). A second concurrent migrator fails fast with exit code
  `5` rather than queueing and surprising the operator later.
- Lock is held on a dedicated `pgx.Conn` for the whole run, not on
  a pool slot, so `pool.Close()` cannot release it inadvertently.
- `SET LOCAL lock_timeout = '5s'` inside each migration transaction:
  protects against wedged table locks (e.g. an open idle psql
  session holding a strong lock).

### Failure semantics

- **Per-migration transaction**: `BEGIN; <up.sql>; INSERT … book; COMMIT;`
  Atomic apply; a crash mid-migration rolls back cleanly with no
  "dirty" bit required.
- **Fail fast**: first failing statement aborts the run. Earlier
  migrations stay applied; later ones are never attempted. This
  makes post-mortem reasoning about the database state linear:
  "everything ≤ V is applied, nothing > V ran."
- **Idempotency**: rerunning `up` against a partially-applied or
  fully-applied database is safe.

### Configuration

The tool reads **only** environment variables:

```
DATABASE_URL              (required) direct Neon endpoint
MIGRATOR_TEST_DSN         (test-only) integration-test DSN
```

Plus optional `-db URL` flag override. No Doppler SDK dependency
in the tool: operator runs `doppler run -- migrator up`. This keeps
CI patterns composable and avoids a heavyweight auth path inside
the migration tool itself.

The migrator uses `DATABASE_URL` (direct endpoint), **not**
`DATABASE_URL_POOLED`. PgBouncer transaction-mode pooling breaks
advisory-lock guarantees — the lock is session-scoped, and pooling
can switch the underlying Postgres session between commands.

### CLI surface

```
migrator up [N]
migrator down [N]
migrator status
migrator version
```

Exit codes are stable contract points (CI may key on them):

| Code | Meaning |
|------|---------|
| `0` | success (no work = success) |
| `1` | CLI misuse |
| `2` | missing or empty `DATABASE_URL` |
| `3` | connection error |
| `4` | migration error |
| `5` | lock contention |

### Tests

- 15 unit tests against an in-memory `dbHandle` fake: planning
  (up / down / target), idempotency, error injection, lock
  contention, status formatting.
- 1 env-gated integration test (`MIGRATOR_TEST_DSN`) running the
  full clean → up → status → down → idempotency round trip against
  a real Neon dev branch.

No testcontainers dependency introduced — the existing
`internal/leaguewatcher/db/integration_test.go` env-gate pattern is
followed.

## Consequences

### Positive

- Deterministic schema evolution: binary version pins migration set.
- Operator-friendly: `migrator status` and `version` integrate with
  pre-deploy checks.
- Crash-safe and safe to re-run; concurrency is gated.
- Single dependency surface (`pgx/v5` already pulled by ADR-009's
  `db` package).

### Negative

- Sequential integer versions collide across parallel branches
  adding migrations concurrently. Mitigated by rebase-time
  renumbering; documented in `AGENTS.md`.
- Bot and schema can drift (bot running with code expecting schema
  V+N while DB is at V). Acceptable for now; becomes a real risk
  when the pidor migration lands — revisit then.
- No checksum validation: edits to a `.sql` file already applied
  are not detected. Mitigation: team rule "never edit a migration
  once merged to main", enforced in review.

### Neutral

- Migrations run as the same Postgres user as the bot. Fine at
  this scale; revisit when access tiers diversify.

## Alternatives Considered

### 1. Bot auto-applies on startup

Rejected — see "Why a standalone CLI" above.

### 2. `golang-migrate/migrate` library

The de-facto standard Go migration library. Rejected because:
- Adds several large dependencies (database/sql + driver + library).
- Has its own state model (`schema_migrations` table with `dirty`
  flag), which we don't need per-migration granularity for.
- We reuse `internal/leaguewatcher/db.Connect` for connection
  config; pulling another connection layer adds complexity.

### 3. `pressly/goose`

Lighter-weight than golang-migrate. Rejected for similar reasons,
but it remains the primary fallback if we outgrow the hand-rolled
runner.

### 4. Filesystem-based migrations

Rejected — see "File layout" above.

### 5. Bootstrap `CREATE TABLE IF NOT EXISTS schema_migrations` in Go

Rejected — the user explicitly asked for the meta-migration to be
migration `0001`. Cost is one extra visible step in `status` output
on a fresh database; benefit is the runner has zero implicit state.

## Future Work

The most likely follow-ups, in rough priority order:

1. First "real" consumer: **`0002_*` pidor relational schema**
   (separate ADR, depends on pidor migration plan).
2. **Bot-side schema-version guard** — bot logs a warning (non-fatal)
   at startup if DB version mismatches its expectation.
3. **Auto-run in CI** as a release step before the bot image rolls
   out. Requires solving Doppler auth in the GitHub Actions runner;
   not yet implemented.
4. **`migrator validate`** subcommand — checksum each applied
   migration against the embedded copy; detects if someone edited
   a merged migration file.
5. **`migrator create NAME`** — scaffolding subcommand that creates
   the next `NNNN_{name}.{up,down}.sql` pair on disk for the
   operator's editor.

## References

- Plan: `PLAN-migrator-cli.md` (working branch only)
- Implementation commits on branch `feat/migrator-cli`:
  - `feat: add internal/leaguewatcher/db/migrations package with embed.FS`
  - `feat: add tools/migrator for SQL schema migrations`
- Code:
  - `internal/leaguewatcher/db/migrations.go` — embedded loader
  - `internal/leaguewatcher/db/migrations/0001_*` — meta-migration
  - `tools/migrator/main.go` — CLI surface
  - `tools/migrator/migrator.go` — runner + planning
- Preceding ADR: [009-neon-postgres-connection.md](009-neon-postgres-connection.md)
- Releases will reference this ADR when schema changes first land.

## Reviewers

- **Author**: @cauchy2384
- **Approver**: @cauchy2384
- **Date**: 2026-10-19

## Changelog

| Date | Change |
|------|--------|
| 2026-10-19 | Initial version, marked Accepted. |
| 2026-10-19 | Added Doppler-token configuration fallback. The migrator now
  resolves `DATABASE_URL` via three paths in order: `-db` flag,
  `DATABASE_URL` env, then `DOPPLER_TOKEN` env which triggers a
  Doppler API lookup (project=`leaguewatcherbot`, config=`prd`).
  This matches the bot's existing `ConfigManager` pattern (ADR-004)
  and lets operators run `go run tools/migrator/main.go up` directly
  when `DOPPLER_TOKEN` is in the environment, without requiring the
  Doppler CLI. Existing `doppler run --` workflows are unchanged
  (`DATABASE_URL` is injected by the CLI and wins by precedence). |
