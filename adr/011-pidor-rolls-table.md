# ADR 011: `pidor_rolls` Table Schema

## Status

**Accepted** - 2026-10-19

Builds on [ADR-009](009-neon-postgres-connection.md),
[ADR-010](010-migrator-cli.md).

## Context

Pidor game state today lives in `pidors.json`:

```json
{
  "Called": {"channelID": "2024-01-01T00:00:00Z"},
  "Stats":  {"channelID": {"userID": {"name": "User", "count": 5}}}
}
```

Two problems motivate a relational replacement:

1. **Lossy history.** `Called` stores only the latest roll
   timestamp per channel; `Stats` stores only the running count.
   Asking "who was pidor on 2024-05-12 in channel X?" is impossible.
2. **Fragile check.** `CanBeChosen` uses
   `t.YearDay() != time.Now().YearDay()` on the value stored in
   `Called` — server-local time, no year comparison, no transactional
   protection against concurrent `!pidorday` calls. AGENTS.md flags
   this as a Known Fragile Area.

ADR-010 gave us the migration runner. The first real schema consumer
is the pidor table.

## Decision

### Schema (migration `0002_pidor_rolls`)

```sql
CREATE TABLE pidor_rolls (
    id           BIGSERIAL   PRIMARY KEY,
    channel_id   TEXT        NOT NULL,
    user_id      TEXT        NOT NULL,
    user_name    TEXT        NOT NULL,
    rolled_on    DATE        NOT NULL,
    rolled_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    times_rolled INTEGER     NOT NULL DEFAULT 1
                  CHECK (times_rolled > 0),

    CONSTRAINT pidor_rolls_one_per_channel_day
        UNIQUE (channel_id, rolled_on)
);
```

### One winner per channel per day, enforced at the DB layer

`UNIQUE (channel_id, rolled_on)` is a defence-in-depth guarantee.
Two `!pidorday` invocations racing past the (non-atomic)
`CanBeChosen` check will both attempt an INSERT; the second one
fails with `23505` and the consumer code reports the standard
Russian refusal message. State stays consistent.

### `times_rolled`: live rolls are 1; backfilled aggregates are N

The schema designs for *both* events and aggregates:

- **Live rolls** (post cut-over): one row per event,
  `times_rolled = 1`. `rolled_on` is the application's idea of
  today.
- **Backfilled historical aggregates** (from `pidors.json.Stats`):
  one row per (channel, user) with `times_rolled = N` (the JSON
  lifetime count). See "Backfill strategy" below.

This hybrid means lifetime-stats queries can use a single
aggregation `SUM(times_rolled)` regardless of whether the data is
live or imported, with no special case in the consumer code.

### Backfill strategy: deterministic synthetic dates

The strict daily constraint forbids multiple users sharing a single
synthetic backfill date. The importer (a future
`tools/pidors-backfill/` command, out of scope for this ADR) will:

1. For each channel, sort `user_id`s ascending (lexicographic).
2. For the i-th user (0-indexed), compute
   `rolled_on = DATE '1970-01-01' + i days`.
3. Insert with `ON CONFLICT (channel_id, rolled_on) DO NOTHING`.

Properties:

- **Deterministic**: rerunning produces identical dates → conflict →
  `DO NOTHING` → idempotent skip. No state file, no resume logic.
- **No overwrites**: `ON CONFLICT DO NOTHING` preserves the first
  backfill's rows as canonical.
- **Multi-channel safe**: each channel gets its own date sequence
  starting at 1970-01-01; the unique constraint is per-channel, so
  this is fine.
- **Bounded**: even a 100-user channel needs only ~4 months of
  synthetic dates in 1970.
- **Visually obvious**: any row with `rolled_on < '2020-01-01'` is
  unambiguously a backfilled aggregate, not a live roll.

### Time zone: Europe/Moscow

`rolled_on` is a `DATE` (no time component) computed by the
application using `time.Now().In(mskLoc)`. PostgreSQL never sees
the zone decision; the application owns it.

Rationale:
- The Discord community is largely MSK-based.
- Codifying the zone in the data (via the supplied `rolled_on`
  value) makes the rule portable across deployments — the pod can
  move regions without changing semantics.
- UTC was rejected because midnight-UTC roll-over is 03:00 MSK and
  01:00 CET: too early for the typical community activity peak.
- Europe/Berlin was rejected because, although it matches the prior
  `pidors.json` behaviour, most players are eastern European, not
  central.

### Why `rolled_on` is stored as a DATE column (not derived)

An earlier draft considered deriving the day from `rolled_at` via an
expression index:

```sql
CREATE UNIQUE INDEX pidor_rolls_daily
  ON pidor_rolls (channel_id, (rolled_at AT TIME ZONE 'Europe/Moscow')::date);
```

Rejected: `AT TIME ZONE` is `STABLE`, not `IMMUTABLE`, because its
result depends on the session `TimeZone`. Expression indexes must
be immutable. Workarounds (custom IMMUTABLE function) add
complexity without benefit.

## Consequences

### Positive

