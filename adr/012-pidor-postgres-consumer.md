# ADR 012: Pidor Commands Consume Postgres (Hard Cutover)

## Status

**Accepted** — 2026-10-19

Builds on [ADR-009](009-neon-postgres-connection.md),
[ADR-010](010-migrator-cli.md),
[ADR-011](011-pidor-rolls-table.md).

## Context

ADR-011 landed the `pidor_rolls` schema and the backfill tool which has
been run against production: at the time of this writing the table holds
74 rows / 1527 lifetime rolls across 6 channels.

The bot still reads + writes `pidors.json` for the three pidor commands:

- `!pidorday` (`pidorOfTheDay`) — pick a winner, write a roll event
- `!pidorstats` (`pidorStats`) — top-N lifetime leaderboard
- `!pidorme` (`pidorPersonalStats`) — user's lifetime rank

Critically, `pidorOfTheDay` performs a non-atomic check-then-act:

```go
if !p.CanBeChosen(cID) { refuse }
winner := pick(members)
p.WasChosen(cID, winner)
```

AGENTS.md flags the matching `YearDay()` comparison and lack of
concurrency primitives as Known Fragile Area #5. The pier-side fix
needs a DB.

## Decision

### Hard cutover — no flag, no JSON fallback

The user explicitly requested a one-step migration with **no
`PIDOR_STORE` flag**, no dual-write, and **no JSON fallback path**.

- `DATABASE_URL` becomes **required** at bot startup. The bot exits
  immediately if unset (previously it ran fine without).
- The `repository.Pidor` struct + `NewPidor` + `Sync` + the
  `sync.Mutex` wrapper in `Bot.Pidors` are **deleted**.
- `internal/leaguewatcher/bot/repository/pidor.go` is removed entirely.
- Existing pidors.json files stay on disk but the bot ignores them.

### Interface kept as a test seam

`bot.Pidors` accepts a `repository.PidorStore` interface. Only one
production implementation exists (`PidorStorePostgres`); the interface
exists so unit tests can use an in-memory fake. We intentionally did
**not** keep a JSON-backed production implementation.

### Atomic daily reservation replaces check-then-act

```go
TryChooseToday(ctx, channelID, userID, userName, today)
```

executes

```sql
INSERT INTO pidor_rolls (channel_id, user_id, user_name, rolled_on, times_rolled)
VALUES ($1,$2,$3,$4::date,1)
ON CONFLICT (channel_id, rolled_on) DO NOTHING
RETURNING id;
```

- Returns 1 row → won → announce winner in Discord.
- Returns 0 rows → lost → reply with the existing
  `"сегодня уже выбирали, пидрила!"` message (same UX as before).

This is provably safe across concurrent callers (the constraint is
enforced at the storage layer), single-shot, and eliminates the
`CanBeChosen` pre-check round-trip. The today date is supplied by the
caller in `Europe/Moscow` (per ADR-011).

### Timezone: Europe/Moscow, codified in the binary

`time.LoadLocation("Europe/Moscow")` at startup. Source of truth lives
in the app's choice, not in the deployment's OS or env. The Docker
image doesn't need `tzdata` because we use

```go
import _ "time/tzdata"
```

which bakes the IANA tables into the binary.

### Data shape relocation

The old `repository.Pidor` (struct + IO) is gone, but `tools/pidors-backfill`
and `tools/pidors2csv` still need to **read** existing pidors.json
snapshots. The shape is relocated:

- `internal/leaguewatcher/pidorjson/legacy.go` — types only.
- `internal/leaguewatcher/pidorjson/load.go` — `pidorjson.Load(path)`,
  tolerant of trailing garbage in the file (matches the legacy bot's
  reader behaviour).

Tools updated to import from this new package; their behaviour is
unchanged.

### Pre-merge sign-off: parity test against prod

`internal/leaguewatcher/bot/repository/pidor_store_pg_integration_test.go`
includes `TestStatsParityAgainstJSON`: gates on `MIGRATOR_TEST_DSN` +
`PIDORS_TEST_JSON_PATH`, asserts exact per-(channel, user) count parity
between the JSON snapshot and the Postgres `Stats()` aggregation.

Verified on the production snapshot from 2026-10-19:

```
channel=1206239463597019156  parity ok  (8 users, 8 rolls)
channel=1218476352278364301  parity ok  (1 users, 1 rolls)
channel=148124484803493888   parity ok  (60 users, 1512 rolls)
channel=836221978003767369   parity ok  (1 users, 1 rolls)
channel=932549761406476309   parity ok  (3 users, 3 rolls)
channel=933623889181954081   parity ok  (1 users, 2 rolls)
```

This test is documented as a pre-merge sign-off, not a long-lived CI
check; it will be deleted in a future cleanup once JSON is permanently
stale.

## Consequences

### Positive