- **Defence-in-depth**: race-condition double-rolls are now stopped
  at the storage layer, not just by a non-atomic application check.
- **Auditability**: every roll leaves a row. Historical queries
  ("who was pidor last Tuesday?", "is this user over-represented
  this month?") become possible.
- **Backfill without fabrication**: `times_rolled` lets us load
  historical aggregates as a single row per user without inventing
  fake event dates.
- **Idempotent backfill**: rerunning the importer is safe.
- **No application change yet**: this ADR is migration-only. The
  bot keeps using `pidors.json` until a follow-up PR flips the
  readers/writers.

### Negative

- **Username snapshot only**: `user_name` is copy-at-write. If a
  user renames, the historical rows keep the old value. Mitigation:
  queries that want the *latest* username use `MAX(user_name)`.
- **No game-logic validation at the DB**: PostgreSQL cannot check
  that the rolled-on day "matches" the `rolled_at` insertion time
  (and shouldn't). Application code owns this invariant.
- **No FK relationships**: `pidor_rolls` doesn't reference any other
  table. If a future schema adds a `users` dimension table we may
  want a migration to normalise.

### Neutral

- **`pidors.json` continues to exist** until the consumer PR removes
  it. There's a (brief) period where two stores diverge: the JSON
  continues to be written by the live bot while `pidor_rolls` sits
  empty. Acceptable because no code reads from `pidor_rolls` yet.

## Alternatives Considered

### Three-column UNIQUE `(channel_id, user_id, rolled_on)`

Looser constraint, allows multiple winners per channel-day. **Rejected**:
loses the DB-level race protection, weakens the data model, no
counterbalancing need (the "double-pidor in one day" use case isn't
real).

### Drop the constraint entirely and rely on application

Matches today's JSON behaviour exactly. **Rejected**: same reason as
above. We have the chance to fix the race now; taking it.

### No separate `rolled_on` column; use derived-day index

See "Why `rolled_on` is stored as a DATE column" above. **Rejected**:
not possible with a UNIQUE INDEX for the zone-aware derivation;
custom IMMUTABLE function is over-engineering.

### Single marker date `1970-01-01` shared across backfilled users

Simpler than deterministic per-user dates. **Rejected**: violates
the strict `UNIQUE (channel_id, rolled_on)` constraint for any
channel with more than one historical player.

### Composite PRIMARY KEY `(channel_id, rolled_on)` instead of `id`

Saves 8 bytes/row + a sequence; composite key expresses the
constraint naturally. **Rejected**: costs ergonomic cursor
pagination (`ORDER BY id`) for future dashboards, and the `BIGSERIAL`
key is the established convention in `schema_migrations`.

## Verification

Production Neon verification (run on 2026-10-19 against
`ep-sweet-mud-bayin6ud.c-8.eu-central-1.aws.neon.tech`):

| Step | Result |
|------|--------|
| `migrator status` before | `applied: 1, pending: 1 (0002)` |
| `migrator up` | applied in ~50 ms, recorded in `schema_migrations` |
| `\d pidor_rolls` | 7 columns, PK on `id`, UNIQUE on `(channel_id, rolled_on)`, CHECK on `times_rolled > 0` |
| Live-roll INSERT on 2026-10-20 | OK |
| Other-user INSERT on same channel + same day | `ERROR 23505 duplicate key value violates unique constraint "pidor_rolls_one_per_channel_day"` |
| Backfill pattern (two users on distinct 1970 dates) | OK, both rows accepted |
| `times_rolled=0` INSERT | `ERROR 23514 new row for relation violates check constraint` |
| Acceptance query `SUM(times_rolled) GROUP BY user_id` | Returns correct totals |
| `migrator down` then `migrator up` | Clean round trip, table re-created |

## Reviewers

Author: @cauchy2384 (with claude-code)
Approver: @cauchy2384
Date: 2026-10-19

## Changelog

- 2026-10-19: Accepted. Schema reviewed and applied to production.
- 2026-10-19: Backfill tool shipped as `tools/pidors-backfill`.
  Reads `pidors.json`, computes deterministic synthetic dates per
  user (ADR-011 §"Backfill strategy"), executes inside a single
  transaction with `ON CONFLICT DO NOTHING` for idempotency.
  **`Pidor.Called` is intentionally not imported** — the relational
  model cannot honestly represent "a roll happened but we don't
  know who won", and the operator accepted the documented consequence
  that any channel whose roll already happened today will allow one
  extra roll on cutover day. See `PLAN-pidors-backfill-tool.md` §5.

## References

- Migration files: `internal/leaguewatcher/db/migrations/0002_pidor_rolls.{up,down}.sql`
- Migration runner: [`tools/migrator/`](https://github.com/cauchy2384/leaguewatcherbot/tree/main/tools/migrator)
- Bot code consuming this table (future PR): `internal/leaguewatcher/bot/pidor.go`
- Related: [ADR-009](009-neon-postgres-connection.md),
  [ADR-010](010-migrator-cli.md)
- Plan: `PLAN-pidor-rolls-table.md` (working tree, not committed)