- **Race-condition fixed.** `UNIQUE(channel_id, rolled_on)` is the
  storage-layer guarantee that there is at most one row per
  (channel, day). Concurrent rolls can no longer double-record.
- **No clock dependency** in `WasChosen` (the previous `time.YearDay()`
  hack). Day boundaries are computed deterministically from
  `time.Now().In(mskLoc)`.
- **Auditable history.** Every roll is a row. Locked behind the same
  transactional correctness as every other Postgres operation.
- **Simpler `Pidors` type.** No mutex, no sync loop, no JSON file
  choreography.

### Negative

- **Hard dependency on Postgres availability.** If Neon is down,
  pidor commands fail-closed with a degradation message. Previously
  they'd run normally off `pidors.json`. Reasonable trade given ADR-009
  already has the bot opening a pooled connection.
- **Breaking change.** Bot refuses to start without `DATABASE_URL`.
  Release will be semver-major (v2.0.0).
- **Cutover-day double-roll edge case** (already documented in ADR-011):
  channels whose `pidors.json.Called[cid]` was today at deploy time
  get one extra roll. Operator accepted.

### Neutral

- **Bot clock still matters**, just differently: `Europe/Moscow` is
  codified in the binary (`time/tzdata`); the host's OS timezone no
  longer leaks into the data model.
- **`pidors.json` is orphaned but not deleted.** Operator cleans up
  manually after cutover (or keeps for posterity).
- **Rollback = git revert + redeploy.** `pidor_rolls` is append-only
  during the live window; the JSON store has the historical data
  through the cutover moment, so a revert would see "stale until
  cutover-time" data but not lose it.

## Alternatives Considered

### Feature flag (`PIDOR_STORE=json|postgres`)

Default JSON, opt-in to Postgres, rollback by flipping flag.
**Rejected** — we already have the backfill landed and verified; the
flag adds a code path that exists only to be deleted later. Hard
cutover is honest. Rollback = revert.

### Dual-write (write JSON + Postgres, read Postgres during transition)

Trades deployment-step complexity for write-path complexity — the bot
now needs a transaction spanning both stores, or accepts divergence.
Backfill already gives us the bootstrap; no need for dual-write.
**Rejected.**

### `WasChosen` semantics preserved as-is

Make the Postgres impl behave exactly like the JSON
`WasChosen(cID, pidor)` (auto-increment counter) without the "Try"
return. Hides the concurrency fix in implementation detail. Rejected —
`TryChooseToday` makes the contract explicit and unit-testable.

### Use `pgconn.PgError.Code == "23505"` instead of `RETURNING id`

Catch the unique-violation error vs. use `ON CONFLICT DO NOTHING
RETURNING`. Both work; `ON CONFLICT` is more idiomatic and avoids
relying on pgx error type plumbing. Chosen.

## Rollback procedure

If this needs to be backed out post-merge:

1. `git revert <mergecommit>`
2. Redeploy.
3. The bot returns to reading `pidors.json`. Data written to
   `pidor_rolls` during the live window is **not** lost — it stays in
   the table.
4. If we later re-roll forward, the new bot code reads the existing
   table including the live interval's rows. Recovery is automatic.

Operator responsibility: keep `pidors.json` around until after the
cleanup window closes.

## Out of Scope (tracked separately)

- **Cleanup PR** to remove `tools/pidors-backfill`,
  `tools/pidors2csv`, `pidors.json` from `.gitignore` after a
  confidence window. The parity test can also retire at that point.
- **New reporting commands** (`!pidormonth`, day-of-week histograms,
  streaks) — the schema already supports them.
- **`Called` map reconciliation** for the cutover-day edge case —
  operator may fix manually if a specific channel matters.

## References

- Schema: [`internal/leaguewatcher/db/migrations/0002_pidor_rolls.up.sql`](https://github.com/cauchy2384/leaguewatcherbot/blob/main/internal/leaguewatcher/db/migrations/0002_pidor_rolls.up.sql)
- Backfill: [`tools/pidors-backfill/`](https://github.com/cauchy2384/leaguewatcherbot/tree/main/tools/pidors-backfill)
- Implementation: [`internal/leaguewatcher/bot/repository/pidor_store_pg.go`](https://github.com/cauchy2384/leaguewatcherbot/blob/main/internal/leaguewatcher/bot/repository/pidor_store_pg.go)
- Bot changes: [`internal/leaguewatcher/bot/pidor.go`](https://github.com/cauchy2384/leaguewatcherbot/blob/main/internal/leaguewatcher/bot/pidor.go)
- Related: [ADR-009](009-neon-postgres-connection.md),
  [ADR-010](010-migrator-cli.md),
  [ADR-011](011-pidor-rolls-table.md)
- Plan: `PLAN-pidor-db-consumer.md` (working tree, not committed)

## Reviewers

Author: @cauchy2384 (with claude-code)
Approver: @cauchy2384
Date: 2026-10-19

## Changelog

- 2026-10-19: Accepted. Parity test passes against production.
